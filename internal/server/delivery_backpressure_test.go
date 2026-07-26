package server

import (
	"bytes"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func TestPoolAddDownloadPreservesExistingWatchers(t *testing.T) {
	pool := NewPool(nil)
	first := &SyncConn{}
	second := &SyncConn{}

	pool.AddDownload("id", first)
	pool.AddDownload("id", nil)
	pool.AddDownload("id", second)
	pool.AddDownload("id", first)

	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if got := pool.m["id"]; len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("watchers = %#v, want first and second exactly once", got)
	}
}

func TestPoolBroadcastSlowClientDoesNotBlock(t *testing.T) {
	pool := NewPool(nil)
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()
	pool.AddDownload("id", NewSyncConn(serverConn))

	start := time.Now()
	for i := 0; i < poolBroadcastQueueSize+2; i++ {
		pool.Broadcast("id", []byte("progress"))
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("broadcast callbacks blocked for %v", elapsed)
	}
	if !pool.HasDownload("id") {
		t.Fatal("queue pressure evicted the subscriber before a terminal update")
	}

	// A client that never reads is still evicted by the finite socket write
	// deadline; retaining it on queue pressure does not leak a writer.
	waitForCondition(t, 4*time.Second, func() bool {
		pool.mu.RLock()
		defer pool.mu.RUnlock()
		return len(pool.m["id"]) == 0
	})
}

