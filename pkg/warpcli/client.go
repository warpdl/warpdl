package warpcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/warpdl/warpdl/common"
)

// Client manages communication with the WarpDL daemon over IPC.
// It provides methods to invoke daemon operations and listen for asynchronous
// updates such as download progress notifications.
// The client automatically selects the best transport available:
// - Unix/Linux: Unix socket with TCP fallback
// - Windows: Named pipe with TCP fallback
type Client struct {
	// mu serializes reads and writes on conn. A daemon connection has no
	// request IDs, so only one goroutine may consume frames at a time.
	mu *sync.RWMutex
	d  *Dispatcher

	conn net.Conn

	stateMu sync.RWMutex
	listen  bool
	// pending holds broadcast frames that arrived while a request was in
	// flight (the daemon multiplexes broadcasts onto the same connection,
	// so they can precede the request's reply). invoke stashes them here;
	// Listen drains them before reading new frames. Guarded by mu: invoke
	// appends under the write lock, Listen pops via takePending.
	pending [][]byte
}

// maxInvokeSkew bounds how many interleaved broadcast frames invoke will
// skip while waiting for its reply, so a misbehaving daemon cannot make it
// spin (and accumulate memory) forever.
const maxInvokeSkew = 1024

// listenPollInterval bounds how long Listen owns the connection while no
// frame is available. This lets invoke take ownership without waiting behind
// an indefinitely blocked read.
const listenPollInterval = 50 * time.Millisecond

var (
	ensureDaemonFunc = ensureDaemon
	dialFunc         = net.Dial
	dialURIFunc      = dialURI
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

// NewClientWithURI creates a client using an explicit daemon URI.
// Unlike NewClient(), this does NOT spawn a daemon - it assumes the daemon exists.
// The URI should be in one of these formats:
//   - unix:///absolute/path/to/socket
//   - tcp://host:port (or tcp://host for default port 3849)
//   - pipe://name (Windows only)
//
// Returns an error if the URI is invalid or connection fails.
func NewClientWithURI(rawURI string) (*Client, error) {
	// Parse the URI
	uri, err := ParseDaemonURI(rawURI)
	if err != nil {
		return nil, fmt.Errorf("invalid daemon URI: %w", err)
	}

	// Connect using the parsed URI (does NOT ensure daemon is running)
	conn, err := dialURIFunc(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon at %s: %w", rawURI, err)
	}

	debugLog("Successfully connected to daemon via URI: %s", rawURI)
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
	if c.conn == nil {
		return errors.New("client connection is nil")
	}
	defer c.Close()
	c.setListening(true)
	defer c.setListening(false)
	for c.isListening() {
		// Replay broadcasts that overtook a reply during invoke before
		// reading new frames, so handlers observe events in wire order.
		if buf := c.takePending(); buf != nil {
			err = c.d.process(buf)
			if err != nil {
				if err == ErrDisconnect {
					err = nil
					break
				}
				err = fmt.Errorf("error processing: %s", err.Error())
				return
			}
			continue
		}

		// Poll only for the first byte of a frame. Once a frame has begun,
		// readAvailable clears the deadline and consumes it in full, so a
		// timeout can never discard a partial frame and desynchronize the
		// stream.
		c.mu.Lock()
		var buf []byte
		var available bool
		buf, available, err = readAvailable(c.conn, listenPollInterval)
		c.mu.Unlock()
		if err != nil {
			if !c.isListening() {
				return nil
			}
			err = fmt.Errorf("error reading: %s", err.Error())
			return
		}
		if !available {
			continue
		}
		err = c.d.process(buf)
		if err != nil {
			if err == ErrDisconnect {
				err = nil
				break
			}
			err = fmt.Errorf("error processing: %s", err.Error())
			return
		}
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
	c.setListening(false)
	// Closing the connection unblocks Listen immediately, including when it
	// is part-way through receiving a frame.
	_ = c.Close()
}

// Close closes the client's connection to the daemon.
// This should be called when the client is no longer needed,
// especially if Listen() will not be called.
// Safe to call multiple times (subsequent calls return an error but have no effect).
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// invoke sends a request to the daemon and waits for a response.
// It blocks the update listener while waiting to ensure the response is received here
// instead of being dispatched to handlers.
//
// The daemon multiplexes pool broadcasts onto the same connection, so the
// next frame after a request is not necessarily its reply: with the download
// queue enabled, progress events from an auto-started download can hit the
// wire before the RPC response is written. The reply is identified as the
// first frame whose update type matches the invoked method; error frames
// (ok=false) carry no type and always belong to the in-flight request.
// Broadcast frames read in the meantime are stashed for the Listen loop.
func (c *Client) invoke(method common.UpdateType, message any) (json.RawMessage, error) {
	// block updates listener while invoking a method
	// to retrieve the message update here instead
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("failed to invoke %s: clear read deadline: %w", method, err)
	}
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
	for skipped := 0; skipped <= maxInvokeSkew; skipped++ {
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
		if res.Update == nil {
			// Malformed success frame; not the reply and useless to
			// handlers - drop it.
			continue
		}
		if res.Update.Type == method {
			return res.Update.Message, nil
		}
		// A broadcast overtook the reply: keep it for Listen's dispatcher.
		c.pending = append(c.pending, buf)
	}
	return nil, fmt.Errorf("failed to invoke %s: no reply within %d frames", method, maxInvokeSkew)
}

func (c *Client) isListening() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.listen
}

func (c *Client) setListening(listen bool) {
	c.stateMu.Lock()
	c.listen = listen
	c.stateMu.Unlock()
}

// takePending pops the oldest frame stashed by invoke, or nil if none.
func (c *Client) takePending() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) == 0 {
		return nil
	}
	buf := c.pending[0]
	c.pending = c.pending[1:]
	return buf
}
