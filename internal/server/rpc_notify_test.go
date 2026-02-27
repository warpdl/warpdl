package server

import (
	"io"
	"log"
	"sync"
	"testing"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/channel"
	"github.com/creachadair/jrpc2/handler"
)

// newTestServer creates a jrpc2 server with push support backed by an
// io.Pipe-based channel. Returns the client channel (for draining), the
// server, and a cleanup function. The client channel must be drained or
// closed to avoid blocking the server's push operations.
func newTestServer(t *testing.T) (channel.Channel, *jrpc2.Server, func()) {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	cli := channel.Line(cr, cw)
	srvCh := channel.Line(sr, sw)

	srv := jrpc2.NewServer(handler.Map{}, &jrpc2.ServerOptions{AllowPush: true})
	srv.Start(srvCh)

	cleanup := func() {
		cli.Close()
		_ = srv.Wait()
	}
	return cli, srv, cleanup
}

func TestNewRPCNotifier(t *testing.T) {
	n := NewRPCNotifier(nil)
	if n == nil {
		t.Fatal("expected non-nil notifier")
	}
	if n.Count() != 0 {
		t.Fatalf("expected 0 servers, got %d", n.Count())
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
	cli, srv, cleanup := newTestServer(t)
	defer cleanup()
	_ = cli // prevent unused

	n.Register(srv)
	if n.Count() != 1 {
		t.Fatalf("expected 1 server, got %d", n.Count())
	}
}

func TestRPCNotifier_Unregister(t *testing.T) {
	n := NewRPCNotifier(nil)
	cli, srv, cleanup := newTestServer(t)
	defer cleanup()
	_ = cli

	n.Register(srv)
	if n.Count() != 1 {
		t.Fatalf("expected 1 server after register, got %d", n.Count())
	}

	n.Unregister(srv)
	if n.Count() != 0 {
		t.Fatalf("expected 0 servers after unregister, got %d", n.Count())
	}
}

func TestRPCNotifier_Unregister_NotRegistered(t *testing.T) {
	n := NewRPCNotifier(nil)
	cli, srv, cleanup := newTestServer(t)
	defer cleanup()
	_ = cli

	// Unregistering a server that was never registered should not panic
	n.Unregister(srv)
	if n.Count() != 0 {
		t.Fatalf("expected 0 servers, got %d", n.Count())
	}
}

func TestRPCNotifier_Broadcast_NoServers(t *testing.T) {
	n := NewRPCNotifier(nil)
	// Broadcast with no servers should not panic
	n.Broadcast("test.method", map[string]string{"key": "value"})
}

func TestRPCNotifier_Broadcast_Success(t *testing.T) {
	n := NewRPCNotifier(nil)
	cli, srv, cleanup := newTestServer(t)
	defer cleanup()

	n.Register(srv)

	// Drain the notification in a goroutine since the channel is synchronous
	done := make(chan []byte, 1)
	go func() {
		data, _ := cli.Recv()
		done <- data
	}()

	// Broadcast should succeed (server is connected)
	n.Broadcast("download.started", &DownloadStartedNotification{
		GID:         "test-gid",
		FileName:    "file.bin",
		TotalLength: 1024,
	})

	// Wait for the notification to be received
	<-done

	// Server should still be registered
	if n.Count() != 1 {
		t.Fatalf("expected 1 server after successful broadcast, got %d", n.Count())
	}
}

func TestRPCNotifier_Broadcast_DisconnectedServer(t *testing.T) {
	l := log.New(io.Discard, "", 0)
	n := NewRPCNotifier(l)

	cli, srv, _ := newTestServer(t)

	n.Register(srv)

	// Close the client side to simulate disconnect
	cli.Close()
	_ = srv.Wait()

	// Broadcast should remove the failed server
	n.Broadcast("download.error", &DownloadErrorNotification{
		GID:   "test-gid",
		Error: "connection lost",
	})

	if n.Count() != 0 {
		t.Fatalf("expected 0 servers after disconnect, got %d", n.Count())
	}
}

func TestRPCNotifier_Broadcast_MultipleServers(t *testing.T) {
	n := NewRPCNotifier(nil)

	cli1, srv1, cleanup1 := newTestServer(t)
	defer cleanup1()
	cli2, srv2, cleanup2 := newTestServer(t)
	defer cleanup2()

	n.Register(srv1)
	n.Register(srv2)

	if n.Count() != 2 {
		t.Fatalf("expected 2 servers, got %d", n.Count())
	}

	// Drain notifications concurrently
	done := make(chan struct{}, 2)
	go func() { _, _ = cli1.Recv(); done <- struct{}{} }()
	go func() { _, _ = cli2.Recv(); done <- struct{}{} }()

	n.Broadcast("download.progress", &DownloadProgressNotification{
		GID:             "gid-123",
		CompletedLength: 512,
	})

	<-done
	<-done

	// Both should still be registered
	if n.Count() != 2 {
		t.Fatalf("expected 2 servers after broadcast, got %d", n.Count())
	}
}

func TestRPCNotifier_Broadcast_PartialFailure(t *testing.T) {
	l := log.New(io.Discard, "", 0)
	n := NewRPCNotifier(l)

	// Server 1: stays connected
	cli1, srv1, cleanup1 := newTestServer(t)
	defer cleanup1()

	// Server 2: will be disconnected
	cli2, srv2, _ := newTestServer(t)

	n.Register(srv1)
	n.Register(srv2)

	// Disconnect server 2
	cli2.Close()
	_ = srv2.Wait()

	// Drain notification from server 1 concurrently
	done := make(chan struct{}, 1)
	go func() { _, _ = cli1.Recv(); done <- struct{}{} }()

	// Broadcast should succeed for srv1 and remove srv2
	n.Broadcast("download.complete", &DownloadCompleteNotification{
		GID:         "gid-123",
		TotalLength: 1024,
	})

	<-done

	if n.Count() != 1 {
		t.Fatalf("expected 1 server after partial failure, got %d", n.Count())
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
			cli, srv, _ := newTestServer(t)

			n.Register(srv)
			_ = n.Count()
			n.Unregister(srv)

			cli.Close()
			_ = srv.Wait()
		}()
	}
	wg.Wait()

	if n.Count() != 0 {
		t.Fatalf("expected 0 servers after concurrent register/unregister, got %d", n.Count())
	}
}

