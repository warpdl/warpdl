package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/adhocore/gronx"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/cookies"
	"github.com/warpdl/warpdl/internal/scheduler"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func (s *Api) downloadHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.DownloadParams
	if err := json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}

	dlURL, err := s.elEngine.Extract(m.Url)
	if err != nil {
		s.log.Printf("failed to extract URL from extension: %s\n", err.Error())
		dlURL = m.Url
	}

	// Detect scheme to choose code path
	parsed, parseErr := url.Parse(dlURL)
	if parseErr != nil {
		return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid URL: %w", parseErr)
	}
	scheme := strings.ToLower(parsed.Scheme)

	switch scheme {
	case "ftp", "ftps", "sftp":
		return s.downloadProtocolHandler(sconn, pool, dlURL, scheme, &m)
	default:
		return s.downloadHTTPHandler(sconn, pool, dlURL, &m)
	}
}

// downloadHTTPHandler handles HTTP and HTTPS downloads.
// This is the existing HTTP download logic extracted from downloadHandler — zero logic changes.
func (s *Api) downloadHTTPHandler(sconn *server.SyncConn, pool *server.Pool, dlURL string, m *common.DownloadParams) (common.UpdateType, any, error) {
	// Determine which client to use based on proxy setting
	dlClient := s.client
	if m.Proxy != "" {
		var err error
		dlClient, err = warplib.NewHTTPClientWithProxy(m.Proxy)
		if err != nil {
			return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		// Preserve cookie jar from default client
		if s.client.Jar != nil {
			dlClient.Jar = s.client.Jar
		}
	}

	var d *warplib.Downloader

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
	var err error
	if m.SpeedLimit != "" {
		speedLimit, err = warplib.ParseSpeedLimit(m.SpeedLimit)
		if err != nil {
			return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid speed limit: %w", err)
		}
	}

	// Import cookies if requested
	if m.CookiesFrom != "" {
		parsedURL, urlErr := url.Parse(dlURL)
		if urlErr == nil {
			domain := parsedURL.Hostname()
			var importedCookies []cookies.Cookie
			var source *cookies.CookieSource
			var cookieErr error

			if m.CookiesFrom == "auto" {
				importedCookies, source, cookieErr = cookies.DetectBrowserCookies(domain)
				if cookieErr == nil {
					s.log.Printf("Auto-detected %s cookie store\n", source.Browser)
				}
			} else {
				importedCookies, source, cookieErr = cookies.ImportCookies(m.CookiesFrom, domain)
			}

			if cookieErr != nil {
				s.log.Printf("warning: failed to import cookies: %s\n", cookieErr.Error())
			} else if len(importedCookies) > 0 {
				cookieHeader := cookies.BuildCookieHeader(importedCookies)
				m.Headers.Update("Cookie", cookieHeader)
				s.log.Printf("Imported %d cookies for %s from %s\n", len(importedCookies), domain, source.Browser)
			}
		}
	}

	d, err = warplib.NewDownloader(dlClient, dlURL, &warplib.DownloaderOpts{
		Headers:           m.Headers,
		ForceParts:        m.ForceParts,
		FileName:          m.FileName,
		DownloadDirectory: m.DownloadDirectory,
		MaxConnections:    m.MaxConnections,
		MaxSegments:       m.MaxSegments,
		Overwrite:         m.Overwrite,
		RetryConfig:       retryConfig,
		RequestTimeout:    requestTimeout,
		SpeedLimit:        speedLimit,
		Handlers: &warplib.Handlers{
			ErrorHandler: func(_ string, err error) {
				if errors.Is(err, context.Canceled) && d.IsStopped() {
					return
				}
				uid := d.GetHash()
				pool.Broadcast(uid, server.InitError(err))
				pool.WriteError(uid, server.ErrorTypeCritical, err.Error())
				pool.StopDownload(uid)
				s.manager.GetItem(uid).StopDownload()
			},
			DownloadProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadProgress,
					Value:      int64(nread),
					Hash:       hash,
				}))
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadComplete,
					Value:      tread,
					Hash:       hash,
				}))
			},
			DownloadStoppedHandler: func() {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadStopped,
				}))
			},
			CompileStartHandler: func(hash string) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileStart,
					Hash:       hash,
				}))
			},
			CompileProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileProgress,
					Value:      int64(nread),
					Hash:       hash,
				}))
			},
			CompileCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileComplete,
					Value:      tread,
					Hash:       hash,
				}))
			},
		},
	})
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}
	pool.AddDownload(d.GetHash(), sconn)
	err = s.manager.AddDownload(d, &warplib.AddDownloadOpts{
		ChildHash:        m.ChildHash,
		IsHidden:         m.IsHidden,
		IsChildren:       m.IsChildren,
		AbsoluteLocation: d.GetDownloadDirectory(),
		Priority:         warplib.Priority(m.Priority),
	})
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}

	// Store cookie source path on item for re-import on resume
	if m.CookiesFrom != "" {
		item := s.manager.GetItem(d.GetHash())
		if item != nil {
			item.CookieSourcePath = m.CookiesFrom
			s.manager.UpdateItem(item)
		}
	}

	// T067/T068: Apply scheduling if Schedule (cron) is set.
	// Schedule takes priority for triggering; StartAt/StartIn can specify the first occurrence.
	if m.Schedule != "" {
		item := s.manager.GetItem(d.GetHash())
		if item != nil {
			item.CronExpr = m.Schedule

			// Determine first trigger time:
			// If StartAt is also set, use it; otherwise compute from cron expression.
			var firstTrigger time.Time
			if m.StartAt != "" {
				t, parseErr := time.ParseInLocation("2006-01-02 15:04", m.StartAt, time.Local)
				if parseErr == nil && t.After(time.Now()) {
					firstTrigger = t
				}
			}
			if firstTrigger.IsZero() {
				next, cronErr := gronx.NextTickAfter(m.Schedule, time.Now(), false)
				if cronErr == nil {
					firstTrigger = next
				}
			}

			if !firstTrigger.IsZero() {
				item.ScheduledAt = firstTrigger
				item.ScheduleState = warplib.ScheduleStateScheduled
				s.manager.UpdateItem(item)
				// Add to scheduler heap
				if s.scheduler != nil {
					s.scheduler.Add(scheduler.ScheduleEvent{
						ItemHash:  item.Hash,
						TriggerAt: firstTrigger,
						CronExpr:  m.Schedule,
					})
				}
				// Do NOT start the download; the scheduler will trigger it.
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
		}
	}

	// Apply scheduling if StartAt is set (one-shot schedule)
	if m.StartAt != "" {
		scheduledAt, parseErr := time.ParseInLocation("2006-01-02 15:04", m.StartAt, time.Local)
		if parseErr == nil {
			item := s.manager.GetItem(d.GetHash())
			if item != nil {
				item.ScheduledAt = scheduledAt
				item.ScheduleState = warplib.ScheduleStateScheduled
				s.manager.UpdateItem(item)
				// Add to scheduler if available
				if s.scheduler != nil {
					s.scheduler.Add(scheduler.ScheduleEvent{
						ItemHash:  item.Hash,
						TriggerAt: scheduledAt,
					})
				}
			}
			// Do NOT start the download; the scheduler will trigger it.
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
	}

	// todo: handle download start error
	go d.Start()
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

	// Create protocol downloader via SchemeRouter
	pd, err := s.schemeRouter.NewDownloader(rawURL, &warplib.DownloaderOpts{
		FileName:          m.FileName,
		DownloadDirectory: m.DownloadDirectory,
		SSHKeyPath:        m.SSHKeyPath,
	})
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}

	// Probe to get file metadata
	probe, err := pd.Probe(context.Background())
	if err != nil {
		pd.Close()
		return common.UPDATE_DOWNLOAD, nil, err
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

	// Build handlers for protocol download (no compile handlers — single-stream protocols)
	handlers := &warplib.Handlers{
		ErrorHandler: func(_ string, err error) {
			if errors.Is(err, context.Canceled) && pd.IsStopped() {
				return
			}
			uid := pd.GetHash()
			pool.Broadcast(uid, server.InitError(err))
			pool.WriteError(uid, server.ErrorTypeCritical, err.Error())
			pool.StopDownload(uid)
			s.manager.GetItem(uid).StopDownload()
		},
		DownloadProgressHandler: func(hash string, nread int) {
			uid := pd.GetHash()
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		DownloadCompleteHandler: func(hash string, tread int64) {
			uid := pd.GetHash()
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadComplete,
				Value:      tread,
				Hash:       hash,
			}))
		},
		DownloadStoppedHandler: func() {
			uid := pd.GetHash()
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadStopped,
			}))
		},
	}

	// Get credential-stripped URL for safe persistence
	cleanURL := warplib.StripURLCredentials(rawURL)

	pool.AddDownload(pd.GetHash(), sconn)
	err = s.manager.AddProtocolDownload(pd, probe, cleanURL, proto, handlers, &warplib.AddDownloadOpts{
		ChildHash:        m.ChildHash,
		IsHidden:         m.IsHidden,
		IsChildren:       m.IsChildren,
		AbsoluteLocation: pd.GetDownloadDirectory(),
		Priority:         warplib.Priority(m.Priority),
		SSHKeyPath:       m.SSHKeyPath,
	})
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}

	// Start protocol download in background
	go pd.Download(context.Background(), handlers)

	return common.UPDATE_DOWNLOAD, &common.DownloadResponse{
		ContentLength:     pd.GetContentLength(),
		DownloadId:        pd.GetHash(),
		FileName:          pd.GetFileName(),
		SavePath:          pd.GetSavePath(),
		DownloadDirectory: pd.GetDownloadDirectory(),
		MaxConnections:    pd.GetMaxConnections(),
		MaxSegments:       pd.GetMaxParts(),
	}, nil
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
