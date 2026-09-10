package cmd

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/internal/scheduler"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/logger"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// timeNow is a function variable for getting the current time.
// It can be overridden in tests for deterministic behavior.
var timeNow = time.Now

type loggerKeyringAdapter struct {
	log logger.Logger
}

func (l *loggerKeyringAdapter) Warning(format string, args ...interface{}) {
	l.log.Warning(format, args...)
}

func daemonTransferHandlers(pool *server.Pool, uid string) *warplib.Handlers {
	if pool == nil {
		return &warplib.Handlers{}
	}
	return &warplib.Handlers{
		DownloadProgressHandler: func(hash string, nread int) {
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		ResumeProgressHandler: func(hash string, nread int) {
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.ResumeProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		DownloadCompleteHandler: func(hash string, tread int64) {
			pool.BroadcastTerminal(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadComplete,
				Value:      tread,
				Hash:       hash,
			}))
		},
		DownloadStoppedHandler: func() {
			pool.BroadcastTerminal(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadStopped,
			}))
		},
		CompileStartHandler: func(hash string) {
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileStart,
				Hash:       hash,
			}))
		},
		CompileProgressHandler: func(hash string, nread int) {
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		CompileCompleteHandler: func(hash string, tread int64) {
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileComplete,
				Value:      tread,
				Hash:       hash,
			}))
		},
	}
}

func scheduledTransferInFlight(m *warplib.Manager, item *warplib.Item, hash string) bool {
	if queue := m.GetQueue(); queue != nil {
		if queue.IsActive(hash) || queue.IsWaiting(hash) {
			return true
		}
	}
	if item == nil || !item.IsDownloading() {
		return false
	}
	snapshot := item.Snapshot()
	return snapshot.TotalSize < 0 || snapshot.Downloaded < snapshot.TotalSize
}

// DaemonComponents holds all initialized daemon components.
// This allows for unified initialization and cleanup across
// console mode and Windows service mode.
type DaemonComponents struct {
	CookieManager   *credman.CookieManager
	TokenManager    *credman.TokenManager
	FlowRegistry    *auth.FlowRegistry
	ExtEngine       *extl.Engine
	Manager         *warplib.Manager
	Api             *api.Api
	Server          *server.Server
	Scheduler       *scheduler.Scheduler
	schedulerCancel context.CancelFunc
	logger          logger.Logger
	stdLogger       interface{ Println(v ...interface{}) }
}

// Close releases all daemon component resources within the normal graceful
// shutdown timeout.
func (c *DaemonComponents) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), common.ShutdownTimeout)
	defer cancel()
	return c.CloseContext(ctx)
}

