package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// Server manages RPC connections from CLI clients over a Unix socket.
// It dispatches incoming requests to registered handlers and manages
// the connection pool for active downloads.
type Server struct {
	log     *log.Logger
	pool    *Pool
	ws      *WebServer
	handler map[common.UpdateType]HandlerFunc
	port    int
}

// NewServer creates a new Server instance with the given logger, download manager,
// and port number. The server uses a Unix socket as the primary transport,
// falling back to TCP on the specified port if Unix socket creation fails.
func NewServer(l *log.Logger, m *warplib.Manager, port int) *Server {
	pool := NewPool(l)
	return &Server{
		log:     l,
		pool:    pool,
		handler: make(map[common.UpdateType]HandlerFunc),
		port:    port,
		ws:      NewWebServer(l, m, pool, port+1),
	}
}

// RegisterHandler associates a handler function with a specific update type method.
// When a request with the given method is received, the corresponding handler is invoked.
func (s *Server) RegisterHandler(method common.UpdateType, handler HandlerFunc) {
	s.handler[method] = handler
}

func (s *Server) createListener() (net.Listener, error) {
	socketPath := socketPath()
	_ = os.Remove(socketPath)
	l, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: socketPath,
		Net:  "unix",
	})
	if err != nil {
		s.log.Println("Error occured while using unix socket: ", err.Error())
		s.log.Println("Trying to use tcp socket")
		tcpListener, tcpErr := net.Listen("tcp", fmt.Sprintf("localhost:%d", s.port))
		if tcpErr != nil {
			return nil, fmt.Errorf("error listening: %s", tcpErr.Error())
		}
		return tcpListener, nil
	}
	_ = os.Chmod(socketPath, 0766)
	return l, nil
}

// Start begins listening for incoming connections and blocks until an error occurs.
// It first starts the web server in a separate goroutine, then creates a Unix socket
// listener (falling back to TCP if necessary) and accepts connections in a loop.
// Each connection is handled in its own goroutine.
func (s *Server) Start() error {
	// todo: handle error
	go s.ws.Start()
	l, err := s.createListener()
	if err != nil {
		return err
	}
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			s.log.Println("Error accepting: ", err.Error())
			continue
		}
		// Handle connections in a new goroutine.
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	sconn := NewSyncConn(conn)
	defer conn.Close()
	for {
		buf, err := sconn.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			s.log.Println("Error reading:", err.Error())
			break
		}
		err = s.handlerWrapper(sconn, buf)
		if err != nil {
			s.log.Println("Error handling:", err.Error())
			break
		}
	}
}

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
