package warpcli

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

// newPipeClient returns a Client wired to one end of a net.Pipe and the
// daemon-side conn for the test to script frames on.
func newPipeClient(t *testing.T) (*Client, net.Conn) {
	t.Helper()
	cliConn, daemonConn := net.Pipe()
	t.Cleanup(func() {
		_ = cliConn.Close()
		_ = daemonConn.Close()
	})
	c := &Client{
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
		conn: cliConn,
	}
	return c, daemonConn
}

// writeFrame marshals a Response and writes it with the length-prefixed
// framing the daemon uses.
func writeFrame(t *testing.T, conn net.Conn, res Response) {
	t.Helper()
	buf, err := json.Marshal(res)
	if err != nil {
		t.Errorf("marshal frame: %v", err)
		return
	}
	if err := write(conn, buf); err != nil {
		t.Errorf("write frame: %v", err)
	}
}

func broadcastFrame(t *testing.T, typ common.UpdateType, payload string) Response {
	t.Helper()
	return Response{
		Ok:     true,
		Update: &Update{Type: typ, Message: json.RawMessage(payload)},
	}
}

func downloadErrorFrame(t *testing.T, downloadID, message string) Response {
	t.Helper()
	payload, err := json.Marshal(common.DownloadErrorResponse{
		DownloadId: downloadID,
		Error:      message,
	})
	if err != nil {
		t.Fatalf("marshal download error: %v", err)
	}
	return broadcastFrame(t, common.UPDATE_DOWNLOAD_ERROR, string(payload))
}

