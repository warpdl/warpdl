package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/scheduler"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func resolveDownloadSchedule(m *common.DownloadParams, now time.Time) (time.Time, string, bool, error) {
	if m.Schedule == "" && m.StartAt == "" && m.StartIn == "" {
		return time.Time{}, "", false, nil
	}
	if m.StartAt != "" && m.StartIn != "" {
		return time.Time{}, "", false, errors.New("start_at and start_in are mutually exclusive")
	}

	var startAt time.Time
	if m.StartAt != "" {
		parsed, err := time.ParseInLocation("2006-01-02 15:04", m.StartAt, time.Local)
		if err != nil {
			return time.Time{}, "", false, fmt.Errorf("invalid start time: %w", err)
		}
		if !parsed.After(now) {
			return time.Time{}, "", false, errors.New("start time must be in the future")
		}
		startAt = parsed
	} else if m.StartIn != "" {
		delay, err := time.ParseDuration(m.StartIn)
		if err != nil {
			return time.Time{}, "", false, fmt.Errorf("invalid start delay: %w", err)
		}
		if delay <= 0 {
			return time.Time{}, "", false, errors.New("start delay must be greater than zero")
		}
		startAt = now.Add(delay)
	}

	if m.Schedule == "" {
		return startAt, "", true, nil
	}
	if startAt.IsZero() {
		next, err := scheduler.NextOccurrence(m.Schedule, now)
		if err != nil {
			return time.Time{}, "", false, fmt.Errorf("invalid schedule: %w", err)
		}
		startAt = next
	} else if _, err := scheduler.NextOccurrence(m.Schedule, now); err != nil {
		return time.Time{}, "", false, fmt.Errorf("invalid schedule: %w", err)
	}
	return startAt, m.Schedule, true, nil
}

func (s *Api) downloadHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.DownloadParams
	if err := json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}

	extRes, err := s.elEngine.Extract(m.Url)
	dlURL := extRes.URL
	if err != nil {
		s.log.Printf("failed to extract URL from extension: %s\n", err.Error())
		dlURL = m.Url
	}
	// Plugin-supplied headers flow through a separate slice so the
	// downloader can strip them on cross-origin redirects (Task 19)
	// while preserving user-supplied Headers semantics.
	var pluginHeaders warplib.Headers
	for k, v := range extRes.Headers {
		pluginHeaders = append(pluginHeaders, warplib.Header{Key: k, Value: v})
	}

	// If the user did NOT pass -o/--filename but the plugin supplied a
	// filename hint (e.g. Drive's files.get?fields=name result), use it.
	// User intent always wins over plugin hints.
	//
	// userExplicitFileName is captured BEFORE the plugin fallback so the
	// downloader knows whether to lock the name (user-chosen, error on
	// collision) or auto-rename it browser-style (URL/plugin-derived,
	// rename to "foo (1).pdf" so a retry of a failed download isn't
	// blocked by the leftover stub).
	userExplicitFileName := m.FileName != ""
	if m.FileName == "" && extRes.FileName != "" {
		m.FileName = extRes.FileName
	}

	// Detect scheme to choose code path
	parsed, parseErr := url.Parse(dlURL)
	if parseErr != nil {
		// net/url parse errors can include the complete input, including
		// credentials and query-string secrets. Keep the client-facing error
		// deliberately generic.
		return common.UPDATE_DOWNLOAD, nil, errors.New("invalid URL")
	}
	scheme := strings.ToLower(parsed.Scheme)

	switch scheme {
	case "ftp", "ftps", "sftp":
		return s.downloadProtocolHandler(sconn, pool, dlURL, scheme, &m)
	default:
		return s.downloadHTTPHandler(sconn, pool, dlURL, &m, pluginHeaders, userExplicitFileName)
	}
}

