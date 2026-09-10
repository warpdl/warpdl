package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/jhttp"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// Custom JSON-RPC error codes for download operations.
const (
	codeDownloadNotFound  = jrpc2.Code(-32001)
	codeDownloadNotActive = jrpc2.Code(-32002)
	codeInvalidParams     = jrpc2.Code(-32602)
)

// RPCConfig holds configuration for the JSON-RPC endpoint.
type RPCConfig struct {
	ListenAll bool   // If true, bind to 0.0.0.0 instead of 127.0.0.1
	Version   string // Daemon version
	Commit    string // Git commit
	BuildType string // Build type
}

// RPCServer manages the JSON-RPC 2.0 bridge and method handlers.
//
// The /jsonrpc and /jsonrpc/ws routes are unauthenticated. The daemon
// listens on 127.0.0.1 by default; --rpc-listen-all is the explicit
// opt-in to bind on all interfaces, which the operator should only use
// behind a separate authentication layer (reverse proxy, etc.).
type RPCServer struct {
	bridge       jhttp.Bridge
	version      string
	commit       string
	buildType    string
	manager      *warplib.Manager
	client       *http.Client
	pool         *Pool
	schemeRouter *warplib.SchemeRouter
	notifier     *RPCNotifier
	log          *log.Logger

	transferMu        sync.Mutex
	transferTerminals map[string]*atomic.Bool

	transferLauncher func(func(context.Context)) bool
}

// logf writes to the daemon log if one is configured. Safe on a zero-value
// RPCServer (tests construct &RPCServer{} directly).
func (rs *RPCServer) logf(format string, args ...any) {
	if rs.log != nil {
		rs.log.Printf(format, args...)
	}
}

func (rs *RPCServer) broadcastError(gid, msg string) {
	if rs.notifier == nil {
		return
	}
	rs.notifier.Broadcast("download.error", &DownloadErrorNotification{
		GID:   gid,
		Error: msg,
	})
}

func (rs *RPCServer) launchTransfer(fn func(context.Context)) bool {
	if rs.transferLauncher != nil {
		return rs.transferLauncher(fn)
	}
	return rs.manager != nil && rs.manager.GoTransfer(fn)
}

func normalizeServerTransferError(ctx context.Context, err error) error {
	err = warplib.NormalizeTransferError(ctx, err)
	if err == nil || ctx == nil || ctx.Err() == nil {
		return err
	}
	if isServerShutdownTransferError(err) {
		return nil
	}
	return err
}

func isServerShutdownTransferError(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isServerShutdownTransferError(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			return isServerShutdownTransferError(child)
		}
	}
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, warplib.ErrItemDownloaderNotFound) ||
		errors.Is(err, warplib.ErrReconstructionSuperseded) ||
		errors.Is(err, warplib.ErrManagerShuttingDown)
}

func (rs *RPCServer) registerTransferTerminal(hash string, terminal *atomic.Bool) {
	if rs == nil || hash == "" || terminal == nil {
		return
	}
	rs.transferMu.Lock()
	if rs.transferTerminals == nil {
		rs.transferTerminals = make(map[string]*atomic.Bool)
	}
	rs.transferTerminals[hash] = terminal
	rs.transferMu.Unlock()
}

func (rs *RPCServer) transferTerminal(hash string) *atomic.Bool {
	rs.transferMu.Lock()
	defer rs.transferMu.Unlock()
	if rs.transferTerminals == nil {
		rs.transferTerminals = make(map[string]*atomic.Bool)
	}
	if terminal := rs.transferTerminals[hash]; terminal != nil {
		return terminal
	}
	terminal := &atomic.Bool{}
	rs.transferTerminals[hash] = terminal
	return terminal
}

func (rs *RPCServer) unregisterTransferTerminal(hash string, terminal *atomic.Bool) {
	if rs == nil || terminal == nil {
		return
	}
	rs.transferMu.Lock()
	if rs.transferTerminals[hash] == terminal {
		delete(rs.transferTerminals, hash)
	}
	rs.transferMu.Unlock()
}

func (rs *RPCServer) clearTransferTerminal(hash string) {
	if rs == nil {
		return
	}
	rs.transferMu.Lock()
	delete(rs.transferTerminals, hash)
	rs.transferMu.Unlock()
}

