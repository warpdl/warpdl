package warpcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/warpdl/warpdl/common"
)

// Client manages communication with the WarpDL daemon over IPC.
// It provides methods to invoke daemon operations and listen for asynchronous
// updates such as download progress notifications.
// The client automatically selects the best transport available:
// - Unix/Linux: Unix socket with TCP fallback
// - Windows: Named pipe with TCP fallback
type Client struct {
	mu     *sync.RWMutex
	d      *Dispatcher
	conn   net.Conn
	listen bool
}

var (
	ensureDaemonFunc = ensureDaemon
	dialFunc         = net.Dial
)

// NewClient creates a new client connection to the WarpDL daemon.
// It connects to the daemon using platform-specific IPC and returns a ready-to-use client.
// If the daemon is not running, it will be automatically spawned.
// Returns an error if the daemon cannot be started or connection fails.
func NewClient() (*Client, error) {
	if err := ensureDaemonFunc(); err != nil {
		return nil, err
	}

	// If force TCP mode is enabled, skip platform-specific dial
	if forceTCP() {
		debugLog("Force TCP mode enabled, connecting via TCP")
		conn, err := dialFunc("tcp", tcpAddress())
		if err != nil {
			return nil, fmt.Errorf("failed to connect via TCP: %w", err)
		}
		debugLog("Successfully connected via TCP to %s", tcpAddress())
		return newClientWithConn(conn), nil
	}

	// Use platform-specific dial (Unix socket or Named Pipe with fallback)
	conn, err := dial()
	if err != nil {
		return nil, err
	}
	return newClientWithConn(conn), nil
}

// newClientWithConn creates a new client with the provided connection.
func newClientWithConn(conn net.Conn) *Client {
	return &Client{
		conn: conn,
		mu:   &sync.RWMutex{},
		d: &Dispatcher{
			Handlers: make(map[common.UpdateType][]Handler),
		},
	}
}

// Listen starts the client's event loop to receive updates from the daemon.
// It blocks until Disconnect is called or an error occurs. Updates received
// are dispatched to registered handlers based on their update type.
// Returns an error if reading from the connection or processing updates fails.
func (c *Client) Listen() (err error) {
	defer c.conn.Close()
	c.listen = true
	for c.listen {
		c.mu.RLock()
		var buf []byte
		buf, err = read(c.conn)
		if err != nil {
			c.mu.RUnlock()
			err = fmt.Errorf("error reading: %s", err.Error())
			return
		}
		err = c.d.process(buf)
		if err != nil {
			c.mu.RUnlock()
			if err == ErrDisconnect {
				err = nil
				break
			}
			err = fmt.Errorf("error processing: %s", err.Error())
			return
		}
		c.mu.RUnlock()
	}
	return
}

// AddHandler registers a handler for the specified update type.
// Multiple handlers can be registered for the same update type and will
// be called in the order they were added.
func (c *Client) AddHandler(t common.UpdateType, h Handler) {
	c.d.AddHandler(t, h)
}

// RemoveHandler removes all handlers registered for the specified update type.
func (c *Client) RemoveHandler(t common.UpdateType) {
	c.d.RemoveHandler(t)
}

// Disconnect signals the client to stop listening for updates.
// The Listen method will return after the current update is processed.
func (c *Client) Disconnect() {
	c.listen = false
}

// Close closes the client's connection to the daemon.
// This should be called when the client is no longer needed,
// especially if Listen() will not be called.
// Safe to call multiple times (subsequent calls return an error but have no effect).
func (c *Client) Close() error {
	return c.conn.Close()
}

// invoke sends a request to the daemon and waits for a response.
// It blocks the update listener while waiting to ensure the response is received here
// instead of being dispatched to handlers.
func (c *Client) invoke(method common.UpdateType, message any) (json.RawMessage, error) {
	// block updates listener while invoking a method
	// to retrieve the message update here instead
	c.mu.Lock()
	defer c.mu.Unlock()
	buf, err := json.Marshal(&Request{
		Method:  method,
		Message: message,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to invoke %s: %s", method, err.Error())
	}
	err = write(c.conn, buf)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke %s: %s", method, err.Error())
	}
	buf, err = read(c.conn)
	if err != nil {
		return nil, fmt.Errorf("failed to invoke %s: %s", method, err.Error())
	}
	var res Response
	err = json.Unmarshal(buf, &res)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %s", method, err.Error())
	}
	if !res.Ok {
		return nil, errors.New(res.Error)
	}
	return res.Update.Message, nil
}
