package server

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
)

type Server struct {
	port    int
	log     *log.Logger
	pool    *Pool
	handler map[string]HandlerFunc
}

func NewServer(l *log.Logger, port int) *Server {
	return &Server{
		port:    port,
		log:     l,
		pool:    NewPool(l),
		handler: make(map[string]HandlerFunc),
	}
}

func (s *Server) RegisterHandler(method string, handler HandlerFunc) {
	s.handler[method] = handler
}

func (s *Server) Start() error {
	socketPath := filepath.Join(os.TempDir(), "warpdl.sock")
	_ = os.Remove(socketPath)
	l, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: socketPath,
		Net:  "unix",
	})
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
	defer conn.Close()
	for {
		buf, err := read(conn)
		if err != nil {
			s.log.Println("Error reading:", err.Error())
			break
		}
		err = s.handlerWrapper(conn, buf)
		if err != nil {
			s.log.Println("Error handling:", err.Error())
			break
		}
	}
}

func (s *Server) handlerWrapper(conn net.Conn, b []byte) error {
	req, err := ParseRequest(b)
	if err != nil {
		return fmt.Errorf("error parsing request: %s", err.Error())
	}
	rHandler, ok := s.handler[req.Method]
	if !ok {
		err = write(conn, CreateError("unknown method: "+req.Method))
		if err != nil {
			return fmt.Errorf("error writing response: %s", err.Error())
		}
		return nil
	}
	msg, err := rHandler(conn, s.pool, req.Message)
	if err != nil {
		err = write(conn, InitError(err))
		if err != nil {
			return fmt.Errorf("error writing response: %s", err.Error())
		}
		return nil
	}
	err = write(conn, MakeResult(msg))
	if err != nil {
		return fmt.Errorf("error writing response: %s", err.Error())
	}
	return nil
}