func (rs *RPCServer) returnedErrorReporter(hash string) func(error) {
	return rs.returnedErrorReporterForGeneration(hash, nil)
}

func (rs *RPCServer) returnedErrorReporterForGeneration(
	hash string,
	generation *TransferGeneration,
) func(error) {
	rs.transferMu.Lock()
	terminal := rs.transferTerminals[hash]
	rs.transferMu.Unlock()
	if terminal == nil {
		return func(error) {}
	}
	return func(err error) {
		if rs.manager != nil {
			err = normalizeServerTransferError(rs.manager.TransferContext(), err)
		}
		runnable := generation == nil || generation.IsRunnable()
		if err != nil && runnable && terminal.CompareAndSwap(false, true) {
			rs.broadcastError(hash, err.Error())
		}
		if generation == nil || generation.IsCurrent() {
			rs.unregisterTransferTerminal(hash, terminal)
		}
	}
}

func (rs *RPCServer) cleanupDownloadRegistration(hash string, fallback interface{ Close() error }) error {
	var closeErr error
	if item := rs.manager.GetItem(hash); item != nil {
		closeErr = item.CloseDownloader()
	} else if fallback != nil {
		closeErr = fallback.Close()
	}
	rs.manager.ReleaseQueueSlot(hash)
	return errors.Join(closeErr, rs.manager.PurgeFailedDownload(hash))
}

func (rs *RPCServer) cleanupExactDownloadRegistration(
	hash string,
	fallback interface{ Close() error },
	terminal *atomic.Bool,
	generation *TransferGeneration,
) error {
	if terminal != nil {
		rs.unregisterTransferTerminal(hash, terminal)
	} else if generation == nil {
		rs.clearTransferTerminal(hash)
	}
	cleanupErr := rs.cleanupDownloadRegistration(hash, fallback)
	if rs.pool != nil {
		if generation != nil {
			generation.Abort()
		} else {
			rs.pool.StopDownload(hash)
		}
	}
	return cleanupErr
}

func transferGID(uid *string, callbackHash string) string {
	if uid != nil && *uid != "" {
		return *uid
	}
	return callbackHash
}

// rpcTransferHandlers keeps notifications and pool membership on the same
// lifecycle. Queue release on an error is deliberately deferred to
// finishAsyncTransfer: ErrorHandler can run while sibling workers are still
// draining, and promoting another queued transfer at that point would exceed
// the configured concurrency limit.
func (rs *RPCServer) rpcTransferHandlers(
	uid *string,
	terminalReported *atomic.Bool,
	poolGeneration *atomic.Pointer[TransferGeneration],
) *warplib.Handlers {
	return &warplib.Handlers{
		ErrorHandler: func(callbackHash string, err error) {
			if rs.manager != nil {
				err = normalizeServerTransferError(rs.manager.TransferContext(), err)
				if err == nil {
					return
				}
			}
			generation := loadTransferGeneration(poolGeneration)
			if generation != nil && !generation.IsRunnable() {
				return
			}
			hash := transferGID(uid, callbackHash)
			firstTerminal := terminalReported == nil || terminalReported.CompareAndSwap(false, true)
			if !firstTerminal {
				return
			}
			rs.broadcastError(hash, err.Error())
			if generation != nil {
				generation.WriteError(ErrorTypeCritical, err.Error())
				generation.RecordTerminal(MakeDownloadError(hash, err))
			} else if rs.pool != nil {
				rs.pool.WriteError(hash, ErrorTypeCritical, err.Error())
			}
		},
		DownloadProgressHandler: func(callbackHash string, nread int) {
			if terminalReported != nil && terminalReported.Load() {
				return
			}
			if generation := loadTransferGeneration(poolGeneration); generation != nil && !generation.IsRunnable() {
				return
			}
			if rs.notifier != nil {
				rs.notifier.Broadcast("download.progress", &DownloadProgressNotification{
					GID:             transferGID(uid, callbackHash),
					CompletedLength: int64(nread),
				})
			}
		},
		ResumeProgressHandler: func(callbackHash string, nread int) {
			if terminalReported != nil && terminalReported.Load() {
				return
			}
			if generation := loadTransferGeneration(poolGeneration); generation != nil && !generation.IsRunnable() {
				return
			}
			if rs.notifier != nil {
				rs.notifier.Broadcast("download.progress", &DownloadProgressNotification{
					GID:             transferGID(uid, callbackHash),
					CompletedLength: int64(nread),
				})
			}
		},
		DownloadCompleteHandler: func(callbackHash string, tread int64) {
			hash := transferGID(uid, callbackHash)
			generation := loadTransferGeneration(poolGeneration)
			if generation != nil && !generation.IsRunnable() {
				return
			}
			firstTerminal := terminalReported == nil || terminalReported.CompareAndSwap(false, true)
			if firstTerminal && rs.notifier != nil {
				rs.notifier.Broadcast("download.complete", &DownloadCompleteNotification{
					GID:         hash,
					TotalLength: tread,
				})
			}
			rs.manager.ReleaseQueueSlot(hash)
			if firstTerminal {
				frame := MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: hash,
					Action:     common.DownloadComplete,
					Value:      tread,
					Hash:       callbackHash,
				})
				if generation != nil {
					generation.RecordTerminal(frame)
				} else if rs.pool != nil {
					rs.pool.BroadcastTerminal(hash, frame)
				}
			}
		},
		DownloadStoppedHandler: func() {
			hash := transferGID(uid, "")
			generation := loadTransferGeneration(poolGeneration)
			if generation != nil && !generation.IsRunnable() {
				return
			}
			firstTerminal := terminalReported == nil || terminalReported.CompareAndSwap(false, true)
			rs.manager.ReleaseQueueSlot(hash)
			if firstTerminal {
				frame := MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: hash,
					Action:     common.DownloadStopped,
				})
				if generation != nil {
					generation.RecordTerminal(frame)
				} else if rs.pool != nil {
					rs.pool.BroadcastTerminal(hash, frame)
				}
			}
		},
	}
}