func TestRPCNotifier_Count(t *testing.T) {
	n := NewRPCNotifier(nil)

	if n.Count() != 0 {
		t.Fatalf("expected 0, got %d", n.Count())
	}

	servers := make([]*jrpc2.Server, 3)
	clients := make([]channel.Channel, 3)

	for i := 0; i < 3; i++ {
		cli, srv, _ := newTestServer(t)
		servers[i] = srv
		clients[i] = cli
		n.Register(srv)
	}

	if n.Count() != 3 {
		t.Fatalf("expected 3, got %d", n.Count())
	}

	// Unregister one
	n.Unregister(servers[1])
	if n.Count() != 2 {
		t.Fatalf("expected 2, got %d", n.Count())
	}

	// Cleanup
	for i := 0; i < 3; i++ {
		clients[i].Close()
		_ = servers[i].Wait()
	}
}

func TestRPCNotifier_DoubleRegister(t *testing.T) {
	n := NewRPCNotifier(nil)
	cli, srv, cleanup := newTestServer(t)
	defer cleanup()
	_ = cli

	// Registering the same server twice should be idempotent (map key)
	n.Register(srv)
	n.Register(srv)
	if n.Count() != 1 {
		t.Fatalf("expected 1 server after double register, got %d", n.Count())
	}
}

// TestNotificationTypes verifies the notification param types can be used
// with Broadcast without errors.
func TestNotificationTypes(t *testing.T) {
	n := NewRPCNotifier(nil)
	cli, srv, cleanup := newTestServer(t)
	defer cleanup()

	n.Register(srv)

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
				data, _ := cli.Recv()
				done <- data
			}()

			n.Broadcast(tt.method, tt.params)

			data := <-done
			if len(data) == 0 {
				t.Fatalf("expected notification data for %s, got empty", tt.method)
			}
		})
	}
}