// CloseContext first closes transfer admission, cancels every active transfer,
// and waits for queue/scheduler/transfer callbacks to drain. Persistent state
// and shared dependencies are closed only after that drain is proven complete;
// callers may retry with a fresh context after a timeout.
func (c *DaemonComponents) CloseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var closeErr error
	if c.stdLogger != nil {
		c.stdLogger.Println("Shutting down daemon...")
	}

	if c.Api != nil {
		c.Api.BeginShutdown()
	}

	var queue *warplib.QueueManager
	if c.Manager != nil {
		queue = c.Manager.GetQueue()
		if queue != nil {
			// Make preservation visible before transfer cancellation can let a
			// queue finalizer release its active slot.
			queue.BeginQuiesce()
		}
	}

	// Stop future scheduler callbacks before cancelling current transfer work.
	if c.schedulerCancel != nil {
		c.schedulerCancel()
	}

	// Close manager admission before invalidating every Item allocation.
	// StopDownload is intentionally unconditional: it also invalidates an
	// in-flight reconstruction that has not published its allocation yet.
	if c.Manager != nil {
		c.Manager.CancelTransfers()
		for _, item := range c.Manager.GetItems() {
			if c.stdLogger != nil && item.IsDownloading() {
				c.stdLogger.Println("Stopping download:", item.Hash)
			}
			if err := item.StopDownload(); err != nil &&
				!errors.Is(err, warplib.ErrItemDownloaderNotFound) {
				if c.stdLogger != nil {
					c.stdLogger.Println("Failed to stop download:", item.Hash, err)
				}
				closeErr = errors.Join(closeErr, fmt.Errorf("stop download %s: %w", item.Hash, err))
			}
		}
	}

	var drainErr error
	if queue != nil {
		if err := queue.QuiesceContext(ctx); err != nil {
			drainErr = errors.Join(drainErr, fmt.Errorf("drain queue callbacks: %w", err))
		}
	}
	if c.Scheduler != nil && c.schedulerCancel != nil {
		select {
		case <-c.Scheduler.Done():
		case <-ctx.Done():
			drainErr = errors.Join(drainErr, fmt.Errorf("drain scheduler callbacks: %w", ctx.Err()))
		}
	}
	if c.Api != nil {
		if err := c.Api.WaitBackground(ctx); err != nil {
			drainErr = errors.Join(drainErr, fmt.Errorf("drain API background work: %w", err))
		}
	}
	if c.Manager != nil {
		if err := c.Manager.WaitTransfers(ctx); err != nil {
			drainErr = errors.Join(drainErr, fmt.Errorf("drain transfers: %w", err))
		}
	}
	if drainErr != nil {
		if c.stdLogger != nil {
			c.stdLogger.Println("Daemon shutdown incomplete:", drainErr)
		}
		return errors.Join(closeErr, drainErr)
	}

	// Close API (closes manager, flushes state)
	if c.Api != nil {
		if err := c.Api.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close API: %w", err))
		}
	} else if c.Manager != nil {
		if err := c.Manager.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close manager: %w", err))
		}
	}

	// Close extension engine
	if c.ExtEngine != nil {
		if err := c.ExtEngine.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close extension engine: %w", err))
		}
	}

	// Shut down OAuth flow registry (cancels any in-flight flows).
	if c.FlowRegistry != nil {
		c.FlowRegistry.Shutdown()
	}

	// Close token manager (flushes encrypted state).
	if c.TokenManager != nil {
		if err := c.TokenManager.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close token manager: %w", err))
		}
	}

	// Close cookie manager
	if c.CookieManager != nil {
		if err := c.CookieManager.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close cookie manager: %w", err))
		}
	}

	if c.stdLogger != nil {
		c.stdLogger.Println("Daemon stopped")
	}
	return closeErr
}