func loadTransferGeneration(pointer *atomic.Pointer[TransferGeneration]) *TransferGeneration {
	if pointer == nil {
		return nil
	}
	return pointer.Load()
}

// finishAsyncTransfer handles errors returned before or after worker callbacks
// and makes terminal cleanup idempotent for every RPC-launched transfer.
func (rs *RPCServer) finishAsyncTransfer(
	hash string,
	generation *TransferGeneration,
	lease *warplib.RunLease,
	runErr error,
	errorReported *atomic.Bool,
) {
	rs.finishAsyncTransferWithCleanup(hash, generation, runErr, errorReported, func() error {
		if lease != nil {
			return lease.Close()
		}
		if item := rs.manager.GetItem(hash); item != nil {
			return item.CloseDownloader()
		}
		return nil
	}, true)
}

func (rs *RPCServer) finishReconstructedTransfer(
	hash string,
	generation *TransferGeneration,
	lease *warplib.ReconstructionLease,
	runErr error,
	errorReported *atomic.Bool,
) {
	rs.finishAsyncTransferWithCleanup(hash, generation, runErr, errorReported, func() error {
		if lease == nil {
			return nil
		}
		_, err := lease.Close()
		return err
	}, false)
}

func (rs *RPCServer) finishAsyncTransferWithCleanup(
	hash string,
	generation *TransferGeneration,
	runErr error,
	errorReported *atomic.Bool,
	closeOnError func() error,
	purgeZeroProgress bool,
) {
	defer rs.unregisterTransferTerminal(hash, errorReported)
	if runErr != nil && closeOnError != nil {
		runErr = errors.Join(runErr, closeOnError())
	}
	if generation != nil && !generation.IsCurrent() {
		return
	}
	needsRPCError := runErr != nil &&
		(errorReported == nil || errorReported.CompareAndSwap(false, true))
	if runErr != nil {
		if needsRPCError {
			rs.broadcastError(hash, "download failed: "+runErr.Error())
		}
		if generation != nil {
			generation.WriteError(ErrorTypeCritical, runErr.Error())
		} else if rs.pool != nil {
			rs.pool.WriteError(hash, ErrorTypeCritical, runErr.Error())
		}
	}
	rs.manager.ReleaseQueueSlot(hash)
	if runErr != nil && purgeZeroProgress {
		if item := rs.manager.GetItem(hash); item != nil && item.GetDownloaded() == 0 {
			_ = rs.manager.PurgeFailedDownload(hash)
		}
	}
	// Publish terminal pool removal last. Tests and callers can use absence
	// from the pool as the point at which cleanup (including purge) finished.
	if rs.pool != nil {
		if generation != nil {
			var fallback []byte
			if runErr != nil {
				fallback = MakeDownloadError(hash, runErr)
			}
			generation.Finish(fallback)
		} else if runErr != nil {
			rs.pool.BroadcastTerminal(hash, MakeDownloadError(hash, runErr))
		} else {
			// A worker callback normally delivered the terminal frame. This
			// remains as cleanup for implementations that returned without
			// invoking any terminal callback.
			rs.pool.StopDownload(hash)
		}
	}
}

