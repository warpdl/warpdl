package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	cws "github.com/coder/websocket"
	"github.com/creachadair/jrpc2"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
	"golang.org/x/net/websocket"
)

type WebServer struct {
	port         int
	l            *log.Logger
	m            *warplib.Manager
	pool         *Pool
	server       *http.Server
	listener     net.Listener
	serveDone    chan struct{}
	serveStarted bool
	mu           sync.Mutex
	shutdownMu   sync.Mutex
	rpc          *RPCServer
	listenAll    bool

	activeHandlers map[*webHandlerState]struct{}
	handlerWG      sync.WaitGroup
	shuttingDown   bool
	rpcCloseOnce   sync.Once

	// listen is a per-server test seam. Production uses net.Listen.
	listen func(network, address string) (net.Listener, error)
}

type webHandlerState struct {
	close func() error
}

type webHandlerContextKey struct{}

type capturedDownload struct {
	Url     string          `json:"url"`
	Headers warplib.Headers `json:"headers"`
	Cookies []*http.Cookie  `json:"cookies"`
}

func NewWebServer(l *log.Logger, m *warplib.Manager, pool *Pool, port int, client *http.Client, router *warplib.SchemeRouter, rpcCfg *RPCConfig) *WebServer {
	ws := &WebServer{
		port:           port,
		l:              l,
		m:              m,
		pool:           pool,
		activeHandlers: make(map[*webHandlerState]struct{}),
	}
	if rpcCfg != nil {
		ws.rpc = NewRPCServer(rpcCfg, m, client, pool, router, l)
		ws.listenAll = rpcCfg.ListenAll
	}
	return ws
}

func (s *WebServer) processDownload(cd *capturedDownload) error {
	parsedURL, err := url.Parse(cd.Url)
	if err != nil {
		// net/url parse errors can embed the original input, including URL
		// userinfo. Do not wrap or return them across the extension boundary.
		return errors.New("invalid download URL")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("create cookie jar: %w", err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects),
	}
	if len(cd.Cookies) > 0 {
		s.l.Printf("[WS] Setting %d cookies for %s", len(cd.Cookies), parsedURL.Host)
		for i, c := range cd.Cookies {
			s.l.Printf("[WS]   cookie[%d]: Name=%q Domain=%q Path=%q", i, c.Name, c.Domain, c.Path)
		}
	}
	client.Jar.SetCookies(parsedURL, cd.Cookies)
	var (
		d              *warplib.Downloader
		poolGeneration *TransferGeneration
	)
	d, err = warplib.NewDownloader(client, cd.Url, &warplib.DownloaderOpts{
		Context:        s.m.TransferContext(),
		Headers:        cd.Headers,
		MaxConnections: 24,
		MaxSegments:    200,
		Handlers: &warplib.Handlers{
			DownloadProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				if poolGeneration == nil || !poolGeneration.IsRunnable() {
					return
				}
				poolGeneration.Broadcast(MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadProgress,
					Value:      int64(nread),
					Hash:       hash,
				}))
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				if poolGeneration == nil || !poolGeneration.IsRunnable() {
					return
				}
				poolGeneration.RecordTerminal(MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadComplete,
					Value:      tread,
					Hash:       hash,
				}))
			},
			DownloadStoppedHandler: func() {
				uid := d.GetHash()
				if poolGeneration == nil || !poolGeneration.IsRunnable() {
					return
				}
				poolGeneration.RecordTerminal(MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadStopped,
				}))
			},
			CompileStartHandler: func(hash string) {
				uid := d.GetHash()
				if poolGeneration == nil || !poolGeneration.IsRunnable() {
					return
				}
				poolGeneration.Broadcast(MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileStart,
					Hash:       hash,
				}))
			},
			CompileProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				if poolGeneration == nil || !poolGeneration.IsRunnable() {
					return
				}
				poolGeneration.Broadcast(MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileProgress,
					Value:      int64(nread),
					Hash:       hash,
				}))
			},
			CompileCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				if poolGeneration == nil || !poolGeneration.IsRunnable() {
					return
				}
				poolGeneration.Broadcast(MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileComplete,
					Value:      tread,
					Hash:       hash,
				}))
			},
		},
	})
	if err != nil {
		return err
	}
	queue := s.m.GetQueue()
	poolGeneration, reserved := s.pool.beginLegacyDownload(d.GetHash(), nil)
	if !reserved {
		return errors.Join(
			errors.New("download is already running or still stopping"),
			d.Close(),
		)
	}
	err = s.m.AddDownload(d, &warplib.AddDownloadOpts{
		AbsoluteLocation: d.GetDownloadDirectory(),
		// Queue callbacks may run synchronously. Finish pool registration
		// before explicitly enqueuing below.
		SkipQueue: queue != nil,
	})
	if err != nil {
		poolGeneration.Abort()
		return errors.Join(err, d.Close())
	}
	var runLease *warplib.RunLease
	if queue == nil {
		runLease, err = s.m.AcquireDownloadRunLease(d.GetHash(), d)
		if err != nil {
			poolGeneration.Abort()
			return err
		}
	}
	if queue != nil {
		queue.Add(d.GetHash(), warplib.PriorityNormal)
		waiting, closeErr := s.m.CloseWaitingDownloader(d.GetHash())
		if closeErr != nil {
			cleanupErr := s.cleanupDownloadRegistration(d, poolGeneration)
			return errors.Join(closeErr, cleanupErr)
		}
		if waiting && len(cd.Cookies) > 0 {
			cleanupErr := s.cleanupDownloadRegistration(d, poolGeneration)
			return errors.Join(
				errors.New("captured download with cookies cannot wait in the queue because cookie secrets are not persisted"),
				cleanupErr,
			)
		}
		return nil
	}
	if !s.m.GoTransfer(func(ctx context.Context) {
		s.startDownload(ctx, d, poolGeneration, runLease)
	}) {
		cleanupErr := errors.Join(
			runLease.Close(),
			s.cleanupDownloadRegistration(d, poolGeneration),
		)
		return errors.Join(warplib.ErrManagerShuttingDown, cleanupErr)
	}
	return nil
}

