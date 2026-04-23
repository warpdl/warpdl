// Package api provides HTTP API handlers for the WarpDL daemon server.
// It coordinates request handling between the server and the download manager,
// exposing endpoints for download operations and extension management.
package api

import (
	"context"
	"log"
	"net/http"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/scheduler"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// Api coordinates request handling between the server and download manager.
// It encapsulates the download manager, extension engine, HTTP client,
// and scheme router required to process download and extension management requests.
type Api struct {
	log          *log.Logger
	manager      *warplib.Manager
	elEngine     *extl.Engine
	client       *http.Client
	schemeRouter *warplib.SchemeRouter
	scheduler    *scheduler.Scheduler
	version      string
	commit       string
	buildType    string

	// shutdownCtx is cancelled by Close; long-running background work
	// (e.g. device-flow polling goroutines) uses it so the daemon can
	// drain cleanly without leaking goroutines blocked on network I/O.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

// NewApi creates a new Api instance with the provided dependencies.
// It returns an initialized Api ready to handle download and extension requests.
// The logger is used for diagnostic output, the manager handles download state,
// the client performs HTTP requests, the elEngine manages JavaScript extensions,
// and the router dispatches FTP/FTPS URLs to the correct protocol downloader.
// The router may be nil if FTP support is not needed.
// Version info (version, commit, buildType) is stored for responding to version queries.
func NewApi(l *log.Logger, m *warplib.Manager, client *http.Client, elEngine *extl.Engine, router *warplib.SchemeRouter, sched *scheduler.Scheduler, version, commit, buildType string) (*Api, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &Api{
		log:            l,
		manager:        m,
		client:         client,
		elEngine:       elEngine,
		schemeRouter:   router,
		scheduler:      sched,
		version:        version,
		commit:         commit,
		buildType:      buildType,
		shutdownCtx:    ctx,
		shutdownCancel: cancel,
	}, nil
}

// daemonCtx returns the Api's lifetime-scoped context. Handlers spawn
// background goroutines against it so that Close() can cancel them in
// the shutdown path.
func (s *Api) daemonCtx() context.Context { return s.shutdownCtx }

// RegisterHandlers registers all API handlers with the provided server.
// It sets up handlers for download operations (download, resume, attach, flush,
// stop, list) and extension management operations (add, get, list, delete,
// activate, deactivate).
func (s *Api) RegisterHandlers(server *server.Server) {
	// downloader API methods
	server.RegisterHandler(common.UPDATE_DOWNLOAD, s.downloadHandler)
	server.RegisterHandler(common.UPDATE_RESUME, s.resumeHandler)
	server.RegisterHandler(common.UPDATE_ATTACH, s.attachHandler)
	server.RegisterHandler(common.UPDATE_FLUSH, s.flushHandler)
	server.RegisterHandler(common.UPDATE_STOP, s.stopHandler)
	server.RegisterHandler(common.UPDATE_LIST, s.listHandler)

	// extension API methods
	server.RegisterHandler(common.UPDATE_ADD_EXT, s.addExtHandler)
	server.RegisterHandler(common.UPDATE_GET_EXT, s.getExtHandler)
	server.RegisterHandler(common.UPDATE_LIST_EXT, s.listExtHandler)
	server.RegisterHandler(common.UPDATE_DELETE_EXT, s.deleteExtHandler)
	server.RegisterHandler(common.UPDATE_ACTIVATE_EXT, s.activateExtHandler)
	server.RegisterHandler(common.UPDATE_DEACTIVATE_EXT, s.deactivateExtHandler)

	// daemon info methods
	server.RegisterHandler(common.UPDATE_VERSION, s.versionHandler)

	// queue management methods
	server.RegisterHandler(common.UPDATE_QUEUE_STATUS, s.queueStatusHandler)
	server.RegisterHandler(common.UPDATE_QUEUE_PAUSE, s.queuePauseHandler)
	server.RegisterHandler(common.UPDATE_QUEUE_RESUME, s.queueResumeHandler)
	server.RegisterHandler(common.UPDATE_QUEUE_MOVE, s.queueMoveHandler)

	// authentication methods
	server.RegisterHandler(common.UPDATE_AUTH_REQUIRED, s.authLoginHandler)
	server.RegisterHandler(common.UPDATE_AUTH_COMPLETED, s.authCompleteHandler)
	server.RegisterHandler(common.UPDATE_AUTH_FAILED, s.authCancelHandler)
	server.RegisterHandler(common.UPDATE_AUTH_LOGGED_OUT, s.authLogoutHandler)
	server.RegisterHandler(common.UPDATE_AUTH_LIST, s.authListHandler)
}

// Close releases resources held by the Api, specifically closing the
// underlying download manager. It returns any error encountered during
// the close operation.
//
// Close also cancels the shutdown context, which signals any
// background goroutines (e.g. OAuth device-flow pollers) to unblock
// from network I/O and exit promptly.
func (s *Api) Close() error {
	if s.shutdownCancel != nil {
		s.shutdownCancel()
	}
	return s.manager.Close()
}
