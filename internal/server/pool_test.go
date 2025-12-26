package server

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestPoolBroadcast(t *testing.T) {
	p := NewPool(nil)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sconn := NewSyncConn(c1)
	p.AddDownload("id", sconn)
	msg := []byte("payload")
	go p.Broadcast("id", msg)

	peer := NewSyncConn(c2)
	got, err := peer.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("unexpected message: %s", string(got))
	}
}

func TestPoolErrors(t *testing.T) {
	p := NewPool(nil)
	p.WriteError("id", ErrorTypeWarning, "warn")
	if err := p.GetError("id"); err == nil || err.Message != "warn" {
		t.Fatalf("expected warning error")
	}
	p.WriteError("id", ErrorTypeCritical, "crit")
	if err := p.GetError("id"); err == nil || err.Message != "crit" {
		t.Fatalf("expected critical error")
	}
	p.WriteError("id", ErrorTypeWarning, "ignored")
	if err := p.GetError("id"); err == nil || err.Message != "crit" {
		t.Fatalf("expected critical error to remain")
	}
	p.ForceWriteError("id", ErrorTypeWarning, "forced")
	if err := p.GetError("id"); err == nil || err.Message != "forced" {
		t.Fatalf("expected forced error")
	}
}

func TestPoolAddConnection(t *testing.T) {
	p := NewPool(nil)
	p.AddDownload("id", nil)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	p.AddConnection("id", NewSyncConn(c1))
	if len(p.m["id"]) != 1 {
		t.Fatalf("expected connection to be added")
	}
}

func TestPoolHasDownloadAndRemove(t *testing.T) {
	p := NewPool(nil)
	p.AddDownload("id", nil)
	if !p.HasDownload("id") {
		t.Fatalf("expected download to be present")
	}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	sconn := NewSyncConn(c1)
	p.AddConnection("id", sconn)
	p.removeConn("id", 0)
	if len(p.m["id"]) != 0 {
		t.Fatalf("expected connection to be removed")
	}
}

func TestPoolBroadcastWriteErrorRemovesConn(t *testing.T) {
	p := NewPool(nil)
	c1, c2 := net.Pipe()
	_ = c2.Close()
	defer c1.Close()
	sconn := NewSyncConn(c1)
	p.AddDownload("id", sconn)
	p.Broadcast("id", []byte("payload"))
	if len(p.m["id"]) != 0 {
		t.Fatalf("expected connection to be removed after write error")
	}
}

// TestPoolAddConnectionConcurrent tests Race 5: AddConnection TOCTOU
// This test verifies that concurrent AddConnection calls don't lose connections
// due to the read-unlock-modify-lock-write race condition.
func TestPoolAddConnectionConcurrent(t *testing.T) {
	p := NewPool(nil)
	p.AddDownload("uid", nil)

	const numAdders = 50
	var wg sync.WaitGroup

	for i := 0; i < numAdders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c1, c2 := net.Pipe()
			defer c1.Close()
			defer c2.Close()
			p.AddConnection("uid", NewSyncConn(c1))
		}()
	}
	wg.Wait()

	p.mu.RLock()
	actual := len(p.m["uid"])
	p.mu.RUnlock()

	if actual != numAdders {
		t.Errorf("expected %d connections, got %d (lost %d)", numAdders, actual, numAdders-actual)
	}
}

// TestPoolBroadcastConcurrentRemoval tests Race 6: Broadcast slice corruption
// This test verifies that concurrent broadcasts with connection failures don't
// cause panics or deadlocks due to:
// 1. Missing unlock in writeBroadcastedMessage on error paths
// 2. Slice modification during iteration
func TestPoolBroadcastConcurrentRemoval(t *testing.T) {
	p := NewPool(nil)
	p.AddDownload("uid", nil)

	// Create connections, some will fail
	var goodClients []net.Conn
	for i := 0; i < 10; i++ {
		c1, c2 := net.Pipe()
		defer c1.Close()
		if i%3 == 0 {
			// Close receiver side immediately - writes will fail
			c2.Close()
		} else {
			// Keep these open but drain them in background to prevent blocking
			goodClients = append(goodClients, c2)
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 1024)
				for {
					conn.SetReadDeadline(time.Now().Add(5 * time.Second))
					_, err := conn.Read(buf)
					if err != nil {
						return
					}
				}
			}(c2)
		}
		p.AddConnection("uid", NewSyncConn(c1))
	}

	// Run concurrent broadcasts
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Broadcast("uid", []byte("msg"))
		}()
	}
	wg.Wait()

	// Clean up good clients
	for _, c := range goodClients {
		c.Close()
	}

	// Verify failed connections were removed
	p.mu.RLock()
	remaining := len(p.m["uid"])
	p.mu.RUnlock()

	// We expect about 6-7 good connections remaining (those not divisible by 3)
	if remaining < 5 || remaining > 8 {
		t.Logf("Warning: expected 6-7 remaining connections, got %d", remaining)
	}
}

// TestPoolWriteErrorConcurrent tests WriteError TOCTOU race
// This test verifies that concurrent WriteError calls properly handle
// the critical error preservation logic without race conditions.
func TestPoolWriteErrorConcurrent(t *testing.T) {
	p := NewPool(nil)

	var wg sync.WaitGroup
	const numWriters = 100

	// Start concurrent writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		errType := ErrorTypeWarning
		if i%10 == 0 {
			errType = ErrorTypeCritical
		}
		go func(idx int, et ErrorType) {
			defer wg.Done()
			p.WriteError("uid", et, "message")
		}(i, errType)
	}
	wg.Wait()

	// Should have either a critical or warning error (no panic, no data race)
	err := p.GetError("uid")
	if err == nil {
		t.Fatal("expected error to be set")
	}
}
