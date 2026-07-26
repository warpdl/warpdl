package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// ErrDrainIncomplete means server shutdown could not prove that every
// server-owned goroutine exited. Callers must keep resources used by those
// goroutines alive.
var ErrDrainIncomplete = errors.New("server drain incomplete")

// Server manages RPC connections from CLI clients over a Unix socket or named pipe.
// It dispatches incoming requests to registered handlers and manages
// the connection pool for active downloads.
// Transport priority (platform-specific):
// - Unix: Unix socket > TCP
// - Windows: Named pipe > TCP
type Server struct {
	log      *log.Logger
	pool     *Pool
	ws       *WebServer
	handler  map[common.UpdateType]HandlerFunc
	port     int
	listener net.Listener
	mu       sync.Mutex

	shutdownMu   sync.Mutex
	connections  map[net.Conn]struct{}
	connectionWG sync.WaitGroup
	acceptWG     sync.WaitGroup
	running      bool
	shuttingDown bool
	ipcOwned     bool

	// listenIPC is a per-server test seam. Production always uses the
	// platform-specific createListener implementation.
	listenIPC func() (net.Listener, error)
}

// NewServer creates a new Server instance with the given logger, download manager,
// and port number. The server uses platform-specific IPC as the primary transport,
// falling back to TCP on the specified port if the primary transport fails.
func NewServer(l *log.Logger, m *warplib.Manager, port int, client *http.Client, router *warplib.SchemeRouter, rpcCfg *RPCConfig) *Server {
	pool := NewPool(l)
	webPort := port + 1
	if port == 0 {
		// Tests and embedders use zero to request an ephemeral port. Port 1 is
		// privileged on Unix and was previously hidden by background startup.
		webPort = 0
	}
	return &Server{
		log:         l,
		pool:        pool,
		handler:     make(map[common.UpdateType]HandlerFunc),
		port:        port,
		ws:          NewWebServer(l, m, pool, webPort, client, router, rpcCfg),
		connections: make(map[net.Conn]struct{}),
	}
}

// RegisterHandler associates a handler function with a specific update type method.
// When a request with the given method is received, the corresponding handler is invoked.
func (s *Server) RegisterHandler(method common.UpdateType, handler HandlerFunc) {
	s.handler[method] = handler
}

// Pool returns the server's download pool. Exposed so daemon code can
// broadcast error events on async paths (e.g. queue auto-start failures)
// that would otherwise only log to the daemon console and leave the CLI
// polling forever.
func (s *Server) Pool() *Pool {
	return s.pool
}

// DecorateTransferHandlers layers daemon-wide JSON-RPC notifications onto a
// fresh set of transfer handlers. Queue and restart reconstruction happen in
// cmd after the original RPC request has returned, so those paths cannot keep
// using RPCServer's request-local handler closure directly.
//
// Existing handlers are always chained. reportReturnedError must be called
// with Start/Resume's return value; it fills the callback gap for failures
// that occur before a downloader invokes ErrorHandler. One atomic terminal
// gate prevents a worker callback and the returned error from publishing the
// same JSON-RPC terminal event twice.
func (s *Server) DecorateTransferHandlers(uid string, handlers *warplib.Handlers) (*warplib.Handlers, func(error)) {
	return s.DecorateTransferHandlersForGeneration(uid, nil, handlers)
}

