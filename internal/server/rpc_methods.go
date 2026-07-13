package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	jsonrpc "github.com/gumeniukcom/golang-jsonrpc2/v2"
	"github.com/gumeniukcom/golang-jsonrpc2/v2/jsonrpchttp"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// Custom JSON-RPC error codes for download operations, unchanged from the
// pre-migration jrpc2 wire protocol. -32000..-32099 is the JSON-RPC 2.0
// implementation-defined server-error range; registering codes in the
// spec-reserved range logs a server-side warning only (golang-jsonrpc2
// v2.7.0+).
const (
	codeDownloadNotFound  = -32001
	codeDownloadNotActive = -32002
	codeInvalidParams     = jsonrpc.InvalidParamsErrorCode
)

// Shared sentinel errors for download lookups. The registered message for
// the code is what the client sees; the wrapped error is logged server-side.
var (
	errDownloadNotFound  = jsonrpc.NewRPCError(codeDownloadNotFound, errors.New("download not found"))
	errDownloadNotActive = jsonrpc.NewRPCError(codeDownloadNotActive, errors.New("download not running"))
)

// rpcErrPublic builds an application RPC error whose detail is exposed to
// the client via error.data (the error message itself stays the registered
// per-code text). The pre-migration jrpc2 protocol carried this detail in
// error.message, so every handler error that used to expose text keeps it
// client-visible — uniformly in error.data.
func rpcErrPublic(code int, detail string) *jsonrpc.RPCError {
	return jsonrpc.NewRPCError(code, errors.New(detail)).WithData(detail)
}

// rpcErrWrap is rpcErrPublic for wrapped errors: err stays in the chain for
// server-side logging and errors.Is/As, and err.Error() is mirrored into
// client-visible error.data — the same string the old protocol put in
// error.message.
func rpcErrWrap(code int, err error) *jsonrpc.RPCError {
	return jsonrpc.NewRPCError(code, err).WithData(err.Error())
}

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
	rpc          *jsonrpc.JSONRPC
	bridge       http.Handler
	version      string
	commit       string
	buildType    string
	manager      *warplib.Manager
	client       *http.Client
	pool         *Pool
	schemeRouter *warplib.SchemeRouter
	notifier     *RPCNotifier
	log          *log.Logger

	// Test seams for the adaptive YouTube download path. Nil in production;
	// see (*RPCServer).legDownloaderFn and (*RPCServer).muxerFn for the
	// defaults. Instance fields rather than package globals so stubbing in
	// tests requires no global write-back (which would race with reads from
	// the adaptive download goroutine).
	legDownloader func(rs *RPCServer, streamURL, outPath string, connections int32, progressFn func(int64)) error
	muxer         func(ctx context.Context, videoIn, audioIn, out string) error
}