// initDaemonComponents initializes all daemon components with the provided logger.
// This is the shared initialization used by both console mode and Windows service mode.
// maxConcurrent sets the maximum concurrent downloads (0 = unlimited).
// Returns the initialized components or an error if initialization fails.
//
// On error, any partially initialized components are cleaned up before returning.
var initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
	stdLog := logger.ToStdLogger(log)

	// Resolve the shared credman master key (env or keyring) once so
	// CookieManager and TokenManager encrypt with the same material.
	credKey, err := getCredmanKey(log)
	if err != nil {
		return nil, err
	}

	// Initialize cookie manager
	cookieFile := filepath.Join(warplib.ConfigDir, "cookies.warp")
	cm, err := credman.NewCookieManager(cookieFile, credKey)
	if err != nil {
		log.Error("Cookie manager initialization failed: %v", err)
		return nil, err
	}

	// Initialize token manager (OAuth2 plugin credentials).
	tokenFile := filepath.Join(warplib.ConfigDir, "tokens.gob")
	tm, err := credman.NewTokenManager(tokenFile, credKey)
	if err != nil {
		log.Error("Token manager initialization failed: %v", err)
		cm.Close()
		return nil, err
	}

	// Flow registry for in-flight OAuth flows (PKCE + device code).
	// 5-minute per-flow timeout matches typical IdP code-expiration windows.
	flows := auth.NewFlowRegistry(5 * time.Minute)

	// Initialize extension engine
	elEng, err := extl.NewEngine(stdLog, cm, tm, flows, false)
	if err != nil {
		log.Error("Extension engine initialization failed: %v", err)
		flows.Shutdown()
		_ = tm.Close()
		cm.Close()
		return nil, err
	}

	// Create HTTP client with cookie jar
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Error("Cookie jar creation failed: %v", err)
		elEng.Close()
		flows.Shutdown()
		_ = tm.Close()
		cm.Close()
		return nil, err
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects),
	}

	// Initialize warplib manager
	m, err := warplib.InitManager()
	if err != nil {
		log.Error("WarpLib manager initialization failed: %v", err)
		elEng.Close()
		flows.Shutdown()
		_ = tm.Close()
		cm.Close()
		return nil, err
	}

	// Queue setup is deferred until after the Server is constructed
	// (below) so onStartDownload can capture the pool and broadcast
	// errors back to any connected CLI. Wiring it before the server
	// existed meant failures were silent: only the daemon console saw
	// them; the CLI kept polling at 0 B/s forever.

	// Create SchemeRouter for protocol dispatch (http, https, ftp, ftps)
	router := warplib.NewSchemeRouter(client)
	m.SetSchemeRouter(router)

	// Initialize scheduler for --start-at scheduling.
	//
	// schedPool is nil during startup (missed-schedule replay) and gets
	// populated below once the server exists. Closures capture it by
	// reference so trigger callbacks that fire AFTER the server is up
	// will see a non-nil pool and broadcast failures to any connected
	// CLI. Nil-safe: ReportAsyncDownloadError does nothing useful with
	// a nil pool — we guard the call site instead of unsafely passing
	// nil and crashing.
	var schedPool *server.Pool
	decorateTransferHandlers := func(
		_ string,
		_ *server.TransferGeneration,
		handlers *warplib.Handlers,
	) (*warplib.Handlers, func(error)) {
		return handlers, func(error) {}
	}
	transferResultReporter := func(string, *server.TransferGeneration) func(error) {
		return func(error) {}
	}
	startManagedItem := func(
		hash string,
		forceFresh bool,
		source string,
		transientHeaders warplib.Headers,
		activation *warplib.QueueActivation,
	) {
		queueActivationValid := func() bool {
			queue := m.GetQueue()
			if activation == nil {
				return queue == nil
			}
			return queue != nil && queue.IsActivationCurrent(*activation)
		}
		if activation != nil {
			queue := m.GetQueue()
			if queue == nil || !queue.ClaimActivation(*activation) {
				log.Info("%s: ignored cancelled queue activation for %s", source, hash)
				return
			}
		}
		var managedGeneration *server.TransferGeneration
		if schedPool != nil {
			managedGeneration, _ = schedPool.CurrentGeneration(hash)
			if managedGeneration == nil {
				managedGeneration, _ = schedPool.BeginDownload(hash, nil)
				if managedGeneration == nil {
					// A concurrent registration may have won between the
					// lookup and reservation attempt. Capture that exact
					// generation rather than falling back to hash cleanup.
					managedGeneration, _ = schedPool.CurrentGeneration(hash)
				}
			}
		}
		runIdentityValid := func() bool {
			return queueActivationValid() &&
				(schedPool == nil ||
					(managedGeneration != nil && managedGeneration.IsRunnable()))
		}
		finishActivation := func(runErr error) {
			if activation == nil {
				return
			}
			queue := m.GetQueue()
			if queue == nil {
				return
			}
			stopped := runErr == nil
			if item := m.GetItem(hash); item != nil {
				snapshot := item.Snapshot()
				stopped = runErr == nil &&
					(snapshot.TotalSize < 0 || snapshot.Downloaded < snapshot.TotalSize)
			}
			if stopped {
				queue.StopActivation(*activation)
				return
			}
			queue.FinishActivation(*activation)
		}
		finishRun := func(runErr error) {
			transferResultReporter(hash, managedGeneration)(runErr)
			api.FinishManagedTransfer(managedGeneration, runErr)
			finishActivation(runErr)
		}
		// A queued start callback is invoked outside the queue lock. Cancellation
		// can therefore invalidate its exact activation before reconstruction.
		if !runIdentityValid() {
			log.Info("%s: ignored cancelled queue activation for %s", source, hash)
			finishRun(nil)
			return
		}

		item := m.GetItem(hash)
		if item == nil {
			err := fmt.Errorf("%s: item %s not found", source, hash)
			log.Error("%v", err)
			finishRun(err)
			return
		}

		hasParts := item.HasParts()
		scheduleInfo, scheduled := m.GetScheduleInfo(hash)
		if scheduled && !hasParts && scheduleInfo.CronExpr != "" {
			// Recurring occurrences use the newly persisted timestamped name,
			// so they must reconstruct rather than reuse the prior downloader.
			forceFresh = true
		}
		// Newly-created scheduled/queued items still have their original,
		// fully configured downloader in this process. Use it so its client
		// notification handlers are preserved. Restored items have no dAlloc
		// and are reconstructed below.
		if !forceFresh && !hasParts && item.IsDownloading() {
			if !runIdentityValid() {
				finishRun(nil)
				return
			}
			runLease, leaseErr := m.AcquireRunLease(hash)
			if leaseErr != nil {
				leaseErr = warplib.NormalizeTransferError(m.TransferContext(), leaseErr)
				if leaseErr == nil {
					finishRun(nil)
					return
				}
				log.Error("%s: claim exact allocation for %s: %v", source, hash, leaseErr)
				finishRun(leaseErr)
				return
			}
			if !runIdentityValid() {
				if abandonErr := runLease.Abandon(); abandonErr != nil {
					log.Error("%s: abandon stale run claim for %s: %v", source, hash, abandonErr)
				}
				finishRun(nil)
				return
			}
			if !m.GoTransfer(func(ctx context.Context) {
				runErr := warplib.NormalizeTransferError(ctx, runLease.StartContext(ctx))
				if runErr != nil {
					runErr = errors.Join(runErr, runLease.Close())
					log.Error("%s: Start failed for %s: %v", source, hash, runErr)
				}
				finishRun(runErr)
			}) {
				log.Info("%s: transfer admission closed for %s", source, hash)
				if closeErr := runLease.Close(); closeErr != nil {
					log.Error("%s: close unstarted allocation for %s: %v", source, hash, closeErr)
				}
				finishRun(nil)
			}
			return
		}

		fresh := forceFresh || !hasParts
		transferCtx := m.TransferContext()
		baseHandlers := daemonTransferHandlers(schedPool, hash)
		if managedHandlers := api.ManagedTransferHandlers(managedGeneration, m); managedHandlers != nil {
			baseHandlers = managedHandlers
		}
		handlers, reportReturnedError := decorateTransferHandlers(
			hash,
			managedGeneration,
			baseHandlers,
		)
		_, reconstruction, err := m.ResumeDownloadWithLease(client, hash, &warplib.ResumeDownloadOpts{
			Fresh:            fresh,
			TransientHeaders: transientHeaders,
			Handlers:         handlers,
			CommitGuard: func() bool {
				return runIdentityValid()
			},
		})
		if err != nil {
			err = warplib.NormalizeTransferError(transferCtx, err)
			if err == nil || errors.Is(err, warplib.ErrManagerShuttingDown) {
				log.Info("%s: reconstruction stopped during shutdown for %s", source, hash)
				reportReturnedError(nil)
				api.FinishManagedTransfer(managedGeneration, nil)
				finishActivation(nil)
				return
			}
			if errors.Is(err, warplib.ErrReconstructionSuperseded) {
				log.Info("%s: reconstruction superseded for %s", source, hash)
				reportReturnedError(nil)
				api.FinishManagedTransfer(managedGeneration, nil)
				finishActivation(nil)
				return
			}
			log.Error("%s: reconstruct failed for %s: %v", source, hash, err)
			reportReturnedError(err)
			api.FinishManagedTransfer(managedGeneration, err)
			finishActivation(err)
			return
		}
		// Cancellation may win while ResumeDownload is probing and assigning
		// a fresh downloader. Detach that allocation before it can be
		// registered or launched.
		if !runIdentityValid() {
			_, _ = reconstruction.Close()
			reportReturnedError(nil)
			api.FinishManagedTransfer(managedGeneration, nil)
			finishActivation(nil)
			return
		}
		if !m.GoTransfer(func(ctx context.Context) {
			var runErr error
			if fresh {
				runErr = reconstruction.StartContext(ctx)
			} else {
				runErr = reconstruction.ResumeContext(ctx)
			}
			runErr = warplib.NormalizeTransferError(ctx, runErr)
			if runErr != nil {
				log.Error("%s: transfer failed for %s: %v", source, hash, runErr)
				_, closeErr := reconstruction.Close()
				runErr = errors.Join(runErr, closeErr)
			}
			finishRun(runErr)
		}) {
			_, closeErr := reconstruction.Close()
			if closeErr != nil {
				log.Error("%s: close unstarted reconstruction for %s: %v", source, hash, closeErr)
			}
			reportReturnedError(nil)
			api.FinishManagedTransfer(managedGeneration, nil)
			finishActivation(nil)
		}
	}

	schedCtx, schedCancel := context.WithCancel(context.Background())
	triggerFn := func(hash string) {
		log.Info("Scheduler triggered download: %s", hash)
		info, ok := m.GetScheduleInfo(hash)
		if !ok {
			log.Error("Scheduler trigger: item %s not found", hash)
			return
		}
		// A cancellation can race an event already removed from the heap.
		// Re-check persisted state at callback time before starting work.
		if info.State != warplib.ScheduleStateScheduled && info.State != warplib.ScheduleStateMissed {
			log.Info("Scheduler trigger ignored for %s in state %s", hash, info.State)
			return
		}

		now := timeNow()
		if info.CronExpr != "" {
			next, nextErr := scheduler.NextOccurrence(info.CronExpr, now)
			if nextErr != nil {
				log.Error("Scheduler trigger: invalid recurring schedule for %s: %v", hash, nextErr)
				_ = m.SetScheduleState(hash, warplib.ScheduleStateCancelled)
				return
			}
			// Persist the next occurrence before starting this one. A crash
			// during the transfer therefore cannot lose the recurrence, and
			// stop can still cancel it while the transfer is active.
			if err := m.ConfigureSchedule(hash, next, info.CronExpr, warplib.ScheduleStateScheduled); err != nil {
				log.Error("Scheduler trigger: persist next occurrence for %s: %v", hash, err)
				return
			}
		} else {
			triggered, transitionErr := m.SetScheduleStateIf(
				hash,
				warplib.ScheduleStateTriggered,
				warplib.ScheduleStateScheduled,
				warplib.ScheduleStateMissed,
			)
			if transitionErr != nil {
				log.Error("Scheduler trigger: persist triggered state for %s: %v", hash, transitionErr)
				return
			}
			if !triggered {
				log.Info("Scheduler trigger ignored after concurrent cancellation for %s", hash)
				return
			}
		}

		currentItem := m.GetItem(hash)
		if info.CronExpr != "" && scheduledTransferInFlight(m, currentItem, hash) {
			// The following recurrence is already persisted above. Do not
			// rename, reconstruct, or enqueue the same Item while its prior
			// occurrence is active (or still waiting for a queue slot).
			log.Info("Scheduler skipped overlapping occurrence for %s", hash)
			return
		}
		hasParts := currentItem != nil && currentItem.HasParts()
		freshOccurrence := info.CronExpr != "" && !hasParts

		// T068: For a fresh recurring occurrence, apply the timestamp before
		// reconstructing the downloader so Item.Name and the actual output
		// path cannot diverge. Partial recurring transfers keep their existing
		// name while resuming.
		if freshOccurrence && info.Name != "" {
			if err := m.RenameItem(hash, daemonApplyTimestampSuffix(info.Name, now)); err != nil {
				log.Error("Scheduler trigger: persist occurrence filename for %s: %v", hash, err)
				return
			}
		}

		if queue := m.GetQueue(); queue != nil {
			// Scheduled work participates in the same concurrency limit as
			// immediate downloads. A paused/full queue persists it as waiting;
			// an available slot invokes the normal queue reconstruction path.
			queue.Add(hash, warplib.PriorityNormal)
			return
		}
		startManagedItem(hash, freshOccurrence, "scheduler trigger", nil, nil)
	}
	sched := scheduler.New(schedCtx, triggerFn)

	// Load schedules: detect missed (daemon was down) and future events.
	allItems := make(warplib.ItemsMap)
	for _, item := range m.GetItems() {
		allItems[item.Hash] = item
	}
	missed, future := scheduler.LoadSchedules(allItems, timeNow())

	// Create API
	s, err := api.NewApi(stdLog, m, client, elEng, router, sched,
		currentBuildArgs.Version, currentBuildArgs.Commit, currentBuildArgs.BuildType)
	if err != nil {
		log.Error("API initialization failed: %v", err)
		schedCancel()
		m.Close()
		elEng.Close()
		flows.Shutdown()
		_ = tm.Close()
		cm.Close()
		return nil, err
	}

	// Create server
	serv := server.NewServer(stdLog, m, DEF_PORT, client, router, rpcCfg)
	s.RegisterHandlers(serv)
	decorateTransferHandlers = serv.DecorateTransferHandlersForGeneration
	transferResultReporter = serv.TransferResultReporterForGeneration

	// Now that the pool exists, wire up the queue's auto-start callback so
	// any failure surfaces to both logs.txt (via item.Start/Resume) and
	// the CLI (via pool.Broadcast). Without broadcasting the CLI would
	// poll progress forever, never learning the download never actually
	// began.
	// Populate the late-bound scheduler pool reference so trigger
	// callbacks firing after daemon startup can broadcast failures.
	schedPool = serv.Pool()

	if maxConcurrent >= 0 {
		onStartDownload := func(activation warplib.QueueActivation) {
			hash := activation.Hash()
			startManagedItem(hash, false, "queue auto-start", nil, &activation)
			log.Info("Queue auto-started download: %s", hash)
		}
		m.SetMaxConcurrentDownloadsWithActivation(maxConcurrent, onStartDownload)
		if maxConcurrent == 0 {
			log.Info("Download queue enabled with unlimited concurrency")
		} else {
			log.Info("Download queue enabled: max %d concurrent", maxConcurrent)
		}
	}

	// A one-shot trigger is persisted before it enters the queue. If the
	// daemon exits in that window, scheduler.LoadSchedules intentionally
	// ignores it; restore it explicitly unless it already completed.
	for _, item := range m.GetItems() {
		snapshot := item.Snapshot()
		if snapshot.ScheduleState != warplib.ScheduleStateTriggered {
			continue
		}
		if snapshot.TotalSize >= 0 && snapshot.Downloaded >= snapshot.TotalSize {
			continue
		}
		if queue := m.GetQueue(); queue != nil {
			queue.Add(snapshot.Hash, warplib.PriorityNormal)
		} else {
			startManagedItem(snapshot.Hash, false, "triggered restart", nil, nil)
		}
	}

	// Restore schedules only after queue configuration, so both missed
	// catch-up work and near-term future triggers honor maxConcurrent.
	for _, event := range future {
		if err := m.ConfigureSchedule(event.ItemHash, event.TriggerAt, event.CronExpr, warplib.ScheduleStateScheduled); err != nil {
			log.Error("Restore schedule: persist %s: %v", event.ItemHash, err)
			continue
		}
		sched.Add(event)
		log.Info("Restored scheduled download: %s at %s", event.ItemHash, event.TriggerAt.Format("2006-01-02 15:04"))
	}
	// T051: Enqueue missed items immediately and log notification.
	for _, event := range missed {
		info, ok := m.GetScheduleInfo(event.ItemHash)
		if !ok {
			continue
		}
		if info.CronExpr == "" {
			_ = m.SetScheduleState(event.ItemHash, warplib.ScheduleStateMissed)
		}
		log.Info("Missed schedule: %s, starting catch-up occurrence", info.Name)
		triggerFn(event.ItemHash)
	}

	return &DaemonComponents{
		CookieManager:   cm,
		TokenManager:    tm,
		FlowRegistry:    flows,
		ExtEngine:       elEng,
		Manager:         m,
		Api:             s,
		Server:          serv,
		Scheduler:       sched,
		schedulerCancel: schedCancel,
		logger:          log,
		stdLogger:       stdLog,
	}, nil
}

