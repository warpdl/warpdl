package server

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
)

type Server struct {
	log     *log.Logger
	pool    *Pool
	handler map[string]HandlerFunc
}

func NewServer(l *log.Logger) *Server {
	return &Server{
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
		sconn := NewSyncConn(conn)
		buf, err := sconn.Read()
		if err != nil {
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
		err = sconn.Write(CreateError("unknown method: " + req.Method))
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