// logf writes to the daemon log if one is configured. Safe on a zero-value
// RPCServer (tests construct &RPCServer{} directly).
func (rs *RPCServer) logf(format string, args ...any) {
	if rs.log != nil {
		rs.log.Printf(format, args...)
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

	rpc := jsonrpc.New()
	if l != nil {
		rpc.SetLogger(slog.New(slog.NewTextHandler(l.Writer(), nil)))
	} else {
		rpc.SetLogger(nil)
	}
	registerRPCErrors(rpc)
	rs.registerMethods(rpc)
	rs.rpc = rpc
	rs.bridge = jsonrpchttp.Handler(rpc)
	return rs
}

// registerRPCErrors registers the application error codes (and their
// client-visible messages) on the dispatcher. Codes must be registered
// before a handler returns them — an unregistered code degrades to
// internal_error on the wire.
func registerRPCErrors(rpc *jsonrpc.JSONRPC) {
	for code, msg := range map[int]string{
		codeDownloadNotFound:    "download not found",
		codeDownloadNotActive:   "download not running",
		codeResolverFailed:      "resolver_failed",
		codeResolverTimeout:     "resolver_timeout",
		codeResolverUnsupported: "resolver_unsupported",
		codeMuxerUnavailable:    "muxer_unavailable",
		codeFormatNotFound:      "format_not_found",
		codeFormatMismatch:      "format_mismatch",
	} {
		if err := rpc.RegisterError(code, msg); err != nil {
			panic(fmt.Sprintf("rpc: register error code %d: %v", code, err))
		}
	}
}

// registerMethods registers the JSON-RPC methods shared by the HTTP bridge
// and the WebSocket endpoint. Typed wrappers adapt the pointer-taking
// handlers: absent params decode to the zero value, matching the previous
// jrpc2 wire behavior for object params.
//
// Methods that only touch local state keep the dispatcher's 30s default
// timeout. Methods that talk to remote services get explicit per-method
// timeouts (jrpc2 had no per-request timeout at all). The side-effecting
// methods — download.add, download.resume, youtube.download — start
// downloads and broadcast download.started BEFORE returning the GID: a
// dispatcher timeout firing after that point would replace the success
// response with -32605 and orphan work the client can no longer correlate,
// so they get a practically-unreachable bound instead of a tight one.
// resolve.url has no side effects and its inner client-tunable clamp
// (maxResolverTimeout) fires first, so a modest dispatcher bound is safe.
func (rs *RPCServer) registerMethods(rpc *jsonrpc.JSONRPC) {
	must := func(err error) {
		if err != nil {
			panic(fmt.Sprintf("rpc: register method: %v", err))
		}
	}
	sideEffectTimeout := jsonrpc.WithTimeout(15 * time.Minute)
	resolverTimeout := jsonrpc.WithTimeout(maxResolverTimeout + 30*time.Second)

	must(jsonrpc.RegisterTyped(rpc, "system.getVersion", func(ctx context.Context, _ struct{}) (*VersionResult, error) {
		return rs.systemGetVersion(ctx)
	}))
	must(jsonrpc.RegisterTyped(rpc, "download.add", func(ctx context.Context, p AddParams) (*AddResult, error) {
		return rs.downloadAdd(ctx, &p)
	}, sideEffectTimeout))
	must(jsonrpc.RegisterTyped(rpc, "download.pause", func(ctx context.Context, p GIDParam) (*EmptyResult, error) {
		return rs.downloadPause(ctx, &p)
	}))
	must(jsonrpc.RegisterTyped(rpc, "download.resume", func(ctx context.Context, p GIDParam) (*EmptyResult, error) {
		return rs.downloadResume(ctx, &p)
	}, sideEffectTimeout))
	must(jsonrpc.RegisterTyped(rpc, "download.remove", func(ctx context.Context, p GIDParam) (*EmptyResult, error) {
		return rs.downloadRemove(ctx, &p)
	}))
	must(jsonrpc.RegisterTyped(rpc, "download.status", func(ctx context.Context, p GIDParam) (*StatusResult, error) {
		return rs.downloadStatus(ctx, &p)
	}))
	must(jsonrpc.RegisterTyped(rpc, "download.list", func(ctx context.Context, p ListParams) (*ListResult, error) {
		return rs.downloadList(ctx, &p)
	}))
	// resolve.url uses github.com/kkdai/youtube/v2 to resolve YouTube
	// page URLs into a list of downloadable formats. URLs are decoded
	// lazily by youtube.download. See internal/server/rpc_resolve.go.
	must(jsonrpc.RegisterTyped(rpc, "resolve.url", func(ctx context.Context, p common.ResolveURLParams) (*common.ResolveURLResult, error) {
		return rs.resolveURL(ctx, &p)
	}, resolverTimeout))
	// youtube.download downloads a chosen format. Progressive itags
	// (audio+video bundled) flow through the existing download manager.
	// Adaptive (video-only + audio-only) downloads run a parallel-leg
	// download then ffmpeg remux. See internal/server/rpc_youtube_download.go.
	must(jsonrpc.RegisterTyped(rpc, "youtube.download", func(ctx context.Context, p common.YouTubeDownloadParams) (*common.YouTubeDownloadResult, error) {
		return rs.youtubeDownload(ctx, &p)
	}, sideEffectTimeout))
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
		return nil, rpcErrPublic(codeInvalidParams, "missing required param: url")
	}

	parsed, err := url.Parse(p.URL)
	if err != nil {
		return nil, rpcErrPublic(codeInvalidParams, "invalid url: "+err.Error())
	}

	scheme := strings.ToLower(parsed.Scheme)
	connections := p.Connections
	if connections <= 0 {
		connections = 24
	}

	opts := &warplib.DownloaderOpts{
		FileName:          p.FileName,
		DownloadDirectory: p.Dir,
		Headers:           p.Headers,
		MaxConnections:    connections,
		SSHKeyPath:        p.SSHKeyPath,
	}

	// Wire notifier into download event handlers if available.
	if rs.notifier != nil {
		opts.Handlers = &warplib.Handlers{
			ErrorHandler: func(hash string, err error) {
				rs.notifier.Broadcast("download.error", &DownloadErrorNotification{
					GID:   hash,
					Error: err.Error(),
				})
			},
			DownloadProgressHandler: func(hash string, nread int) {
				rs.notifier.Broadcast("download.progress", &DownloadProgressNotification{
					GID:             hash,
					CompletedLength: int64(nread),
				})
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				rs.notifier.Broadcast("download.complete", &DownloadCompleteNotification{
					GID:         hash,
					TotalLength: tread,
				})
			},
		}
	}

	switch scheme {
	case "http", "https":
		d, err := warplib.NewDownloader(rs.client, p.URL, opts)
		if err != nil {
			return nil, rpcErrWrap(codeInvalidParams, err)
		}
		if err := rs.manager.AddDownload(d, &warplib.AddDownloadOpts{
			AbsoluteLocation: d.GetDownloadDirectory(),
		}); err != nil {
			return nil, rpcErrWrap(codeInvalidParams, err)
		}
		hash := d.GetHash()
		if rs.pool != nil {
			rs.pool.AddDownload(hash, nil)
		}
		if rs.notifier != nil {
			rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
				GID:         hash,
				FileName:    d.GetFileName(),
				TotalLength: d.GetContentLengthAsInt(),
			})
		}
		go d.Start()
		return &AddResult{GID: hash}, nil

	default:
		// FTP, FTPS, SFTP -- use SchemeRouter
		if rs.schemeRouter == nil {
			return nil, rpcErrPublic(codeInvalidParams, "unsupported scheme: "+scheme)
		}
		pd, err := rs.schemeRouter.NewDownloader(p.URL, opts)
		if err != nil {
			return nil, rpcErrWrap(codeInvalidParams, err)
		}
		probe, err := pd.Probe(context.Background())
		if err != nil {
			pd.Close()
			return nil, rpcErrWrap(codeInvalidParams, err)
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
		if err := rs.manager.AddProtocolDownload(pd, probe, cleanURL, proto, opts.Handlers, &warplib.AddDownloadOpts{
			AbsoluteLocation: pd.GetDownloadDirectory(),
			SSHKeyPath:       p.SSHKeyPath,
		}); err != nil {
			return nil, rpcErrWrap(codeInvalidParams, err)
		}
		hash := pd.GetHash()
		if rs.pool != nil {
			rs.pool.AddDownload(hash, nil)
		}
		if rs.notifier != nil {
			rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
				GID:         hash,
				FileName:    probe.FileName,
				TotalLength: probe.ContentLength,
			})
		}
		go pd.Download(context.Background(), opts.Handlers)
		return &AddResult{GID: hash}, nil
	}
}