// VersionResult is the response for system.getVersion.
type VersionResult struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildType string `json:"buildType,omitempty"`
}

// AddParams is the input for download.add.
type AddParams struct {
	URL         string          `json:"url"`
	FileName    string          `json:"fileName,omitempty"`
	Dir         string          `json:"dir,omitempty"`
	Headers     warplib.Headers `json:"headers,omitempty"`
	Connections int32           `json:"connections,omitempty"`
	SSHKeyPath  string          `json:"sshKeyPath,omitempty"`
}

// AddResult is the response for download.add.
type AddResult struct {
	GID string `json:"gid"`
}

// GIDParam is a common input with just a download GID.
type GIDParam struct {
	GID string `json:"gid"`
}

// StatusResult is the response for download.status.
type StatusResult struct {
	GID             string `json:"gid"`
	Status          string `json:"status"`
	TotalLength     int64  `json:"totalLength"`
	CompletedLength int64  `json:"completedLength"`
	Percentage      int64  `json:"percentage"`
	FileName        string `json:"fileName"`
}

// ListParams is the input for download.list.
type ListParams struct {
	Status string `json:"status,omitempty"` // "active", "waiting", "complete", "all" (default)
}

// ListItem is a single entry in the download.list response.
type ListItem struct {
	GID             string `json:"gid"`
	Status          string `json:"status"`
	TotalLength     int64  `json:"totalLength"`
	CompletedLength int64  `json:"completedLength"`
	FileName        string `json:"fileName"`
}

// ListResult is the response for download.list.
type ListResult struct {
	Downloads []*ListItem `json:"downloads"`
}

// EmptyResult is a placeholder for methods that return no data.
type EmptyResult struct{}

// NewRPCServer creates a new RPCServer with method handlers and HTTP bridge.
func NewRPCServer(cfg *RPCConfig, m *warplib.Manager, client *http.Client, pool *Pool, router *warplib.SchemeRouter, l *log.Logger) *RPCServer {
	rs := &RPCServer{
		version:      cfg.Version,
		commit:       cfg.Commit,
		buildType:    cfg.BuildType,
		manager:      m,
		client:       client,
		pool:         pool,
		schemeRouter: router,
		notifier:     NewRPCNotifier(l),
		log:          l,
	}

	rs.bridge = jhttp.NewBridge(rs.methods(), nil)
	return rs
}

// methods returns the handler.Map used by both the HTTP bridge and WebSocket servers.
func (rs *RPCServer) methods() handler.Map {
	return handler.Map{
		"system.getVersion": handler.New(rs.systemGetVersion),
		"download.add":      handler.New(rs.downloadAdd),
		"download.pause":    handler.New(rs.downloadPause),
		"download.resume":   handler.New(rs.downloadResume),
		"download.remove":   handler.New(rs.downloadRemove),
		"download.status":   handler.New(rs.downloadStatus),
		"download.list":     handler.New(rs.downloadList),
	}
}

func (rs *RPCServer) systemGetVersion(_ context.Context) (*VersionResult, error) {
	return &VersionResult{
		Version:   rs.version,
		Commit:    rs.commit,
		BuildType: rs.buildType,
	}, nil
}

