package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

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
}

// NewServer creates a new Server instance with the given logger, download manager,
// and port number. The server uses platform-specific IPC as the primary transport,
// falling back to TCP on the specified port if the primary transport fails.
func NewServer(l *log.Logger, m *warplib.Manager, port int, rpcCfg *RPCConfig) *Server {
	pool := NewPool(l)
	return &Server{
		log:     l,
		pool:    pool,
		handler: make(map[common.UpdateType]HandlerFunc),
		port:    port,
		ws:      NewWebServer(l, m, pool, port+1, rpcCfg),
	}
}

// RegisterHandler associates a handler function with a specific update type method.
// When a request with the given method is received, the corresponding handler is invoked.
func (s *Server) RegisterHandler(method common.UpdateType, handler HandlerFunc) {
	s.handler[method] = handler
}

// Start begins listening for incoming connections and blocks until the context is canceled.
// It first starts the web server in a separate goroutine, then creates a platform-specific
// listener (Unix socket/named pipe with TCP fallback) and accepts connections in a loop.
// Each connection is handled in its own goroutine.
func (s *Server) Start(ctx context.Context) error {
	// Start web server in background
	go s.ws.Start()

	l, err := s.createListener()
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.listener = l
	s.mu.Unlock()

	// Watch for context cancellation to trigger shutdown
	go func() {
		<-ctx.Done()
		s.Shutdown()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-ctx.Done():
				return nil // Clean shutdown
			default:
			}
			s.log.Println("Error accepting connection:", err.Error())
			continue
		}
		// Handle connections in a new goroutine.
		go s.handleConnection(conn)
	}
}

// Shutdown gracefully stops the server by closing the listener and cleaning up resources.
// It uses common.ShutdownTimeout for the web server shutdown timeout.
func (s *Server) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			s.log.Printf("Error closing listener: %v", err)
		}
		s.listener = nil
	}

	// Shutdown web server with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), common.ShutdownTimeout)
	defer cancel()
	if err := s.ws.Shutdown(shutdownCtx); err != nil {
		s.log.Printf("Error shutting down web server: %v", err)
	}

	// Clean up socket/pipe using platform-specific cleanup
	if err := cleanupSocket(); err != nil {
		s.log.Printf("Error cleaning up socket: %v", err)
	}

	return nil
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
