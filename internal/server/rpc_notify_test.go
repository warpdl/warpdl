package server

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"

	jsonrpc "github.com/gumeniukcom/golang-jsonrpc2/v2"
	"github.com/gumeniukcom/golang-jsonrpc2/v2/structs"
)

// testPusher is an in-memory jsonrpc.Pusher standing in for a live WebSocket
// connection. Notify marshals the notification frame and delivers it on an
// unbuffered channel — so, like the synchronous pipe channel it replaces, a
// push blocks until the peer drains it. After Close, Notify fails like a
// dead connection.
type testPusher struct {
	mu     sync.Mutex
	closed bool
	ch     chan []byte
}

func newTestPusher(t *testing.T) (*testPusher, func()) {
	t.Helper()
	p := &testPusher{ch: make(chan []byte)}
	return p, p.Close
}

func (p *testPusher) Notify(_ context.Context, method string, params any) error {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return errors.New("connection closed")
	}
	rawParams, err := jsonrpc.MarshalParams(params)
	if err != nil {
		return err
	}
	frame, err := structs.Request{
		Version: jsonrpc.Version,
		Method:  method,
		Params:  rawParams,
	}.MarshalJSON()
	if err != nil {
		return err
	}
	p.ch <- frame
	return nil
}

// Recv blocks until a pushed notification frame arrives.
func (p *testPusher) Recv() []byte { return <-p.ch }

// Close simulates the client disconnecting: subsequent Notify calls fail.
func (p *testPusher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
}

func TestNewRPCNotifier(t *testing.T) {
	n := NewRPCNotifier(nil)
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
	if n.Count() != 0 {
		t.Fatalf("expected 0 pushers, got %d", n.Count())
	}
}

func TestNewRPCNotifier_WithLogger(t *testing.T) {
	l := log.New(io.Discard, "", 0)
	n := NewRPCNotifier(l)
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
}

func TestRPCNotifier_Register(t *testing.T) {
	n := NewRPCNotifier(nil)
	p, cleanup := newTestPusher(t)
	defer cleanup()

	n.Register(p)
	if n.Count() != 1 {
		t.Fatalf("expected 1 pusher, got %d", n.Count())
	}
}

func TestRPCNotifier_Unregister(t *testing.T) {
	n := NewRPCNotifier(nil)
	p, cleanup := newTestPusher(t)
	defer cleanup()

	n.Register(p)
	if n.Count() != 1 {
		t.Fatalf("expected 1 pusher after register, got %d", n.Count())
	}

	n.Unregister(p)
	if n.Count() != 0 {
		t.Fatalf("expected 0 pushers after unregister, got %d", n.Count())
	}
}

func TestRPCNotifier_Unregister_NotRegistered(t *testing.T) {
	n := NewRPCNotifier(nil)
	p, cleanup := newTestPusher(t)
	defer cleanup()

	// Unregistering a pusher that was never registered should not panic
	n.Unregister(p)
	if n.Count() != 0 {
		t.Fatalf("expected 0 pushers, got %d", n.Count())
	}
}

func TestRPCNotifier_Broadcast_NoServers(t *testing.T) {
	n := NewRPCNotifier(nil)
	// Broadcast with no pushers should not panic
	n.Broadcast("test.method", map[string]string{"key": "value"})
}

func TestRPCNotifier_Broadcast_Success(t *testing.T) {
	n := NewRPCNotifier(nil)
	p, cleanup := newTestPusher(t)
	defer cleanup()

	n.Register(p)

	// Drain the notification in a goroutine since the channel is synchronous
	done := make(chan []byte, 1)
	go func() {
		done <- p.Recv()
	}()

	// Broadcast should succeed (pusher is connected)
	n.Broadcast("download.started", &DownloadStartedNotification{
		GID:         "test-gid",
		FileName:    "file.bin",
		TotalLength: 1024,
	})

	// Wait for the notification to be received
	<-done

	// Pusher should still be registered
	if n.Count() != 1 {
		t.Fatalf("expected 1 pusher after successful broadcast, got %d", n.Count())
	}
}

func TestRPCNotifier_Broadcast_DisconnectedServer(t *testing.T) {
	l := log.New(io.Discard, "", 0)
	n := NewRPCNotifier(l)

	p, _ := newTestPusher(t)

	n.Register(p)

	// Close the client side to simulate disconnect
	p.Close()

	// Broadcast should remove the failed pusher
	n.Broadcast("download.error", &DownloadErrorNotification{
		GID:   "test-gid",
		Error: "connection lost",
	})

	if n.Count() != 0 {
		t.Fatalf("expected 0 pushers after disconnect, got %d", n.Count())
	}
}

