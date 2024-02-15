package server

import (
	"bufio"
	"fmt"
	"log"
	"net"
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
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
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
		go func(conn net.Conn) {
			for {
				b, err := bufio.NewReader(conn).ReadBytes(0)
				if err != nil {
					s.log.Println("Error reading:", err.Error())
					return
				}
				_ = s.handlerWrapper(conn, b[:len(b)-1])
				s.transmitEnd(conn)
			}
		}(conn)
	}
}

func (s *Server) transmitEnd(conn net.Conn) {
	conn.Write([]byte{0})
}

func (s *Server) handlerWrapper(conn net.Conn, b []byte) bool {
	req, err := ParseRequest(b)
	if err != nil {
		s.log.Println("Error parsing request:", err.Error())
		return false
	}
	rHandler, ok := s.handler[req.Method]
	if !ok {
		conn.Write(CreateError("unknown method: " + req.Method))
		return false
	}
	msg, err := rHandler(conn, s.pool, req.Message)
	if err != nil {
		conn.Write(InitError(err))
		return false
	}
	conn.Write(MakeResult(msg))
	return true
}