func (s *WebServer) cleanupManagedDownloadRegistration(d *warplib.Downloader) error {
	hash := d.GetHash()
	var closeErr error
	if item := s.m.GetItem(hash); item != nil {
		closeErr = item.CloseDownloader()
	} else {
		closeErr = d.Close()
	}
	s.m.ReleaseQueueSlot(hash)
	return errors.Join(closeErr, s.m.PurgeFailedDownload(hash))
}

func (s *WebServer) cleanupDownloadRegistration(d *warplib.Downloader, generation *TransferGeneration) error {
	hash := d.GetHash()
	cleanupErr := s.cleanupManagedDownloadRegistration(d)
	if generation != nil {
		generation.Abort()
	} else if s.pool != nil {
		s.pool.StopDownload(hash)
	}
	return cleanupErr
}

func (s *WebServer) startDownload(
	ctx context.Context,
	d *warplib.Downloader,
	generation *TransferGeneration,
	lease *warplib.RunLease,
) {
	err := normalizeServerTransferError(ctx, lease.StartContext(ctx))
	hash := d.GetHash()
	if err != nil {
		err = errors.Join(err, lease.Close())
	}
	if generation != nil && !generation.IsCurrent() {
		return
	}
	if err == nil {
		s.m.ReleaseQueueSlot(hash)
		if generation != nil {
			// Completion/stopped handlers normally remove the registration.
			// This covers a pre-start daemon cancellation with no callback.
			generation.Finish(nil)
		}
		return
	}
	s.l.Printf("Download %s failed (%T)", hash, err)
	if generation != nil {
		generation.WriteError(ErrorTypeCritical, err.Error())
	} else if s.pool != nil {
		s.pool.WriteError(hash, ErrorTypeCritical, err.Error())
	}
	s.m.ReleaseQueueSlot(hash)
	if item := s.m.GetItem(hash); item != nil && item.GetDownloaded() == 0 {
		_ = s.m.PurgeFailedDownload(hash)
	}
	if generation != nil {
		generation.Finish(MakeDownloadError(hash, err))
	} else if s.pool != nil {
		s.pool.BroadcastTerminal(hash, MakeDownloadError(hash, err))
	}
}

func (s *WebServer) handleConnection(conn *websocket.Conn) {
	if request := conn.Request(); request != nil {
		if !s.attachWebHandler(webHandlerFromContext(request.Context()), conn.Close) {
			return
		}
	}
	s.l.Println("[WS] New extension connection from:", conn.Request().RemoteAddr)
	defer func() {
		s.l.Println("[WS] Connection closed")
		conn.Close()
	}()
	for {
		var data []byte
		err := websocket.Message.Receive(conn, &data)
		if err != nil {
			if err == io.EOF {
				s.l.Println("[WS] Client disconnected (EOF)")
				return
			}
			s.l.Println("[WS] Error receiving message:", err)
			return
		}
		s.l.Printf("[WS] Received %d bytes", len(data))
		var cd capturedDownload
		err = json.Unmarshal(data, &cd)
		if err != nil {
			s.l.Printf("[WS] Error unmarshalling %d-byte payload: %v", len(data), err)
			continue
		}
		safeURL := sanitizedURL(cd.Url)
		s.l.Printf("[WS] Parsed download - URL: %s, Headers: %d, Cookies: %d", safeURL, len(cd.Headers), len(cd.Cookies))
		err = s.processDownload(&cd)
		if err != nil {
			s.l.Printf("[WS] Error processing download for %s", safeURL)
			continue
		}
		s.l.Printf("[WS] Download queued successfully: %s", safeURL)
	}
}

func sanitizedURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	if parsed.Opaque != "" {
		return parsed.Scheme + ":<opaque>"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func (s *WebServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", websocket.Handler(s.handleConnection))
	if s.rpc != nil {
		mux.Handle("/jsonrpc", s.rpc.bridge)
		mux.HandleFunc("/jsonrpc/ws", s.handleJSONRPCWebSocket)
	}
	return s.trackWebHandler(mux)
}

// handleJSONRPCWebSocket handles WebSocket upgrade at /jsonrpc/ws.
// Each connection gets its own jrpc2.Server with AllowPush for notifications.
func (s *WebServer) handleJSONRPCWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := cws.Accept(w, r, nil)
	if err != nil {
		s.l.Printf("WebSocket accept error: %v", err)
		return
	}
	if !s.attachWebHandler(webHandlerFromContext(r.Context()), conn.CloseNow) {
		return
	}

	ctx := r.Context()
	ch := &wsChannel{conn: conn, ctx: ctx}

	// Create jrpc2 server for this connection with push support
	srv := jrpc2.NewServer(s.rpc.methods(), &jrpc2.ServerOptions{
		AllowPush: true,
	})

	// Register for notifications
	if s.rpc.notifier != nil {
		s.rpc.notifier.Register(srv)
		defer s.rpc.notifier.Unregister(srv)
	}

	// Serve blocks until connection closes
	srv.Start(ch)
	_ = srv.Wait()
}

func (s *WebServer) addr() string {
	if s.listenAll {
		return fmt.Sprintf(":%d", s.port)
	}
	return fmt.Sprintf("127.0.0.1:%d", s.port)
}

func (s *WebServer) Start() error {
	server, listener, done, err := s.prepare()
	if err != nil {
		return err
	}
	if err := s.markServeStarted(server, done); err != nil {
		return err
	}
	return s.serve(server, listener, done)
}

// startAsync binds the web listener synchronously and only then starts its
// serve loop. Server.Start uses this to ensure bind failures are observable
// before the daemon begins accepting IPC requests.
func (s *WebServer) startAsync() (<-chan error, error) {
	server, listener, done, err := s.prepare()
	if err != nil {
		return nil, err
	}
	return s.startPrepared(server, listener, done)
}

func (s *WebServer) startPrepared(server *http.Server, listener net.Listener, done chan struct{}) (<-chan error, error) {
	if err := s.markServeStarted(server, done); err != nil {
		return nil, err
	}
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		result <- s.serve(server, listener, done)
		close(result)
	}()
	<-started
	return result, nil
}

func (s *WebServer) markServeStarted(server *http.Server, done chan struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != server || s.serveDone != done {
		return errors.New("web server preparation is no longer active")
	}
	if s.shuttingDown {
		return errors.New("web server is shutting down")
	}
	if s.serveStarted {
		return errors.New("web server serve loop is already started")
	}
	s.serveStarted = true
	return nil
}

func (s *WebServer) prepare() (*http.Server, net.Listener, chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil, nil, nil, errors.New("web server is already started")
	}

	server := &http.Server{
		Addr:    s.addr(),
		Handler: s.handler(),
	}
	listen := s.listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", server.Addr)
	if err != nil {
		return nil, nil, nil, err
	}

	if s.activeHandlers == nil {
		s.activeHandlers = make(map[*webHandlerState]struct{})
	}
	s.server = server
	s.listener = listener
	s.serveDone = make(chan struct{})
	s.serveStarted = false
	s.shuttingDown = false
	return server, listener, s.serveDone, nil
}

func (s *WebServer) serve(server *http.Server, listener net.Listener, done chan struct{}) error {
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	s.mu.Lock()
	if s.server == server {
		s.listener = nil
	}
	close(done)
	s.mu.Unlock()
	return err
}