// ReportAsyncDownloadError pushes an async-path error (queue auto-start
// failure, scheduler trigger failure, goroutine panic) through the pool
// so any connected CLI sees an InitError broadcast and Stop event
// instead of polling progress forever.
//
// When mgr is non-nil and the failed download never wrote a byte
// (item.Downloaded == 0), the manager entry is purged. Rationale: a
// download that died before writing anything has no resume state and
// no completed bytes — leaving it in history is noise that accumulates
// every retry. Mid-download failures (nread > 0) are kept so the user
// can resume.
//
// Exported for daemon wiring (queue, scheduler).
func ReportAsyncDownloadError(pool *server.Pool, uid string, err error, mgr *warplib.Manager) {
	if err == nil {
		return
	}
	pool.BroadcastTerminal(uid, server.MakeDownloadError(uid, err))
	pool.WriteError(uid, server.ErrorTypeCritical, err.Error())
	if mgr != nil {
		mgr.ReleaseQueueSlot(uid)
		info, scheduled := mgr.GetScheduleInfo(uid)
		if item := mgr.GetItem(uid); item != nil && item.GetDownloaded() == 0 &&
			(!scheduled || info.CronExpr == "") {
			_ = mgr.PurgeFailedDownload(uid)
		}
	}
}

// reportAsyncDownloadError is the api-internal helper that wraps
// ReportAsyncDownloadError with the api's manager so zero-byte
// failures are also purged from history.
func (s *Api) reportAsyncDownloadError(pool *server.Pool, uid string, err error) {
	ReportAsyncDownloadError(pool, uid, err, s.manager)
}

// cleanupDownloadRegistration releases resources after a request has already
// registered a downloader with the pool and possibly the manager. It is used
// only before a successful response is returned, so queue work that the
// caller never learned about cannot start later.
func cleanupDownloadRegistration(
	manager *warplib.Manager,
	pool *server.Pool,
	hash string,
	fallback interface{ Close() error },
	generations ...*server.TransferGeneration,
) error {
	var generation *server.TransferGeneration
	if len(generations) > 0 {
		generation = generations[0]
	}
	var closeErr error
	if fallback != nil {
		closeErr = fallback.Close()
	} else if item := manager.GetItem(hash); item != nil {
		closeErr = item.CloseDownloader()
	}
	cleanupErr := errors.Join(
		closeErr,
		cleanupDownloadRegistrationState(manager, hash),
	)
	// Keep the exact generation registered until all hash-keyed manager and
	// queue cleanup has finished. A same-hash replacement cannot publish in
	// the gap and be closed, released, or purged by this rejected request.
	if generation != nil {
		generation.Abort()
	} else if pool != nil {
		// Compatibility path for callers that have not reserved an exact
		// generation (principally setup cleanup and legacy tests).
		pool.StopDownload(hash)
	}
	return cleanupErr
}

// cleanupUnpublishedDownloadRegistration releases only resources owned by a
// request whose Manager AddDownload/AddProtocolDownload call failed. Add
// errors occur before publication, so hash-wide queue/manager cleanup could
// delete an unrelated same-hash replacement. Keep the exact pool generation
// reserved until the local pointer is closed, then abort only that generation.
func cleanupUnpublishedDownloadRegistration(
	fallback interface{ Close() error },
	generation *server.TransferGeneration,
) error {
	var closeErr error
	if fallback != nil {
		closeErr = fallback.Close()
	}
	if generation != nil {
		generation.Abort()
	}
	return closeErr
}

// cleanupDownloadRegistrationState removes only non-allocation registration
// state. Callers that already closed an exact RunLease use this to avoid a
// second shared allocation close.
func cleanupDownloadRegistrationState(
	manager *warplib.Manager,
	hash string,
) error {
	manager.ReleaseQueueSlot(hash)
	return manager.PurgeFailedDownload(hash)
}

