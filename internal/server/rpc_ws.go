package server

import (
	"context"
	"time"

	cws "github.com/coder/websocket"
)

const rpcWebSocketWriteTimeout = 2 * time.Second

// wsChannel adapts a coder/websocket.Conn to the jrpc2 Channel interface.
// Each WebSocket connection gets one wsChannel that bridges read/write
// operations between the WebSocket transport and the jrpc2 server.
type wsChannel struct {
	conn *cws.Conn
	ctx  context.Context
}

// Send writes a JSON-RPC message to the WebSocket connection.
func (c *wsChannel) Send(data []byte) error {
	ctx, cancel := context.WithTimeout(c.ctx, rpcWebSocketWriteTimeout)
	defer cancel()
	return c.conn.Write(ctx, cws.MessageText, data)
}

// Recv reads a JSON-RPC message from the WebSocket connection.
func (c *wsChannel) Recv() ([]byte, error) {
	_, data, err := c.conn.Read(c.ctx)
	return data, err
}

// Close shuts down the WebSocket connection with a normal closure status.
func (c *wsChannel) Close() error {
	return c.conn.Close(cws.StatusNormalClosure, "")
}
