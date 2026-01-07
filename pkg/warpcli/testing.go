package warpcli

import (
	"net"
	"sync"

	"github.com/warpdl/warpdl/common"
)

// NewClientForTesting creates a Client with a custom connection for testing purposes.
// This allows tests to inject mock connections without needing a daemon.
func NewClientForTesting(conn net.Conn) *Client {
	return &Client{
		conn: conn,
		mu:   &sync.RWMutex{},
		d: &Dispatcher{
			Handlers: make(map[common.UpdateType][]Handler),
		},
	}
}

// ReadForTesting exposes the read function for testing purposes.
func ReadForTesting(conn net.Conn) ([]byte, error) {
	return read(conn)
}

// WriteForTesting exposes the write function for testing purposes.
func WriteForTesting(conn net.Conn, data []byte) error {
	return write(conn, data)
}