// DecorateTransferHandlersForGeneration binds daemon-wide notifications to
// one exact pool generation. Late callbacks from a replaced transfer still
// reach the allocation's original handlers, but cannot notify as its successor.
func (s *Server) DecorateTransferHandlersForGeneration(
	uid string,
	generation *TransferGeneration,
	handlers *warplib.Handlers,
) (*warplib.Handlers, func(error)) {
	if handlers == nil {
		handlers = &warplib.Handlers{}
	}
	if s == nil || s.ws == nil || s.ws.rpc == nil || s.ws.rpc.notifier == nil {
		return handlers, func(error) {}
	}

	rpc := s.ws.rpc
	terminalReported := rpc.transferTerminal(uid)

	oldError := handlers.ErrorHandler
	handlers.ErrorHandler = func(hash string, err error) {
		reportErr := err
		if s.ws.m != nil {
			reportErr = normalizeServerTransferError(s.ws.m.TransferContext(), err)
		}
		runnable := generation == nil || generation.IsRunnable()
		if reportErr != nil && runnable && terminalReported.CompareAndSwap(false, true) {
			rpc.broadcastError(uid, reportErr.Error())
		}
		if oldError != nil {
			oldError(hash, err)
		}
	}

	oldProgress := handlers.DownloadProgressHandler
	handlers.DownloadProgressHandler = func(hash string, nread int) {
		runnable := generation == nil || generation.IsRunnable()
		if runnable && !terminalReported.Load() {
			rpc.notifier.Broadcast("download.progress", &DownloadProgressNotification{
				GID:             uid,
				CompletedLength: int64(nread),
			})
		}
		if oldProgress != nil {
			oldProgress(hash, nread)
		}
	}

	oldResumeProgress := handlers.ResumeProgressHandler
	handlers.ResumeProgressHandler = func(hash string, nread int) {
		runnable := generation == nil || generation.IsRunnable()
		if runnable && !terminalReported.Load() {
			rpc.notifier.Broadcast("download.progress", &DownloadProgressNotification{
				GID:             uid,
				CompletedLength: int64(nread),
			})
		}
		if oldResumeProgress != nil {
			oldResumeProgress(hash, nread)
		}
	}

	oldComplete := handlers.DownloadCompleteHandler
	handlers.DownloadCompleteHandler = func(hash string, tread int64) {
		runnable := generation == nil || generation.IsRunnable()
		if runnable && terminalReported.CompareAndSwap(false, true) {
			rpc.notifier.Broadcast("download.complete", &DownloadCompleteNotification{
				GID:         uid,
				TotalLength: tread,
			})
		}
		if oldComplete != nil {
			oldComplete(hash, tread)
		}
	}

	reportReturnedError := func(err error) {
		reportErr := err
		if s.ws.m != nil {
			reportErr = normalizeServerTransferError(s.ws.m.TransferContext(), err)
		}
		runnable := generation == nil || generation.IsRunnable()
		if reportErr != nil && runnable && terminalReported.CompareAndSwap(false, true) {
			rpc.broadcastError(uid, reportErr.Error())
		}
		if generation == nil || generation.IsCurrent() {
			rpc.unregisterTransferTerminal(uid, terminalReported)
		}
	}
	return handlers, reportReturnedError
}

// TransferResultReporter returns the generation-bound completion reporter for
// a live RPC allocation. Immediate queue activation can reuse the downloader
// created by download.add instead of reconstructing it; this closure lets the
// daemon report an early returned error without double-reporting an error that
// the allocation's original handler already published.
func (s *Server) TransferResultReporter(uid string) func(error) {
	return s.TransferResultReporterForGeneration(uid, nil)
}

// TransferResultReporterForGeneration binds the returned-error gap reporter
// to one exact pool generation so a late result cannot notify or clean up a
// replacement transfer that reused the same UID.
func (s *Server) TransferResultReporterForGeneration(
	uid string,
	generation *TransferGeneration,
) func(error) {
	if s == nil || s.ws == nil || s.ws.rpc == nil {
		return func(error) {}
	}
	return s.ws.rpc.returnedErrorReporterForGeneration(uid, generation)
}