// Shutdown gracefully stops the web server and cleans up RPC resources.
func (s *WebServer) Shutdown(ctx context.Context) error {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()

	s.mu.Lock()
	server := s.server
	listener := s.listener
	serveDone := s.serveDone
	serveStarted := s.serveStarted
	hasHandlers := len(s.activeHandlers) != 0
	if server != nil || hasHandlers {
		s.shuttingDown = true
	}
	s.mu.Unlock()

	s.rpcCloseOnce.Do(func() {
		if s.rpc != nil {
			s.rpc.Close()
		}
	})
	if server == nil && !hasHandlers {
		return nil
	}

	s.closeWebHandlers()

	var errs []error
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			errs = append(errs, err)
			if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				errs = append(errs, fmt.Errorf("force close HTTP server: %w", closeErr))
			}
		}
	}
	// Shutdown can race Serve before net/http has registered the listener.
	// Closing the pre-bound listener after setting Server's shutdown flag
	// guarantees the delayed Serve call cannot leak it.
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, fmt.Errorf("close web listener: %w", err))
		}
	}
	if !serveStarted && serveDone != nil {
		s.mu.Lock()
		if s.server == server && s.serveDone == serveDone && !s.serveStarted {
			close(serveDone)
		}
		s.mu.Unlock()
	}
	s.closeWebHandlers()

	handlersQuiesced, handlerErr := s.waitForWebHandlers(ctx)
	if handlerErr != nil {
		errs = append(errs, fmt.Errorf("wait for web handlers: %w", handlerErr))
	}
	serveQuiesced, serveErr := waitForWebDone(ctx, serveDone)
	if serveErr != nil {
		errs = append(errs, fmt.Errorf("wait for web serve loop: %w", serveErr))
	}

	if handlersQuiesced && serveQuiesced {
		s.mu.Lock()
		if s.server == server {
			s.server = nil
			s.listener = nil
			s.serveDone = nil
			s.serveStarted = false
		}
		s.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (s *WebServer) trackWebHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, ok := s.beginWebHandler()
		if !ok {
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
			return
		}
		defer s.finishWebHandler(state)
		ctx := context.WithValue(r.Context(), webHandlerContextKey{}, state)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *WebServer) beginWebHandler() (*webHandlerState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shuttingDown {
		return nil, false
	}
	if s.activeHandlers == nil {
		s.activeHandlers = make(map[*webHandlerState]struct{})
	}
	state := &webHandlerState{}
	s.activeHandlers[state] = struct{}{}
	s.handlerWG.Add(1)
	return state, true
}

func (s *WebServer) finishWebHandler(state *webHandlerState) {
	if state == nil {
		return
	}
	s.mu.Lock()
	if _, ok := s.activeHandlers[state]; !ok {
		s.mu.Unlock()
		return
	}
	delete(s.activeHandlers, state)
	s.mu.Unlock()
	s.handlerWG.Done()
}

func (s *WebServer) attachWebHandler(state *webHandlerState, closeFn func() error) bool {
	if state == nil {
		return true
	}
	s.mu.Lock()
	if _, ok := s.activeHandlers[state]; !ok {
		s.mu.Unlock()
		_ = closeFn()
		return false
	}
	state.close = closeFn
	shuttingDown := s.shuttingDown
	s.mu.Unlock()
	if shuttingDown {
		_ = closeFn()
		return false
	}
	return true
}

func webHandlerFromContext(ctx context.Context) *webHandlerState {
	state, _ := ctx.Value(webHandlerContextKey{}).(*webHandlerState)
	return state
}

func (s *WebServer) closeWebHandlers() {
	s.mu.Lock()
	closers := make([]func() error, 0, len(s.activeHandlers))
	for state := range s.activeHandlers {
		if state.close != nil {
			closers = append(closers, state.close)
		}
	}
	s.mu.Unlock()
	for _, closeFn := range closers {
		_ = closeFn()
	}
}

func (s *WebServer) waitForWebHandlers(ctx context.Context) (bool, error) {
	done := make(chan struct{})
	go func() {
		s.handlerWG.Wait()
		close(done)
	}()
	return waitForWebDone(ctx, done)
}

func waitForWebDone(ctx context.Context, done <-chan struct{}) (bool, error) {
	return waitForWebDoneWithTimeout(ctx, done, common.ShutdownTimeout)
}

func waitForWebDoneWithTimeout(ctx context.Context, done <-chan struct{}, fallbackTimeout time.Duration) (bool, error) {
	if done == nil {
		return true, nil
	}
	select {
	case <-done:
		return true, nil
	case <-ctx.Done():
	}

	fallbackCtx, cancel := context.WithTimeout(context.Background(), fallbackTimeout)
	defer cancel()
	select {
	case <-done:
		return true, ctx.Err()
	case <-fallbackCtx.Done():
		// Resolve a completion racing the fallback timer in favor of the
		// proven drain.
		select {
		case <-done:
			return true, ctx.Err()
		default:
		}
		return false, errors.Join(ErrDrainIncomplete, ctx.Err(), fallbackCtx.Err())
	}
}
