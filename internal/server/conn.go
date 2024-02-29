package server

import (
	"net"
	"sync"
)

type SyncConn struct {
	Conn     net.Conn
	rmu, wmu sync.Mutex
}

func NewSyncConn(conn net.Conn) *SyncConn {
	return &SyncConn{
		Conn: conn,
	}
}

func (s *SyncConn) Write(b []byte) error {
	return write(&s.wmu, s.Conn, b)
}

func (s *SyncConn) Read() ([]byte, error) {
	return read(&s.rmu, s.Conn)
}