// downloadAdd creates a new download from a URL.
func (rs *RPCServer) downloadAdd(_ context.Context, p *AddParams) (*AddResult, error) {
	if p.URL == "" {
		return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "missing required param: url"}
	}

	parsed, err := url.Parse(p.URL)
	if err != nil {
		return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "invalid url"}
	}

	scheme := strings.ToLower(parsed.Scheme)
	queue := rs.manager.GetQueue()
	connections := p.Connections
	if connections <= 0 {
		connections = 24
	}

	transferCtx := rs.manager.TransferContext()
	opts := &warplib.DownloaderOpts{
		Context:           transferCtx,
		FileName:          p.FileName,
		DownloadDirectory: p.Dir,
		Headers:           p.Headers,
		MaxConnections:    connections,
		SSHKeyPath:        p.SSHKeyPath,
	}

	var (
		hash           string
		errorReported  atomic.Bool
		poolGeneration atomic.Pointer[TransferGeneration]
	)
	opts.Handlers = rs.rpcTransferHandlers(&hash, &errorReported, &poolGeneration)

	switch scheme {
	case "http", "https":
		d, err := warplib.NewDownloader(rs.client, p.URL, opts)
		if err != nil {
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: err.Error()}
		}
		hash = d.GetHash()
		if rs.pool != nil {
			generation, reserved := rs.pool.beginLegacyDownload(hash, nil)
			if !reserved {
				return nil, &jrpc2.Error{
					Code: codeDownloadNotActive,
					Message: errors.Join(
						errors.New("download is already running or still stopping"),
						d.Close(),
					).Error(),
				}
			}
			poolGeneration.Store(generation)
		}
		if err := rs.manager.AddDownload(d, &warplib.AddDownloadOpts{
			AbsoluteLocation: d.GetDownloadDirectory(),
			// Queue.Add can synchronously start an item. Defer registration
			// until the RPC has finished manager/pool bookkeeping.
			SkipQueue: queue != nil,
		}); err != nil {
			if generation := poolGeneration.Load(); generation != nil {
				generation.Abort()
			}
			return nil, &jrpc2.Error{
				Code:    codeInvalidParams,
				Message: errors.Join(err, d.Close()).Error(),
			}
		}
		var runLease *warplib.RunLease
		if queue == nil {
			runLease, err = rs.manager.AcquireDownloadRunLease(hash, d)
			if err != nil {
				if generation := poolGeneration.Load(); generation != nil {
					generation.Abort()
				}
				return nil, &jrpc2.Error{
					Code:    codeDownloadNotActive,
					Message: err.Error(),
				}
			}
		}
		rs.registerTransferTerminal(hash, &errorReported)
		if queue != nil {
			queue.Add(hash, warplib.PriorityNormal)
			if _, closeErr := rs.manager.CloseWaitingDownloader(hash); closeErr != nil {
				cleanupErr := rs.cleanupExactDownloadRegistration(
					hash,
					d,
					&errorReported,
					poolGeneration.Load(),
				)
				return nil, &jrpc2.Error{
					Code:    codeDownloadNotActive,
					Message: errors.Join(closeErr, cleanupErr).Error(),
				}
			}
		}
		if queue == nil {
			if !rs.launchTransfer(func(ctx context.Context) {
				runErr := normalizeServerTransferError(ctx, runLease.StartContext(ctx))
				rs.finishAsyncTransfer(
					hash,
					poolGeneration.Load(),
					runLease,
					runErr,
					&errorReported,
				)
			}) {
				closeErr := runLease.Close()
				cleanupErr := rs.cleanupExactDownloadRegistration(
					hash,
					d,
					&errorReported,
					poolGeneration.Load(),
				)
				return nil, &jrpc2.Error{
					Code: codeDownloadNotActive,
					Message: errors.Join(
						warplib.ErrManagerShuttingDown,
						closeErr,
						cleanupErr,
					).Error(),
				}
			}
		}
		if rs.notifier != nil {
			rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
				GID:         hash,
				FileName:    d.GetFileName(),
				TotalLength: d.GetContentLengthAsInt(),
			})
		}
		return &AddResult{GID: hash}, nil

	default:
		// FTP, FTPS, SFTP -- use SchemeRouter
		if rs.schemeRouter == nil {
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "unsupported scheme: " + scheme}
		}
		var protocolUsername string
		protocolCredentialsRequired := false
		if parsed.User != nil {
			protocolUsername = parsed.User.Username()
			_, protocolCredentialsRequired = parsed.User.Password()
		}
		if queue != nil && protocolCredentialsRequired {
			return nil, &jrpc2.Error{
				Code: codeInvalidParams,
				Message: scheme +
					" credentials cannot be used with queued downloads because protocol secrets are not persisted",
			}
		}
		pd, err := rs.schemeRouter.NewDownloader(p.URL, opts)
		if err != nil {
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: err.Error()}
		}
		probe, err := pd.Probe(transferCtx)
		if err != nil {
			return nil, &jrpc2.Error{
				Code:    codeInvalidParams,
				Message: errors.Join(err, pd.Close()).Error(),
			}
		}
		cleanURL := warplib.StripURLCredentials(p.URL)
		var proto warplib.Protocol
		switch scheme {
		case "ftps":
			proto = warplib.ProtoFTPS
		case "sftp":
			proto = warplib.ProtoSFTP
		default:
			proto = warplib.ProtoFTP
		}
		hash = pd.GetHash()
		if rs.pool != nil {
			generation, reserved := rs.pool.beginLegacyDownload(hash, nil)
			if !reserved {
				return nil, &jrpc2.Error{
					Code: codeDownloadNotActive,
					Message: errors.Join(
						errors.New("download is already running or still stopping"),
						pd.Close(),
					).Error(),
				}
			}
			poolGeneration.Store(generation)
		}
		if err := rs.manager.AddProtocolDownload(pd, probe, cleanURL, proto, opts.Handlers, &warplib.AddDownloadOpts{
			AbsoluteLocation: pd.GetDownloadDirectory(),
			SSHKeyPath:       p.SSHKeyPath,
			SkipQueue:        queue != nil,
			TransferConfig: warplib.TransferConfig{
				ProtocolUsername:            protocolUsername,
				ProtocolCredentialsRequired: protocolCredentialsRequired,
			},
		}); err != nil {
			if generation := poolGeneration.Load(); generation != nil {
				generation.Abort()
			}
			return nil, &jrpc2.Error{
				Code:    codeInvalidParams,
				Message: errors.Join(err, pd.Close()).Error(),
			}
		}
		var runLease *warplib.RunLease
		if queue == nil {
			runLease, err = rs.manager.AcquireProtocolRunLease(hash, pd)
			if err != nil {
				if generation := poolGeneration.Load(); generation != nil {
					generation.Abort()
				}
				return nil, &jrpc2.Error{
					Code:    codeDownloadNotActive,
					Message: err.Error(),
				}
			}
		}
		rs.registerTransferTerminal(hash, &errorReported)
		if queue != nil {
			queue.Add(hash, warplib.PriorityNormal)
			if _, closeErr := rs.manager.CloseWaitingDownloader(hash); closeErr != nil {
				cleanupErr := rs.cleanupExactDownloadRegistration(
					hash,
					pd,
					&errorReported,
					poolGeneration.Load(),
				)
				return nil, &jrpc2.Error{
					Code:    codeDownloadNotActive,
					Message: errors.Join(closeErr, cleanupErr).Error(),
				}
			}
		}
		if queue == nil {
			if !rs.launchTransfer(func(ctx context.Context) {
				runErr := normalizeServerTransferError(
					ctx,
					runLease.StartContext(ctx),
				)
				rs.finishAsyncTransfer(
					hash,
					poolGeneration.Load(),
					runLease,
					runErr,
					&errorReported,
				)
			}) {
				closeErr := runLease.Close()
				cleanupErr := rs.cleanupExactDownloadRegistration(
					hash,
					pd,
					&errorReported,
					poolGeneration.Load(),
				)
				return nil, &jrpc2.Error{
					Code: codeDownloadNotActive,
					Message: errors.Join(
						warplib.ErrManagerShuttingDown,
						closeErr,
						cleanupErr,
					).Error(),
				}
			}
		}
		if rs.notifier != nil {
			rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
				GID:         hash,
				FileName:    probe.FileName,
				TotalLength: probe.ContentLength,
			})
		}
		return &AddResult{GID: hash}, nil
	}
}