// getCookieManagerWithLogger initializes the cookie manager using the Logger interface.
// This is used in service mode where cli.Context is not available.
func getCookieManagerWithLogger(log logger.Logger) (*credman.CookieManager, error) {
	key, err := getCredmanKey(log)
	if err != nil {
		return nil, err
	}
	cookieFile := filepath.Join(warplib.ConfigDir, "cookies.warp")
	cm, err := credman.NewCookieManager(cookieFile, key)
	if err != nil {
		log.Error("Cookie manager initialization failed: %v", err)
		return nil, err
	}
	return cm, nil
}

// getCredmanKey resolves the master credential-encryption key used by
// both CookieManager and TokenManager. Env var wins; otherwise fall
// back to the OS keyring, generating a new key on first use.
func getCredmanKey(log logger.Logger) ([]byte, error) {
	if keyHex := os.Getenv(cookieKeyEnv); keyHex != "" {
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			log.Error("Invalid cookie key hex: %v", err)
			return nil, err
		}
		return key, nil
	}
	kr := newKeyring(warplib.ConfigDir, &loggerKeyringAdapter{log: log})
	key, err := kr.GetKey()
	if err != nil {
		key, err = kr.SetKey()
		if err != nil {
			log.Error("Keyring initialization failed: %v", err)
			return nil, err
		}
	}
	return key, nil
}

// daemonApplyTimestampSuffix adds a per-occurrence timestamp suffix to a filename
// before the last extension for recurring scheduled downloads.
// Format: <basename>-<YYYY-MM-DDTHHMMSS>.<ext>
func daemonApplyTimestampSuffix(filename string, t time.Time) string {
	ts := t.UTC().Format("2006-01-02T150405")
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	// Replace the previous occurrence suffix instead of accumulating one on
	// every recurring run (name-<ts>-<ts>.ext).
	const timestampLen = len("2006-01-02T150405")
	if len(base) > timestampLen && base[len(base)-timestampLen-1] == '-' {
		previous := base[len(base)-timestampLen:]
		if _, err := time.Parse("2006-01-02T150405", previous); err == nil {
			base = base[:len(base)-timestampLen-1]
		}
	}
	return base + "-" + ts + ext
}
