package server

import (
	"net"
	"sync"
)

// SyncConn wraps a net.Conn with mutex-protected read and write operations.
// It ensures thread-safe access to the underlying connection when multiple
// goroutines need to read from or write to the same connection.
type SyncConn struct {
	Conn     net.Conn
	rmu, wmu sync.Mutex
}

// NewSyncConn creates a new SyncConn wrapping the given network connection.
// The returned SyncConn is ready for concurrent read and write operations.
func NewSyncConn(conn net.Conn) *SyncConn {
	return &SyncConn{
		Conn: conn,
	}
}

// Write sends the given byte slice to the connection in a thread-safe manner.
// It acquires a write lock before writing to prevent concurrent writes.
func (s *SyncConn) Write(b []byte) error {
	return write(&s.wmu, s.Conn, b)
}

// Read receives data from the connection in a thread-safe manner.
// It acquires a read lock before reading to prevent concurrent reads.
func (s *SyncConn) Read() ([]byte, error) {
	return read(&s.rmu, s.Conn)
}