func TestPoolBroadcastPreservesPerClientOrder(t *testing.T) {
	pool := NewPool(nil)
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()
	pool.AddDownload("id", NewSyncConn(serverConn))

	const messages = 20
	received := make(chan string, messages)
	go func() {
		peer := NewSyncConn(clientConn)
		for range messages {
			data, err := peer.Read()
			if err != nil {
				return
			}
			received <- string(data)
		}
	}()

	for i := 0; i < messages; i++ {
		pool.Broadcast("id", []byte{byte(i)})
	}
	for i := 0; i < messages; i++ {
		select {
		case got := <-received:
			if len(got) != 1 || got[0] != byte(i) {
				t.Fatalf("message %d = %v", i, []byte(got))
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for message %d", i)
		}
	}
}

func TestPoolWriteBroadcastedMessageHandlesShortWrites(t *testing.T) {
	pool := NewPool(nil)
	conn := &shortWriteConn{maxWrite: 2}
	payload := []byte("short writes still preserve the frame")
	head := intToBytes(uint32(len(payload)))

	if ok := pool.writeBroadcastedMessage(NewSyncConn(conn), head, payload); !ok {
		t.Fatal("writeBroadcastedMessage rejected successful short writes")
	}

	want := append(append([]byte(nil), head...), payload...)
	if got := conn.written(); !bytes.Equal(got, want) {
		t.Fatalf("framed bytes = %x, want %x", got, want)
	}
}

func TestPoolWriteBroadcastedMessageRejectsNoProgress(t *testing.T) {
	pool := NewPool(nil)
	conn := &shortWriteConn{maxWrite: 0}
	if ok := pool.writeBroadcastedMessage(NewSyncConn(conn), []byte{0, 0, 0, 1}, []byte("x")); ok {
		t.Fatal("writeBroadcastedMessage accepted a zero-byte write")
	}
}

func TestTransferGenerationFinishInterruptsWriterHoldingConnectionMutex(t *testing.T) {
	pool := NewPool(nil)
	conn := newDeadlineUnlockConn()
	syncConn := NewSyncConn(conn)
	generation, ok := pool.BeginDownload("id", syncConn)
	if !ok {
		t.Fatal("BeginDownload failed")
	}

	directWrite := make(chan error, 1)
	go func() {
		directWrite <- syncConn.Write([]byte("blocked response"))
	}()
	select {
	case <-conn.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("direct response writer did not block")
	}

	if !generation.RecordTerminal([]byte("terminal")) {
		t.Fatal("RecordTerminal failed")
	}
	finished := make(chan bool, 1)
	go func() {
		finished <- generation.Finish(nil)
	}()

	select {
	case ok := <-finished:
		if !ok {
			t.Fatal("Finish rejected current generation")
		}
	case <-time.After(time.Second):
		t.Fatal("Finish remained blocked behind an unbounded response write")
	}
	select {
	case err := <-directWrite:
		if err == nil {
			t.Fatal("deadline-interrupted response write unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("response writer retained the connection mutex")
	}
}

func TestPoolStopDownloadDrainsQueuedMessages(t *testing.T) {
	pool := NewPool(nil)
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()
	pool.AddDownload("id", NewSyncConn(serverConn))

	pool.Broadcast("id", []byte("final"))
	pool.StopDownload("id")

	payload, err := NewSyncConn(clientConn).Read()
	if err != nil {
		t.Fatalf("read queued final message: %v", err)
	}
	if string(payload) != "final" {
		t.Fatalf("queued final message = %q", payload)
	}
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	if _, ok := pool.m["id"]; ok {
		t.Fatal("stopped download remains registered")
	}
	for key := range pool.subs {
		if key.uid == "id" {
			t.Fatal("stopped download retains subscriber")
		}
	}
}

func TestPoolTerminalBroadcastReplacesProgressBacklog(t *testing.T) {
	pool := NewPool(nil)
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()
	pool.AddDownload("id", NewSyncConn(serverConn))

	for i := 0; i < poolBroadcastQueueSize*2; i++ {
		pool.Broadcast("id", []byte("progress"))
	}

	start := time.Now()
	pool.BroadcastTerminal("id", []byte("terminal"))
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("terminal broadcast blocked for %v", elapsed)
	}
	pool.Broadcast("id", []byte("late progress"))
	if pool.HasDownload("id") {
		t.Fatal("terminal download remains registered for future broadcasts")
	}
	pool.mu.RLock()
	for key := range pool.subs {
		if key.uid == "id" {
			pool.mu.RUnlock()
			t.Fatal("terminal download retains a subscriber")
		}
	}
	pool.mu.RUnlock()

	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	peer := NewSyncConn(clientConn)
	progressFrames := 0
	for range 2 {
		payload, err := peer.Read()
		if err != nil {
			t.Fatalf("terminal was not delivered before read failure: %v", err)
		}
		switch string(payload) {
		case "progress":
			progressFrames++
		case "terminal":
			if progressFrames > 1 {
				t.Fatalf("received %d stale progress frames before terminal", progressFrames)
			}
			return
		default:
			t.Fatalf("unexpected payload %q", payload)
		}
	}
	t.Fatal("terminal update was not delivered")
}

func TestPoolTerminalBroadcastConcurrentWithProgress(t *testing.T) {
	pool := NewPool(nil)
	serverConn, clientConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()
	pool.AddDownload("id", NewSyncConn(serverConn))

	terminalReceived := make(chan struct{})
	go func() {
		peer := NewSyncConn(clientConn)
		for {
			payload, err := peer.Read()
			if err != nil {
				return
			}
			if string(payload) == "terminal" {
				close(terminalReceived)
				return
			}
		}
	}()

	// Ensure the subscriber exists before racing terminal detachment against
	// ordinary sends.
	pool.Broadcast("id", []byte("progress"))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 32 {
				pool.Broadcast("id", []byte("progress"))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		pool.BroadcastTerminal("id", []byte("terminal"))
	}()
	close(start)
	wg.Wait()

	select {
	case <-terminalReceived:
	case <-time.After(time.Second):
		t.Fatal("terminal update was lost during concurrent progress broadcasts")
	}
}

func TestPoolOldWriterFailureDoesNotRemoveNewSubscription(t *testing.T) {
	pool := NewPool(nil)
	conn := newStaleWriterConn()
	syncConn := NewSyncConn(conn)
	pool.AddDownload("id", syncConn)
	pool.Broadcast("id", []byte("old"))

	select {
	case <-conn.firstWrite:
	case <-time.After(time.Second):
		t.Fatal("old writer did not start")
	}

	pool.StopDownload("id")
	pool.AddDownload("id", syncConn)
	pool.Broadcast("id", []byte("new"))
	close(conn.releaseFirst)

	var payload []byte
	deadline := time.After(time.Second)
	for string(payload) != "new" {
		select {
		case payload = <-conn.successfulWrites:
		case <-deadline:
			t.Fatal("new subscription did not deliver")
		}
	}

	pool.mu.RLock()
	watchers := len(pool.m["id"])
	pool.mu.RUnlock()
	if watchers != 1 {
		t.Fatalf("old writer removed %d new watchers", 1-watchers)
	}
	if conn.isClosed() {
		t.Fatal("old writer failure closed a connection reused by the new subscription")
	}
}

func TestPoolTerminalWriterFailureClosesDetachedConnection(t *testing.T) {
	pool := NewPool(nil)
	conn := newStaleWriterConn()
	syncConn := NewSyncConn(conn)
	pool.AddDownload("id", syncConn)
	pool.Broadcast("id", []byte("progress"))

	select {
	case <-conn.firstWrite:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}

	pool.BroadcastTerminal("id", []byte("terminal"))
	close(conn.releaseFirst)

	select {
	case <-conn.closedSignal:
	case <-time.After(time.Second):
		t.Fatal("failed detached terminal writer did not close its connection")
	}
}

type staleWriterConn struct {
	mu               sync.Mutex
	writes           int
	closed           bool
	firstWrite       chan struct{}
	releaseFirst     chan struct{}
	successfulWrites chan []byte
	closedSignal     chan struct{}
	closeOnce        sync.Once
}

type deadlineUnlockConn struct {
	writeStarted chan struct{}
	deadlineSet  chan struct{}
	writeOnce    sync.Once
	deadlineOnce sync.Once
}

func newDeadlineUnlockConn() *deadlineUnlockConn {
	return &deadlineUnlockConn{
		writeStarted: make(chan struct{}),
		deadlineSet:  make(chan struct{}),
	}
}

func (c *deadlineUnlockConn) Read([]byte) (int, error) { return 0, net.ErrClosed }

func (c *deadlineUnlockConn) Write([]byte) (int, error) {
	c.writeOnce.Do(func() { close(c.writeStarted) })
	<-c.deadlineSet
	return 0, os.ErrDeadlineExceeded
}

func (c *deadlineUnlockConn) Close() error {
	c.deadlineOnce.Do(func() { close(c.deadlineSet) })
	return nil
}

func (c *deadlineUnlockConn) LocalAddr() net.Addr  { return testAddr("local") }
func (c *deadlineUnlockConn) RemoteAddr() net.Addr { return testAddr("remote") }
func (c *deadlineUnlockConn) SetDeadline(deadline time.Time) error {
	return c.SetWriteDeadline(deadline)
}
func (c *deadlineUnlockConn) SetReadDeadline(time.Time) error { return nil }
func (c *deadlineUnlockConn) SetWriteDeadline(deadline time.Time) error {
	if !deadline.IsZero() {
		c.deadlineOnce.Do(func() { close(c.deadlineSet) })
	}
	return nil
}

func newStaleWriterConn() *staleWriterConn {
	return &staleWriterConn{
		firstWrite:       make(chan struct{}),
		releaseFirst:     make(chan struct{}),
		successfulWrites: make(chan []byte, 4),
		closedSignal:     make(chan struct{}),
	}
}

func (c *staleWriterConn) Read([]byte) (int, error) { return 0, net.ErrClosed }

func (c *staleWriterConn) Write(data []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.writes++
	write := c.writes
	c.mu.Unlock()
	if write == 1 {
		close(c.firstWrite)
		<-c.releaseFirst
		return 0, errors.New("old writer failed")
	}
	c.successfulWrites <- append([]byte(nil), data...)
	return len(data), nil
}

func (c *staleWriterConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.closeOnce.Do(func() { close(c.closedSignal) })
	return nil
}

func (c *staleWriterConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *staleWriterConn) LocalAddr() net.Addr              { return testAddr("local") }
func (c *staleWriterConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (c *staleWriterConn) SetDeadline(time.Time) error      { return nil }
func (c *staleWriterConn) SetReadDeadline(time.Time) error  { return nil }
func (c *staleWriterConn) SetWriteDeadline(time.Time) error { return nil }

type shortWriteConn struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	maxWrite int
}

func (c *shortWriteConn) Read([]byte) (int, error) { return 0, net.ErrClosed }

func (c *shortWriteConn) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(data) > c.maxWrite {
		data = data[:c.maxWrite]
	}
	return c.buf.Write(data)
}

func (c *shortWriteConn) written() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf.Bytes()...)
}

func (c *shortWriteConn) Close() error                     { return nil }
func (c *shortWriteConn) LocalAddr() net.Addr              { return testAddr("local") }
func (c *shortWriteConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (c *shortWriteConn) SetDeadline(time.Time) error      { return nil }
func (c *shortWriteConn) SetReadDeadline(time.Time) error  { return nil }
func (c *shortWriteConn) SetWriteDeadline(time.Time) error { return nil }

type testAddr string

func (a testAddr) Network() string { return string(a) }
func (a testAddr) String() string  { return string(a) }