// downloadPause stops an active download.
func (rs *RPCServer) downloadPause(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, &jrpc2.Error{Code: codeDownloadNotFound, Message: "download not found"}
	}
	if queue := rs.manager.GetQueue(); queue != nil {
		waiting, closeErr := rs.manager.RemoveWaitingDownloader(p.GID)
		if waiting {
			var generation *TransferGeneration
			if rs.pool != nil {
				generation, _ = rs.pool.CurrentGeneration(p.GID)
			}
			rs.clearTransferTerminal(p.GID)
			if generation != nil {
				frame := MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: p.GID,
					Action:     common.DownloadStopped,
				})
				generation.RecordTerminal(frame)
				generation.Finish(frame)
			}
			if closeErr != nil {
				return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: closeErr.Error()}
			}
			return &EmptyResult{}, nil
		}
	}

	if rs.pool != nil && !rs.pool.HasDownload(p.GID) {
		return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: "download not running"}
	}
	if err := item.StopDownload(); err != nil {
		return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: err.Error()}
	}
	// Stop only requests cancellation. The transfer goroutine still owns its
	// network/file resources, so its manager-patched stopped callback (or
	// finishAsyncTransfer fallback) releases the queue slot and publishes the
	// terminal event after the old transfer has actually drained.
	return &EmptyResult{}, nil
}