func TestRPCNotifier_Broadcast_MultipleServers(t *testing.T) {
	n := NewRPCNotifier(nil)

	p1, cleanup1 := newTestPusher(t)
	defer cleanup1()
	p2, cleanup2 := newTestPusher(t)
	defer cleanup2()

	n.Register(p1)
	n.Register(p2)

	if n.Count() != 2 {
		t.Fatalf("expected 2 pushers, got %d", n.Count())
	}

	// Drain notifications concurrently
	done := make(chan struct{}, 2)
	go func() { p1.Recv(); done <- struct{}{} }()
	go func() { p2.Recv(); done <- struct{}{} }()

	n.Broadcast("download.progress", &DownloadProgressNotification{
		GID:             "gid-123",
		CompletedLength: 512,
	})

	<-done
	<-done

	// Both should still be registered
	if n.Count() != 2 {
		t.Fatalf("expected 2 pushers after broadcast, got %d", n.Count())
	}
}

func TestRPCNotifier_Broadcast_PartialFailure(t *testing.T) {
	l := log.New(io.Discard, "", 0)
	n := NewRPCNotifier(l)

	// Pusher 1: stays connected
	p1, cleanup1 := newTestPusher(t)
	defer cleanup1()

	// Pusher 2: will be disconnected
	p2, _ := newTestPusher(t)

	n.Register(p1)
	n.Register(p2)

	// Disconnect pusher 2
	p2.Close()

	// Drain notification from pusher 1 concurrently
	done := make(chan struct{}, 1)
	go func() { p1.Recv(); done <- struct{}{} }()

	// Broadcast should succeed for p1 and remove p2
	n.Broadcast("download.complete", &DownloadCompleteNotification{
		GID:         "gid-123",
		TotalLength: 1024,
	})

	<-done

	if n.Count() != 1 {
		t.Fatalf("expected 1 pusher after partial failure, got %d", n.Count())
	}
}

func TestRPCNotifier_ConcurrentRegisterUnregister(t *testing.T) {
	n := NewRPCNotifier(log.New(io.Discard, "", 0))
	var wg sync.WaitGroup

	// Concurrent register/unregister should not race
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, _ := newTestPusher(t)

			n.Register(p)
			_ = n.Count()
			n.Unregister(p)

			p.Close()
		}()
	}
	wg.Wait()

	if n.Count() != 0 {
		t.Fatalf("expected 0 pushers after concurrent register/unregister, got %d", n.Count())
	}
}

func TestRPCNotifier_Count(t *testing.T) {
	n := NewRPCNotifier(nil)

	if n.Count() != 0 {
		t.Fatalf("expected 0, got %d", n.Count())
	}

	pushers := make([]*testPusher, 3)

	for i := 0; i < 3; i++ {
		p, _ := newTestPusher(t)
		pushers[i] = p
		n.Register(p)
	}

	if n.Count() != 3 {
		t.Fatalf("expected 3, got %d", n.Count())
	}

	// Unregister one
	n.Unregister(pushers[1])
	if n.Count() != 2 {
		t.Fatalf("expected 2, got %d", n.Count())
	}

	// Cleanup
	for i := 0; i < 3; i++ {
		pushers[i].Close()
	}
}

func TestRPCNotifier_DoubleRegister(t *testing.T) {
	n := NewRPCNotifier(nil)
	p, cleanup := newTestPusher(t)
	defer cleanup()

	// Registering the same pusher twice should be idempotent (map key)
	n.Register(p)
	n.Register(p)
	if n.Count() != 1 {
		t.Fatalf("expected 1 pusher after double register, got %d", n.Count())
	}
}

// TestNotificationTypes verifies the notification param types can be used
// with Broadcast without errors.
func TestNotificationTypes(t *testing.T) {
	n := NewRPCNotifier(nil)
	p, cleanup := newTestPusher(t)
	defer cleanup()

	n.Register(p)

	tests := []struct {
		method string
		params any
	}{
		{"download.started", &DownloadStartedNotification{GID: "g1", FileName: "f.bin", TotalLength: 100}},
		{"download.progress", &DownloadProgressNotification{GID: "g1", CompletedLength: 50}},
		{"download.complete", &DownloadCompleteNotification{GID: "g1", TotalLength: 100}},
		{"download.error", &DownloadErrorNotification{GID: "g1", Error: "timeout"}},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			done := make(chan []byte, 1)
			go func() {
				done <- p.Recv()
			}()

			n.Broadcast(tt.method, tt.params)

			data := <-done
			if len(data) == 0 {
				t.Fatalf("expected notification data for %s, got empty", tt.method)
			}
		})
	}
}
