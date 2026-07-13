package server

import (
	"context"
	"sync"
	"time"

	cws "github.com/coder/websocket"

	jsonrpc "github.com/gumeniukcom/golang-jsonrpc2/v2"
	"github.com/gumeniukcom/golang-jsonrpc2/v2/structs"
)

// Limits for one /jsonrpc/ws connection, mirroring the defaults of the
// golang-jsonrpc2 jsonrpcws transport adapter.
const (
	wsMaxMessageSize     = 1 << 20 // 1 MiB per incoming frame
	wsMaxConcurrentCalls = 16      // in-flight frames dispatched per connection
	wsWriteTimeout       = 10 * time.Second
)

// wsWriter serializes all writes to one WebSocket connection (response
// frames and pushed notifications) under a mutex and bounds each write with
// a timeout. Writes are timed off the connection's own context, not any
// caller's request context, so a handler that pushes with an already-canceled
// context cannot fail the write. A genuine write failure (dead or slow peer)
// invokes onError, which tears the connection down so callers stop queueing
// behind the mutex.
type wsWriter struct {
	conn    *cws.Conn
	baseCtx context.Context //nolint:containedctx // connection-lifetime ctx, deliberately held
	onError func()

	mu sync.Mutex
}

func (w *wsWriter) write(frame []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	wctx, cancel := context.WithTimeout(w.baseCtx, wsWriteTimeout)
	defer cancel()

	if err := w.conn.Write(wctx, cws.MessageText, frame); err != nil {
		w.onError()
		return err
	}
	return nil
}

// wsPusher is the per-connection jsonrpc.Pusher: it sends server-initiated
// JSON-RPC notifications (requests without an id) to the client through the
// connection's shared serialized writer. It is registered with the
// RPCNotifier for the life of the connection; once the connection is gone,
// Notify returns an error and the notifier prunes it on broadcast.
type wsPusher struct {
	w *wsWriter
}

// Notify sends a server-initiated notification to the client. Safe for
// concurrent use. The ctx is accepted for jsonrpc.Pusher symmetry but the
// write is bounded by the connection's own context instead.
func (p *wsPusher) Notify(_ context.Context, method string, params any) error {
	rawParams, err := jsonrpc.MarshalParams(params)
	if err != nil {
		return err
	}
	// A notification is a request with no id member.
	frame, err := structs.Request{
		Version: jsonrpc.Version,
		Method:  method,
		Params:  rawParams,
	}.MarshalJSON()
	if err != nil {
		return err
	}
	return p.w.write(frame)
}

// interface guard
var _ jsonrpc.Pusher = (*wsPusher)(nil)

// serveJSONRPCWS drives one upgraded /jsonrpc/ws connection: it registers
// the connection's pusher with the notifier (unregistering on disconnect),
// reads JSON-RPC frames, and dispatches each frame on its own goroutine —
// bounded by wsMaxConcurrentCalls — so a slow method cannot stall progress
// queries on the same connection (jrpc2 dispatched concurrently too).
// Responses may therefore arrive in any order; JSON-RPC correlates by id.
// It blocks until the read side ends (client close, drop, or an oversized
// frame), cancels in-flight calls, and waits for them to drain.
func serveJSONRPCWS(rctx context.Context, rpc *jsonrpc.JSONRPC, conn *cws.Conn, notifier *RPCNotifier) {
	conn.SetReadLimit(wsMaxMessageSize)

	// rctx is NOT canceled when a hijacked connection drops — net/http
	// cancels it only after the handler returns. The connection runs its own
	// lifecycle: cancel fires as soon as the read side ends, canceling
	// in-flight handlers and unblocking writers.
	ctx, cancel := context.WithCancel(rctx)
	defer cancel()

	cw := &wsWriter{
		conn:    conn,
		baseCtx: ctx,
		onError: func() { cancel(); _ = conn.CloseNow() },
	}
	pusher := &wsPusher{w: cw}

	if notifier != nil {
		notifier.Register(pusher)
		defer notifier.Unregister(pusher)
	}

	var wg sync.WaitGroup
	slots := make(chan struct{}, wsMaxConcurrentCalls)

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			// Client closed, dropped, read limit exceeded, or the connection
			// was torn down by a failed write.
			break
		}

		slots <- struct{}{}
		wg.Add(1)
		go func(data []byte) {
			defer wg.Done()
			defer func() { <-slots }()

			// Inject the pusher so handlers could send server-initiated
			// notifications over this connection while they run.
			reqCtx := jsonrpc.ContextWithPusher(ctx, pusher)
			resp := rpc.HandleRPCJSONRawMessage(reqCtx, data)
			if len(resp) == 0 {
				return // notification: no response frame
			}
			_ = cw.write(resp)
		}(data)
	}

	cancel()
	wg.Wait()
	_ = conn.Close(cws.StatusNormalClosure, "")
}