// downloadResume resumes a paused download.
// Mirrors downloadAdd's notification pattern: wires handlers for progress/error/complete
// notifications and broadcasts download.started after successful resume.
func (rs *RPCServer) downloadResume(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, &jrpc2.Error{Code: codeDownloadNotFound, Message: "download not found"}
	}
	if rs.pool != nil && rs.pool.HasDownload(p.GID) {
		return nil, &jrpc2.Error{
			Code:    codeDownloadNotActive,
			Message: "download is already running or still stopping",
		}
	}

	hash := p.GID
	var terminalReported atomic.Bool
	var poolGeneration atomic.Pointer[TransferGeneration]
	queue := rs.manager.GetQueue()
	var queuedSnapshot warplib.ItemSnapshot
	if queue != nil {
		queuedSnapshot = item.Snapshot()
		if !queuedSnapshot.Resumable {
			return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: warplib.ErrDownloadNotResumable.Error()}
		}
	}
	if rs.pool != nil {
		generation, reserved := rs.pool.beginLegacyDownload(hash, nil)
		if !reserved {
			return nil, &jrpc2.Error{
				Code:    codeDownloadNotActive,
				Message: "download is already running or still stopping",
			}
		}
		poolGeneration.Store(generation)
	}
	if queue != nil {
		// The daemon queue callback owns reconstruction and start. Rebuilding
		// here as well would leak/overwrite a downloader when Add immediately
		// activates an item with persisted parts.
		rs.registerTransferTerminal(hash, &terminalReported)
		queue.Add(hash, warplib.PriorityNormal)
		if _, closeErr := rs.manager.CloseWaitingDownloader(hash); closeErr != nil {
			rs.unregisterTransferTerminal(hash, &terminalReported)
			queue.Remove(hash)
			if rs.pool != nil {
				if generation := poolGeneration.Load(); generation != nil {
					generation.Abort()
				}
			}
			return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: closeErr.Error()}
		}
		if rs.notifier != nil {
			rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
				GID:         queuedSnapshot.Hash,
				FileName:    queuedSnapshot.Name,
				TotalLength: int64(queuedSnapshot.TotalSize),
			})
		}
		return &EmptyResult{}, nil
	}

	resumeOpts := &warplib.ResumeDownloadOpts{
		Handlers: rs.rpcTransferHandlers(&hash, &terminalReported, &poolGeneration),
	}

	resumedItem, lease, err := rs.manager.ResumeDownloadWithLease(rs.client, p.GID, resumeOpts)
	if err != nil {
		var closeErr error
		if lease != nil {
			_, closeErr = lease.Close()
		}
		if generation := poolGeneration.Load(); generation != nil {
			generation.Abort()
		}
		return nil, &jrpc2.Error{
			Code:    codeDownloadNotActive,
			Message: errors.Join(err, closeErr).Error(),
		}
	}
	rs.registerTransferTerminal(hash, &terminalReported)

	if !rs.launchTransfer(func(ctx context.Context) {
		runErr := normalizeServerTransferError(ctx, lease.ResumeContext(ctx))
		rs.finishReconstructedTransfer(
			p.GID,
			poolGeneration.Load(),
			lease,
			runErr,
			&terminalReported,
		)
	}) {
		rs.unregisterTransferTerminal(hash, &terminalReported)
		_, closeErr := lease.Close()
		rs.manager.ReleaseQueueSlot(hash)
		if rs.pool != nil {
			if generation := poolGeneration.Load(); generation != nil {
				generation.Abort()
			}
		}
		return nil, &jrpc2.Error{
			Code: codeDownloadNotActive,
			Message: errors.Join(
				warplib.ErrManagerShuttingDown,
				closeErr,
			).Error(),
		}
	}

	// Broadcast download.started AFTER successful resume (not before, to avoid phantom events)
	if rs.notifier != nil {
		snapshot := resumedItem.Snapshot()
		rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
			GID:         snapshot.Hash,
			FileName:    snapshot.Name,
			TotalLength: int64(snapshot.TotalSize),
		})
	}

	return &EmptyResult{}, nil
}