// Start begins listening for incoming connections and blocks until the context
// is canceled or either listener fails. Both listeners are established before
// the accept loops are allowed to run, so bind failures are reported to the
// caller instead of being lost in a background goroutine.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("server is already running")
	}
	s.running = true
	s.shuttingDown = false
	s.ipcOwned = false
	if s.connections == nil {
		s.connections = make(map[net.Conn]struct{})
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	webServer, webListener, webDone, err := s.ws.prepare()
	if err != nil {
		return fmt.Errorf("start web server: %w", err)
	}

	l, err := s.createIPCListener()
	if err != nil {
		shutdownErr := s.Shutdown()
		return errors.Join(
			fmt.Errorf("start IPC listener: %w", err),
			shutdownErr,
		)
	}

	s.mu.Lock()
	s.ipcOwned = true
	if s.shuttingDown {
		s.mu.Unlock()
		_ = l.Close()
		shutdownErr := s.Shutdown()
		return shutdownErr
	}
	s.listener = l
	s.mu.Unlock()

	webErrCh, err := s.ws.startPrepared(webServer, webListener, webDone)
	if err != nil {
		shuttingDown := s.isShuttingDown()
		shutdownErr := s.Shutdown()
		if shuttingDown {
			return shutdownErr
		}
		return errors.Join(fmt.Errorf("start web server: %w", err), shutdownErr)
	}

	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		shutdownErr := s.Shutdown()
		webErr := waitForServerResult(webErrCh)
		return errors.Join(shutdownErr, wrapServerRuntimeError("web server", webErr))
	}
	s.acceptWG.Add(1)
	s.mu.Unlock()

	acceptErrCh := make(chan error, 1)
	go func() {
		defer s.acceptWG.Done()
		acceptErrCh <- s.acceptLoop(l)
	}()

	var (
		cause               error
		webErr              error
		acceptErr           error
		webResultRead       bool
		acceptResultRead    bool
		alreadyShuttingDown bool
	)
	select {
	case <-ctx.Done():
	case webErr = <-webErrCh:
		webResultRead = true
		alreadyShuttingDown = s.isShuttingDown()
		if !alreadyShuttingDown {
			if webErr == nil {
				cause = errors.New("web server stopped unexpectedly")
			} else {
				cause = fmt.Errorf("web server failed: %w", webErr)
			}
		}
	case acceptErr = <-acceptErrCh:
		acceptResultRead = true
		alreadyShuttingDown = s.isShuttingDown()
		if !alreadyShuttingDown {
			if acceptErr == nil {
				cause = errors.New("IPC listener stopped unexpectedly")
			} else {
				cause = fmt.Errorf("IPC listener failed: %w", acceptErr)
			}
		}
	}

	shutdownErr := s.Shutdown()
	if !webResultRead {
		webErr = waitForServerResult(webErrCh)
		if webErr != nil {
			cause = errors.Join(cause, fmt.Errorf("web server failed: %w", webErr))
		}
	}
	if !acceptResultRead {
		acceptErr = waitForServerResult(acceptErrCh)
		if acceptErr != nil && !errors.Is(acceptErr, net.ErrClosed) {
			cause = errors.Join(cause, fmt.Errorf("IPC listener failed: %w", acceptErr))
		}
	}
	return errors.Join(cause, shutdownErr)
}

// Shutdown gracefully stops the server by closing the listener and cleaning up resources.
// It uses common.ShutdownTimeout for the web server shutdown timeout.
func (s *Server) Shutdown() error {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), common.ShutdownTimeout)
	defer cancel()

	s.mu.Lock()
	s.shuttingDown = true
	listener := s.listener
	s.listener = nil
	connections := make([]net.Conn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	cleanupIPC := s.ipcOwned
	s.ipcOwned = false
	s.mu.Unlock()

	var errs []error
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, fmt.Errorf("close IPC listener: %w", err))
		}
	}
	for _, conn := range connections {
		// Closing is the cancellation mechanism for a client blocked in Read.
		// Errors here generally mean the peer closed first and are harmless.
		_ = conn.Close()
	}

	if s.ws != nil {
		if err := s.ws.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown web server: %w", err))
		}
	}
	if err := waitForServerGroup(shutdownCtx, &s.acceptWG); err != nil {
		errs = append(errs, fmt.Errorf("wait for IPC accept loop: %w", err))
	}
	if err := waitForServerGroup(shutdownCtx, &s.connectionWG); err != nil {
		errs = append(errs, fmt.Errorf("wait for IPC connections: %w", err))
	}

	// Only remove a socket/pipe endpoint that this lifecycle successfully
	// created. A bind failure may be caused by an unrelated file at the
	// configured path, which must never be deleted during rollback.
	if cleanupIPC {
		if err := cleanupSocket(); err != nil {
			errs = append(errs, fmt.Errorf("clean up IPC listener: %w", err))
		}
	}

	return errors.Join(errs...)
}