// downloadHTTPHandler handles HTTP and HTTPS downloads.
// pluginHeaders carries headers sourced from a plugin's extract() result;
// they are routed through DownloaderOpts.PluginHeaders so the downloader
// strips them on cross-origin redirects (Task 19).
// userExplicitFileName is true when the user passed --filename on the CLI
// (vs. a name supplied by a plugin or derived from the URL); the
// downloader uses it to decide whether to auto-rename on collision.
func (s *Api) downloadHTTPHandler(sconn *server.SyncConn, pool *server.Pool, dlURL string, m *common.DownloadParams, pluginHeaders warplib.Headers, userExplicitFileName bool) (common.UpdateType, any, error) {
	scheduledAt, cronExpr, scheduled, err := resolveDownloadSchedule(m, time.Now())
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}
	queue := s.manager.GetQueue()

	// Determine which client to use based on proxy setting
	dlClient := s.client
	safeProxyURL, proxyCredentialsRequired, err := warplib.SanitizeProxyURLForPersistence(m.Proxy)
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	if proxyCredentialsRequired && (scheduled || queue != nil) {
		return common.UPDATE_DOWNLOAD, nil, errors.New(
			"queued or scheduled downloads cannot use proxy credentials because proxy secrets are not persisted",
		)
	}
	if m.Proxy != "" {
		dlClient, err = warplib.NewHTTPClientWithProxy(m.Proxy)
		if err != nil {
			return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		// Preserve cookie jar from default client
		if s.client.Jar != nil {
			dlClient.Jar = s.client.Jar
		}
	}

	var (
		d          *warplib.Downloader
		generation *server.TransferGeneration
	)

	// Build retry config from params
	var retryConfig *warplib.RetryConfig
	if m.MaxRetries != 0 || m.RetryDelay != 0 {
		cfg := warplib.DefaultRetryConfig()
		if m.MaxRetries != 0 {
			cfg.MaxRetries = m.MaxRetries
		}
		if m.RetryDelay != 0 {
			cfg.BaseDelay = time.Duration(m.RetryDelay) * time.Millisecond
		}
		retryConfig = &cfg
	}

	// Convert timeout from seconds to duration
	var requestTimeout time.Duration
	if m.Timeout > 0 {
		requestTimeout = time.Duration(m.Timeout) * time.Second
	}

	// Parse speed limit
	var speedLimit int64
	err = nil
	if m.SpeedLimit != "" {
		speedLimit, err = warplib.ParseSpeedLimit(m.SpeedLimit)
		if err != nil {
			return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid speed limit: %w", err)
		}
	}

	transferCtx := s.manager.TransferContext()
	handlers := managedTransferHandlers(
		func() *server.TransferGeneration { return generation },
		func() bool { return d != nil && d.IsStopped() },
		transferCtx,
	)
	d, err = warplib.NewDownloader(dlClient, dlURL, &warplib.DownloaderOpts{
		Context:             transferCtx,
		Headers:             m.Headers,
		PluginHeaders:       pluginHeaders,
		LockFileName:        userExplicitFileName,
		ForceParts:          m.ForceParts,
		FileName:            m.FileName,
		DownloadDirectory:   m.DownloadDirectory,
		MaxConnections:      m.MaxConnections,
		MaxSegments:         m.MaxSegments,
		Overwrite:           m.Overwrite,
		RetryConfig:         retryConfig,
		RequestTimeout:      requestTimeout,
		SpeedLimit:          speedLimit,
		DisableWorkStealing: m.DisableWorkStealing,
		Handlers:            handlers,
	})
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}
	var reserved bool
	generation, reserved = pool.BeginDownload(d.GetHash(), sconn)
	if !reserved {
		return common.UPDATE_DOWNLOAD, nil, errors.Join(
			errors.New("download is already running or still stopping"),
			d.Close(),
		)
	}
	err = s.manager.AddDownload(d, &warplib.AddDownloadOpts{
		ChildHash:        m.ChildHash,
		IsHidden:         m.IsHidden,
		IsChildren:       m.IsChildren,
		AbsoluteLocation: d.GetDownloadDirectory(),
		Priority:         warplib.Priority(m.Priority),
		// Queue registration is deliberately deferred until all metadata
		// has been durably recorded below.
		// Queue.Add may synchronously invoke the daemon's start callback.
		SkipQueue: scheduled || queue != nil,
		TransferConfig: warplib.TransferConfig{
			ProxyURL:                 safeProxyURL,
			ProxyCredentialsRequired: proxyCredentialsRequired,
		},
	})
	if err != nil {
		cleanupErr := cleanupUnpublishedDownloadRegistration(d, generation)
		return common.UPDATE_DOWNLOAD, nil, errors.Join(err, cleanupErr)
	}

	if scheduled {
		if err := s.manager.ConfigureSchedule(d.GetHash(), scheduledAt, cronExpr, warplib.ScheduleStateScheduled); err != nil {
			cleanupErr := cleanupDownloadRegistration(s.manager, pool, d.GetHash(), d, generation)
			return common.UPDATE_DOWNLOAD, nil, errors.Join(err, cleanupErr)
		}
		// A validator-less response body is deliberately retained by
		// NewDownloader so an immediate transfer consumes the exact metadata
		// representation. A schedule may wait arbitrarily long, so release
		// that body now and leave only persisted metadata. The trigger path
		// observes the nil allocation and Fresh-reconstructs/re-probes.
		item := s.manager.GetItem(d.GetHash())
		if item == nil {
			cleanupErr := cleanupDownloadRegistration(s.manager, pool, d.GetHash(), d, generation)
			return common.UPDATE_DOWNLOAD, nil, errors.Join(warplib.ErrDownloadNotFound, cleanupErr)
		}
		if closeErr := item.CloseDownloader(); closeErr != nil {
			cleanupErr := cleanupDownloadRegistration(s.manager, pool, d.GetHash(), d, generation)
			return common.UPDATE_DOWNLOAD, nil, errors.Join(
				fmt.Errorf("close scheduled downloader: %w", closeErr),
				cleanupErr,
			)
		}
		if s.scheduler != nil {
			s.scheduler.Add(scheduler.ScheduleEvent{
				ItemHash:  d.GetHash(),
				TriggerAt: scheduledAt,
				CronExpr:  cronExpr,
			})
		}
		// Do not start the download; scheduler owns its first activation.
		return common.UPDATE_DOWNLOAD, &common.DownloadResponse{
			ContentLength:     d.GetContentLength(),
			DownloadId:        d.GetHash(),
			FileName:          d.GetFileName(),
			SavePath:          d.GetSavePath(),
			DownloadDirectory: d.GetDownloadDirectory(),
			MaxConnections:    d.GetMaxConnections(),
			MaxSegments:       d.GetMaxParts(),
		}, nil
	}

	// Queue-enabled daemons start downloads through the queue callback.
	// Without a queue, start immediately.
	if queue == nil {
		runLease, leaseErr := s.manager.AcquireDownloadRunLease(d.GetHash(), d)
		if leaseErr != nil {
			// A failed exact claim means ownership may already have moved to
			// an ABA replacement. Abort only this pool generation; closing or
			// purging by hash could destroy the replacement.
			generation.Abort()
			return common.UPDATE_DOWNLOAD, nil, leaseErr
		}
		if launchErr := s.launchInitialRunLease(
			generation,
			d.GetHash(),
			runLease,
		); launchErr != nil {
			return common.UPDATE_DOWNLOAD, nil, launchErr
		}
	} else {
		queue.Add(d.GetHash(), warplib.Priority(m.Priority))
		waiting, closeErr := s.manager.CloseWaitingDownloader(d.GetHash())
		if closeErr != nil {
			cleanupErr := cleanupDownloadRegistration(s.manager, pool, d.GetHash(), d, generation)
			return common.UPDATE_DOWNLOAD, nil, errors.Join(
				fmt.Errorf("close queued downloader: %w", closeErr),
				cleanupErr,
			)
		}
		if waiting {
			s.log.Printf("Queued download %s will be freshly probed when promoted\n", d.GetHash())
		}
	}
	return common.UPDATE_DOWNLOAD, &common.DownloadResponse{
		ContentLength:     d.GetContentLength(),
		DownloadId:        d.GetHash(),
		FileName:          d.GetFileName(),
		SavePath:          d.GetSavePath(),
		DownloadDirectory: d.GetDownloadDirectory(),
		MaxConnections:    d.GetMaxConnections(),
		MaxSegments:       d.GetMaxParts(),
	}, nil
}