// TestInvoke_SkipsBroadcastFramesBeforeReply reproduces the queue-enabled
// daemon race: when a download is auto-started while the download RPC is
// still in flight, progress broadcasts can reach the socket before the RPC
// reply. invoke must not mistake such a broadcast for the reply; it must
// keep reading until the frame type matches the invoked method and hand the
// skipped broadcasts to the Listen loop afterwards.
func TestInvoke_SkipsBroadcastFramesBeforeReply(t *testing.T) {
	c, daemon := newPipeClient(t)

	go func() {
		if _, err := read(daemon); err != nil { // consume the request
			t.Errorf("daemon read: %v", err)
			return
		}
		// Two broadcasts sneak in ahead of the reply.
		writeFrame(t, daemon, broadcastFrame(t, common.UPDATE_DOWNLOADING, `{"hash":"main","value":42}`))
		writeFrame(t, daemon, broadcastFrame(t, common.UPDATE_DOWNLOADING, `{"hash":"main","value":84}`))
		writeFrame(t, daemon, broadcastFrame(t, common.UPDATE_DOWNLOAD, `{"download_id":"abc123","file_name":"f.bin"}`))
	}()

	raw, err := c.invoke(common.UPDATE_DOWNLOAD, struct{}{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	var got struct {
		DownloadID string `json:"download_id"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if got.DownloadID != "abc123" {
		t.Fatalf("invoke returned the wrong frame as reply: %s", string(raw))
	}

	// The skipped broadcasts must be replayed to handlers once Listen runs.
	var mu sync.Mutex
	var seen []int
	c.AddHandler(common.UPDATE_DOWNLOADING, HandlerFunc(func(m json.RawMessage) error {
		var p struct {
			Value int `json:"value"`
		}
		if err := json.Unmarshal(m, &p); err != nil {
			return err
		}
		mu.Lock()
		seen = append(seen, p.Value)
		done := len(seen) == 2
		mu.Unlock()
		if done {
			return ErrDisconnect
		}
		return nil
	}))

	listenErr := make(chan error, 1)
	go func() { listenErr <- c.Listen() }()
	select {
	case err := <-listenErr:
		if err != nil {
			t.Fatalf("Listen: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Listen did not replay stashed broadcasts")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != 42 || seen[1] != 84 {
		t.Fatalf("stashed broadcasts replayed wrong: %v", seen)
	}
}

// TestInvoke_ErrorReplySurfaced ensures an ok=false frame (which carries no
// update type) is still treated as the reply to the in-flight request.
func TestInvoke_ErrorReplySurfaced(t *testing.T) {
	c, daemon := newPipeClient(t)

	go func() {
		if _, err := read(daemon); err != nil {
			t.Errorf("daemon read: %v", err)
			return
		}
		writeFrame(t, daemon, broadcastFrame(t, common.UPDATE_DOWNLOADING, `{"value":1}`))
		writeFrame(t, daemon, Response{Ok: false, Error: "boom"})
	}()

	_, err := c.invoke(common.UPDATE_DOWNLOAD, struct{}{})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected error reply 'boom', got %v", err)
	}
}

func TestInvoke_SkipsAsyncErrorAndListenDispatchesIt(t *testing.T) {
	c, daemon := newPipeClient(t)

	go func() {
		if _, err := read(daemon); err != nil {
			t.Errorf("daemon read: %v", err)
			return
		}
		writeFrame(t, daemon, downloadErrorFrame(t, "background-id", "background failed"))
		writeFrame(t, daemon, broadcastFrame(t, common.UPDATE_LIST, `{"items":[]}`))
	}()

	raw, err := c.invoke(common.UPDATE_LIST, nil)
	if err != nil {
		t.Fatalf("invoke treated async failure as its RPC error: %v", err)
	}
	if string(raw) != `{"items":[]}` {
		t.Fatalf("invoke returned wrong reply: %s", raw)
	}

	var got common.DownloadErrorResponse
	c.AddHandler(common.UPDATE_DOWNLOAD_ERROR, HandlerFunc(func(message json.RawMessage) error {
		if err := json.Unmarshal(message, &got); err != nil {
			return err
		}
		return ErrDisconnect
	}))
	if err := c.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if got.DownloadId != "background-id" || got.Error != "background failed" {
		t.Fatalf("listener received wrong async error: %+v", got)
	}
}

func TestInvoke_AsyncErrorDoesNotMaskRPCError(t *testing.T) {
	c, daemon := newPipeClient(t)

	go func() {
		if _, err := read(daemon); err != nil {
			t.Errorf("daemon read: %v", err)
			return
		}
		writeFrame(t, daemon, downloadErrorFrame(t, "background-id", "background failed"))
		writeFrame(t, daemon, Response{Ok: false, Error: "list request rejected"})
	}()

	_, err := c.invoke(common.UPDATE_LIST, nil)
	if err == nil || err.Error() != "list request rejected" {
		t.Fatalf("expected the actual RPC error, got %v", err)
	}
}

func TestListen_UnhandledAsyncErrorReturnsUnderlyingFailure(t *testing.T) {
	c, daemon := newPipeClient(t)

	go writeFrame(t, daemon, downloadErrorFrame(t, "download-id", "disk is full"))

	err := c.Listen()
	if err == nil || !strings.Contains(err.Error(), "disk is full") {
		t.Fatalf("expected underlying async failure, got %v", err)
	}
}

func TestListen_PoolBacklogReportsTerminalErrorInsteadOfEOF(t *testing.T) {
	clientConn, daemonConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = daemonConn.Close()
	})
	client := newClientWithConn(clientConn)
	pool := server.NewPool(nil)
	pool.AddDownload("download-id", server.NewSyncConn(daemonConn))

	progress := server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
		DownloadId: "download-id",
		Action:     common.DownloadProgress,
		Value:      1,
	})
	for range 128 {
		pool.Broadcast("download-id", progress)
	}
	pool.BroadcastTerminal(
		"download-id",
		server.MakeDownloadError("download-id", errors.New("disk is full")),
	)

	// A writer may already own one progress frame when terminal delivery
	// discards the queue. Accept it, then require the typed error.
	client.AddHandler(common.UPDATE_DOWNLOADING, HandlerFunc(func(json.RawMessage) error {
		return nil
	}))
	err := client.Listen()
	if err == nil || !strings.Contains(err.Error(), "disk is full") {
		t.Fatalf("Listen returned transport EOF instead of terminal failure: %v", err)
	}
}

// TestInvoke_TooManyInterleavedFrames ensures the skip loop is bounded and
// reports a protocol error instead of spinning forever.
func TestInvoke_TooManyInterleavedFrames(t *testing.T) {
	c, daemon := newPipeClient(t)

	go func() {
		if _, err := read(daemon); err != nil {
			return
		}
		for {
			buf, err := json.Marshal(broadcastFrame(t, common.UPDATE_DOWNLOADING, `{}`))
			if err != nil {
				return
			}
			if err := write(daemon, buf); err != nil {
				return // client gave up and the pipe closed: expected
			}
		}
	}()

	if _, err := c.invoke(common.UPDATE_DOWNLOAD, struct{}{}); err == nil {
		t.Fatal("expected invoke to fail after too many interleaved frames")
	}
	_ = c.conn.Close() // unblock the writer goroutine
}

type readSignalConn struct {
	net.Conn
	once    sync.Once
	started chan struct{}
}

func (c *readSignalConn) Read(buf []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Read(buf)
}

// TestListenDoesNotBlockInvoke reproduces the original deadlock: Listen owns
// the connection while waiting for an event, then an RPC invocation needs the
// same connection to send its request. Listen must yield ownership promptly.
func TestListenDoesNotBlockInvoke(t *testing.T) {
	clientConn, daemonConn := net.Pipe()
	signaledConn := &readSignalConn{
		Conn:    clientConn,
		started: make(chan struct{}),
	}
	c := newClientWithConn(signaledConn)
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = daemonConn.Close()
	})

	listenDone := make(chan error, 1)
	go func() { listenDone <- c.Listen() }()

	select {
	case <-signaledConn.started:
	case <-time.After(time.Second):
		t.Fatal("Listen did not begin reading")
	}

	daemonDone := make(chan error, 1)
	go func() {
		if _, err := read(daemonConn); err != nil {
			daemonDone <- err
			return
		}
		buf, err := json.Marshal(Response{
			Ok: true,
			Update: &Update{
				Type:    common.UPDATE_LIST,
				Message: json.RawMessage(`{}`),
			},
		})
		if err == nil {
			err = write(daemonConn, buf)
		}
		daemonDone <- err
	}()

	invokeDone := make(chan error, 1)
	go func() {
		_, err := c.invoke(common.UPDATE_LIST, nil)
		invokeDone <- err
	}()

	select {
	case err := <-invokeDone:
		if err != nil {
			t.Fatalf("invoke while listening: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("invoke deadlocked behind Listen")
	}
	if err := <-daemonDone; err != nil {
		t.Fatalf("daemon exchange: %v", err)
	}

	c.Disconnect()
	select {
	case err := <-listenDone:
		if err != nil {
			t.Fatalf("Listen after Disconnect: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Disconnect did not unblock Listen")
	}
}