func (s *Server) createIPCListener() (net.Listener, error) {
	if s.listenIPC != nil {
		return s.listenIPC()
	}
	return s.createListener()
}

func (s *Server) acceptLoop(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		if !s.trackConnection(conn) {
			_ = conn.Close()
			continue
		}
		go func() {
			defer s.untrackConnection(conn)
			s.handleConnection(conn)
		}()
	}
}

func (s *Server) trackConnection(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shuttingDown {
		return false
	}
	s.connections[conn] = struct{}{}
	s.connectionWG.Add(1)
	return true
}

func (s *Server) untrackConnection(conn net.Conn) {
	s.mu.Lock()
	delete(s.connections, conn)
	s.mu.Unlock()
	s.connectionWG.Done()
}

func (s *Server) isShuttingDown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shuttingDown
}

func waitForServerGroup(ctx context.Context, group *sync.WaitGroup) error {
	return waitForServerGroupWithTimeout(ctx, group, common.ShutdownTimeout)
}

func waitForServerGroupWithTimeout(ctx context.Context, group *sync.WaitGroup, fallbackTimeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}

	// All owned connections have already been forcibly closed. Give their
	// goroutines one final bounded interval to observe that closure before
	// reporting a teardown failure.
	fallbackCtx, cancel := context.WithTimeout(context.Background(), fallbackTimeout)
	defer cancel()
	select {
	case <-done:
		return ctx.Err()
	case <-fallbackCtx.Done():
		// Resolve a completion racing the fallback timer in favor of the
		// proven drain.
		select {
		case <-done:
			return ctx.Err()
		default:
		}
		return errors.Join(ErrDrainIncomplete, ctx.Err(), fallbackCtx.Err())
	}
}

func waitForServerResult(result <-chan error) error {
	return waitForServerResultWithTimeout(result, common.ShutdownTimeout)
}

func waitForServerResultWithTimeout(result <-chan error, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		// Resolve a loop completion racing the timer in favor of the proven
		// drain.
		select {
		case err := <-result:
			return err
		default:
		}
		return fmt.Errorf("%w: timed out waiting for server loop", ErrDrainIncomplete)
	}
}

func wrapServerRuntimeError(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s failed: %w", name, err)
}

// handleConnection manages a single client connection.
// It reads requests in a loop until an error occurs or the client disconnects.
func (s *Server) handleConnection(conn net.Conn) {
	sconn := NewSyncConn(conn)
	defer conn.Close()
	for {
		buf, err := sconn.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			s.log.Println("Error reading from connection:", err.Error())
			break
		}
		err = s.handlerWrapper(sconn, buf)
		if err != nil {
			s.log.Println("Error handling request:", err.Error())
			break
		}
	}
}

// handlerWrapper processes a single request by parsing it, invoking the appropriate
// handler, and writing the response back to the client.
func (s *Server) handlerWrapper(sconn *SyncConn, b []byte) error {
	req, err := ParseRequest(b)
	if err != nil {
		return fmt.Errorf("error parsing request: %s", err.Error())
	}
	rHandler, ok := s.handler[req.Method]
	if !ok {
		err = sconn.Write(CreateError("unknown method: " + string(req.Method)))
		if err != nil {
			return fmt.Errorf("error writing response: %s", err.Error())
		}
		return nil
	}
	utype, msg, err := rHandler(sconn, s.pool, req.Message)
	if err != nil {
		err = sconn.Write(InitError(err))
		if err != nil {
			return fmt.Errorf("error writing response: %s", err.Error())
		}
		return nil
	}
	err = sconn.Write(MakeResult(utype, msg))
	if err != nil {
		return fmt.Errorf("error writing response: %s", err.Error())
	}
	return nil
}