// downloadProtocolHandler handles FTP, FTPS, and SFTP downloads via SchemeRouter.
func (s *Api) downloadProtocolHandler(sconn *server.SyncConn, pool *server.Pool, rawURL, scheme string, m *common.DownloadParams) (common.UpdateType, any, error) {
	if s.schemeRouter == nil {
		return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("%s downloads not available: scheme router not initialized", scheme)
	}
	scheduledAt, cronExpr, scheduled, err := resolveDownloadSchedule(m, time.Now())
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}
	queue := s.manager.GetQueue()
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid %s URL: %w", scheme, err)
	}
	var protocolUsername string
	protocolCredentialsRequired := false
	if parsedURL.User != nil {
		protocolUsername = parsedURL.User.Username()
		_, protocolCredentialsRequired = parsedURL.User.Password()
	}
	if protocolCredentialsRequired && (scheduled || queue != nil) {
		return common.UPDATE_DOWNLOAD, nil, fmt.Errorf(
			"%s credentials cannot be used with queued or scheduled downloads because protocol secrets are not persisted",
			scheme,
		)
	}

	// Create protocol downloader via SchemeRouter
	transferCtx := s.manager.TransferContext()
	pd, err := s.schemeRouter.NewDownloader(rawURL, &warplib.DownloaderOpts{
		Context:           transferCtx,
		FileName:          m.FileName,
		DownloadDirectory: m.DownloadDirectory,
		SSHKeyPath:        m.SSHKeyPath,
		Overwrite:         m.Overwrite,
	})
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}

	// Probe to get file metadata
	probe, err := pd.Probe(transferCtx)
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, errors.Join(err, pd.Close())
	}

	// Determine protocol
	var proto warplib.Protocol
	switch scheme {
	case "ftps":
		proto = warplib.ProtoFTPS
	case "sftp":
		proto = warplib.ProtoSFTP
	default:
		proto = warplib.ProtoFTP
	}

	var generation *server.TransferGeneration
	// Build handlers for protocol download (no compile handlers — single-stream protocols).
	handlers := managedTransferHandlers(
		func() *server.TransferGeneration { return generation },
		func() bool { return pd.IsStopped() },
		transferCtx,
	)

	// Get credential-stripped URL for safe persistence
	cleanURL := warplib.StripURLCredentials(rawURL)

	var reserved bool
	generation, reserved = pool.BeginDownload(pd.GetHash(), sconn)
	if !reserved {
		return common.UPDATE_DOWNLOAD, nil, errors.Join(
			errors.New("download is already running or still stopping"),
			pd.Close(),
		)
	}
	err = s.manager.AddProtocolDownload(pd, probe, cleanURL, proto, handlers, &warplib.AddDownloadOpts{
		ChildHash:        m.ChildHash,
		IsHidden:         m.IsHidden,
		IsChildren:       m.IsChildren,
		AbsoluteLocation: pd.GetDownloadDirectory(),
		Priority:         warplib.Priority(m.Priority),
		SkipQueue:        scheduled || queue != nil,
		SSHKeyPath:       m.SSHKeyPath,
		TransferConfig: warplib.TransferConfig{
			ProtocolUsername:            protocolUsername,
			Overwrite:                   m.Overwrite,
			ProtocolCredentialsRequired: protocolCredentialsRequired,
		},
	})
	if err != nil {
		cleanupErr := cleanupUnpublishedDownloadRegistration(pd, generation)
		return common.UPDATE_DOWNLOAD, nil, errors.Join(err, cleanupErr)
	}

	if scheduled {
		if err := s.manager.ConfigureSchedule(pd.GetHash(), scheduledAt, cronExpr, warplib.ScheduleStateScheduled); err != nil {
			cleanupErr := cleanupDownloadRegistration(s.manager, pool, pd.GetHash(), pd, generation)
			return common.UPDATE_DOWNLOAD, nil, errors.Join(err, cleanupErr)
		}
		item := s.manager.GetItem(pd.GetHash())
		if item == nil {
			cleanupErr := cleanupDownloadRegistration(s.manager, pool, pd.GetHash(), pd, generation)
			return common.UPDATE_DOWNLOAD, nil, errors.Join(warplib.ErrDownloadNotFound, cleanupErr)
		}
		if closeErr := item.CloseDownloader(); closeErr != nil {
			cleanupErr := cleanupDownloadRegistration(s.manager, pool, pd.GetHash(), pd, generation)
			return common.UPDATE_DOWNLOAD, nil, errors.Join(
				fmt.Errorf("close scheduled downloader: %w", closeErr),
				cleanupErr,
			)
		}
		if s.scheduler != nil {
			s.scheduler.Add(scheduler.ScheduleEvent{
				ItemHash:  pd.GetHash(),
				TriggerAt: scheduledAt,
				CronExpr:  cronExpr,
			})
		}
		// Scheduled transfers always reconstruct and re-probe at trigger time.
		// This keeps in-process behavior identical to restart behavior and
		// avoids holding protocol connections across an arbitrary delay.
		return common.UPDATE_DOWNLOAD, protocolDownloadResponse(pd), nil
	}

	// Queue-enabled daemons start downloads through the queue callback.
	// Without a queue, start immediately.
	if queue == nil {
		runLease, leaseErr := s.manager.AcquireProtocolRunLease(pd.GetHash(), pd)
		if leaseErr != nil {
			// Lost exact ownership: do not close or purge a possible
			// replacement registered under the same hash.
			generation.Abort()
			return common.UPDATE_DOWNLOAD, nil, leaseErr
		}
		if launchErr := s.launchInitialRunLease(
			generation,
			pd.GetHash(),
			runLease,
		); launchErr != nil {
			return common.UPDATE_DOWNLOAD, nil, launchErr
		}
	} else {
		queue.Add(pd.GetHash(), warplib.Priority(m.Priority))
		if _, closeErr := s.manager.CloseWaitingDownloader(pd.GetHash()); closeErr != nil {
			cleanupErr := cleanupDownloadRegistration(s.manager, pool, pd.GetHash(), pd, generation)
			return common.UPDATE_DOWNLOAD, nil, errors.Join(
				fmt.Errorf("close queued downloader: %w", closeErr),
				cleanupErr,
			)
		}
	}

	return common.UPDATE_DOWNLOAD, protocolDownloadResponse(pd), nil
}

func protocolDownloadResponse(pd warplib.ProtocolDownloader) *common.DownloadResponse {
	return &common.DownloadResponse{
		ContentLength:     pd.GetContentLength(),
		DownloadId:        pd.GetHash(),
		FileName:          pd.GetFileName(),
		SavePath:          pd.GetSavePath(),
		DownloadDirectory: pd.GetDownloadDirectory(),
		MaxConnections:    pd.GetMaxConnections(),
		MaxSegments:       pd.GetMaxParts(),
	}
}

// applyTimestampSuffix adds a timestamp suffix to a filename before the last extension.
// Format: <basename>-<YYYY-MM-DDTHHMMSS>.<ext>
// For files with no extension: <basename>-<timestamp>
// For files with multiple dots: only the last extension is treated as the extension.
func applyTimestampSuffix(filename string, t time.Time) string {
	ts := t.UTC().Format("2006-01-02T150405")
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	return base + "-" + ts + ext
}