// downloadPause stops an active download.
func (rs *RPCServer) downloadPause(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, errDownloadNotFound
	}
	if rs.pool != nil && !rs.pool.HasDownload(p.GID) {
		return nil, errDownloadNotActive
	}
	item.StopDownload()
	return &EmptyResult{}, nil
}

// downloadResume resumes a paused download.
// Mirrors downloadAdd's notification pattern: wires handlers for progress/error/complete
// notifications and broadcasts download.started after successful resume.
func (rs *RPCServer) downloadResume(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, errDownloadNotFound
	}

	var resumeOpts *warplib.ResumeDownloadOpts
	if rs.notifier != nil {
		resumeOpts = &warplib.ResumeDownloadOpts{
			Handlers: &warplib.Handlers{
				ErrorHandler: func(hash string, err error) {
					rs.notifier.Broadcast("download.error", &DownloadErrorNotification{
						GID:   hash,
						Error: err.Error(),
					})
				},
				DownloadProgressHandler: func(hash string, nread int) {
					rs.notifier.Broadcast("download.progress", &DownloadProgressNotification{
						GID:             hash,
						CompletedLength: int64(nread),
					})
				},
				DownloadCompleteHandler: func(hash string, tread int64) {
					rs.notifier.Broadcast("download.complete", &DownloadCompleteNotification{
						GID:         hash,
						TotalLength: tread,
					})
				},
			},
		}
	}

	resumedItem, err := rs.manager.ResumeDownload(rs.client, p.GID, resumeOpts)
	if err != nil {
		return nil, rpcErrWrap(codeDownloadNotActive, err)
	}
	if rs.pool != nil {
		rs.pool.AddDownload(p.GID, nil)
	}

	// Broadcast download.started AFTER successful resume (not before, to avoid phantom events)
	if rs.notifier != nil {
		rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
			GID:         resumedItem.Hash,
			FileName:    resumedItem.Name,
			TotalLength: int64(resumedItem.GetTotalSize()),
		})
	}

	go resumedItem.Resume()
	return &EmptyResult{}, nil
}

// downloadRemove removes a download from the manager.
func (rs *RPCServer) downloadRemove(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	if err := rs.manager.FlushOne(p.GID); err != nil {
		if err == warplib.ErrFlushHashNotFound {
			return nil, errDownloadNotFound
		}
		return nil, rpcErrWrap(codeDownloadNotActive, err)
	}
	return &EmptyResult{}, nil
}

// downloadStatus returns the status of a download.
func (rs *RPCServer) downloadStatus(_ context.Context, p *GIDParam) (*StatusResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, errDownloadNotFound
	}
	return &StatusResult{
		GID:             item.Hash,
		Status:          itemStatus(item),
		TotalLength:     int64(item.GetTotalSize()),
		CompletedLength: int64(item.GetDownloaded()),
		Percentage:      item.GetPercentage(),
		FileName:        item.Name,
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
		downloads = append(downloads, &ListItem{
			GID:             item.Hash,
			Status:          itemStatus(item),
			TotalLength:     int64(item.GetTotalSize()),
			CompletedLength: int64(item.GetDownloaded()),
			FileName:        item.Name,
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

// Close releases RPC resources. The jsonrpchttp handler and the shared
// dispatcher are stateless (no background goroutines), so this is a no-op
// kept for lifecycle symmetry — WebServer.Shutdown and the tests call it,
// and it must stay safe to call more than once.
func (rs *RPCServer) Close() {}