// downloadRemove removes a download from the manager.
func (rs *RPCServer) downloadRemove(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	if err := rs.manager.FlushOne(p.GID); err != nil {
		if err == warplib.ErrFlushHashNotFound {
			return nil, &jrpc2.Error{Code: codeDownloadNotFound, Message: "download not found"}
		}
		return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: err.Error()}
	}
	return &EmptyResult{}, nil
}

// downloadStatus returns the status of a download.
func (rs *RPCServer) downloadStatus(_ context.Context, p *GIDParam) (*StatusResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, &jrpc2.Error{Code: codeDownloadNotFound, Message: "download not found"}
	}
	snapshot := item.Snapshot()
	return &StatusResult{
		GID:             snapshot.Hash,
		Status:          itemStatus(item),
		TotalLength:     int64(snapshot.TotalSize),
		CompletedLength: int64(snapshot.Downloaded),
		Percentage:      item.GetPercentage(),
		FileName:        snapshot.Name,
	}, nil
}

// downloadList returns a list of downloads, optionally filtered by status.
func (rs *RPCServer) downloadList(_ context.Context, p *ListParams) (*ListResult, error) {
	var items []*warplib.Item

	status := p.Status
	if status == "" {
		status = "all"
	}

	switch status {
	case "all":
		items = rs.manager.GetItems()
	case "active":
		for _, item := range rs.manager.GetItems() {
			if item.IsDownloading() {
				items = append(items, item)
			}
		}
	case "complete":
		items = rs.manager.GetCompletedItems()
	case "waiting":
		for _, item := range rs.manager.GetIncompleteItems() {
			if !item.IsDownloading() {
				items = append(items, item)
			}
		}
	default:
		items = rs.manager.GetItems()
	}

	downloads := make([]*ListItem, 0, len(items))
	for _, item := range items {
		snapshot := item.Snapshot()
		downloads = append(downloads, &ListItem{
			GID:             snapshot.Hash,
			Status:          itemStatus(item),
			TotalLength:     int64(snapshot.TotalSize),
			CompletedLength: int64(snapshot.Downloaded),
			FileName:        snapshot.Name,
		})
	}

	return &ListResult{Downloads: downloads}, nil
}

// itemStatus returns the status string for a download item.
// Checks completion first because IsDownloading() returns true even
// after a download finishes (dAlloc is only cleared by StopDownload).
// Uses thread-safe getters for Downloaded/TotalSize to avoid data races.
func itemStatus(item *warplib.Item) string {
	downloaded := item.GetDownloaded()
	totalSize := item.GetTotalSize()
	if downloaded >= totalSize && totalSize > 0 {
		return "complete"
	}
	if item.IsDownloading() {
		return "active"
	}
	return "waiting"
}

// Close shuts down the jrpc2 bridge, releasing internal goroutines.
func (rs *RPCServer) Close() {
	rs.bridge.Close()
}
