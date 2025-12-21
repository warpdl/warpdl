package warpcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/warpdl/warpdl/common"
)

// Client manages communication with the WarpDL daemon over a Unix socket.
// It provides methods to invoke daemon operations and listen for asynchronous
// updates such as download progress notifications.
type Client struct {
	mu     *sync.RWMutex
	d      *Dispatcher
	conn   net.Conn
	listen bool
}

// NewClient creates a new client connection to the WarpDL daemon.
// It connects to the daemon's Unix socket and returns a ready-to-use client.
// Returns an error if the connection to the daemon fails.
func NewClient() (*Client, error) {
	socketPath := socketPath()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		err = fmt.Errorf("error connecting to server: %s", err.Error())
		return nil, err
	}
	return &Client{
		conn: conn,
		mu:   &sync.RWMutex{},
		d: &Dispatcher{
			Handlers: make(map[common.UpdateType][]Handler),
		},
	}, nil
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
