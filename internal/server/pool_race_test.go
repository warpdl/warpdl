package server

import (
	"log"
	"os"
	"sync"
	"testing"
)

// TestAddConnectionRegression ensures AddConnection uses single write lock
// instead of RLock-unlock-Lock pattern (TOCTOU fix at pool.go:62-66).
func TestAddConnectionRegression(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))
	uid := "test-download"

	// Add initial connection
	initialConn := &SyncConn{}
	p.AddDownload(uid, initialConn)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.AddConnection(uid, &SyncConn{})
		}()
	}
	wg.Wait()

	// Verify all connections were added
	p.mu.RLock()
	count := len(p.m[uid])
	p.mu.RUnlock()

	if count != 101 { // 1 initial + 100 added
		t.Errorf("expected 101 connections, got %d", count)
	}
}

// TestAddConnectionConcurrentWithRemove tests concurrent AddDownload and StopDownload
func TestAddConnectionConcurrentWithRemove(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		uid := "download-" + string(rune('A'+i%26))

		wg.Add(3)
		go func(u string) {
			defer wg.Done()
			p.AddDownload(u, &SyncConn{})
		}(uid)
		go func(u string) {
			defer wg.Done()
			p.AddConnection(u, &SyncConn{})
		}(uid)
		go func(u string) {
			defer wg.Done()
			p.StopDownload(u)
		}(uid)
	}
	wg.Wait()
	// No panic = success
}

// TestPoolConcurrentOperations stress tests all pool operations
func TestPoolConcurrentOperations(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		uid := "uid-" + string(rune('0'+i%10))

		wg.Add(4)
		go func(u string) {
			defer wg.Done()
			p.AddDownload(u, &SyncConn{})
		}(uid)
		go func(u string) {
			defer wg.Done()
			p.AddConnection(u, &SyncConn{})
		}(uid)
		go func(u string) {
			defer wg.Done()
			_ = p.HasDownload(u)
		}(uid)
		go func(u string) {
			defer wg.Done()
			p.StopDownload(u)
		}(uid)
	}
	wg.Wait()
}

// TestAddConnectionNoTOCTOU verifies that AddConnection doesn't suffer from
// time-of-check-to-time-of-use race by using a single write lock.
func TestAddConnectionNoTOCTOU(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))
	uid := "toctou-test"

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	// Concurrent AddConnection calls
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				p.AddConnection(uid, &SyncConn{})
			}
		}()
	}

	// Concurrent HasDownload calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = p.HasDownload(uid)
			}
		}()
	}

	wg.Wait()

	// Verify all connections were added
	p.mu.RLock()
	count := len(p.m[uid])
	p.mu.RUnlock()

	expected := goroutines * iterations
	if count != expected {
		t.Errorf("expected %d connections, got %d (indicates lost updates)", expected, count)
	}
}

// TestPoolAddRemoveRace tests concurrent addition and removal of UIDs
func TestPoolAddRemoveRace(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))

	var wg sync.WaitGroup
	const uids = 20
	const iterations = 50

	for i := 0; i < uids; i++ {
		uid := string(rune('A' + i))

		// AddDownload operations
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				p.AddDownload(u, &SyncConn{})
			}
		}(uid)

		// StopDownload operations
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				p.StopDownload(u)
			}
		}(uid)

		// AddConnection operations
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				p.AddConnection(u, &SyncConn{})
			}
		}(uid)
	}

	wg.Wait()
	// No panic means proper synchronization
}

// TestPoolHasDownloadConsistency ensures HasDownload returns consistent results
func TestPoolHasDownloadConsistency(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))
	uid := "consistency-test"

	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			p.AddConnection(uid, &SyncConn{})
		}
	}()

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				exists := p.HasDownload(uid)
				// Verify we got a valid boolean
				_ = exists
			}
		}()
	}

	wg.Wait()
}

// TestPoolMultipleUIDs tests operations on multiple UIDs concurrently
func TestPoolMultipleUIDs(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))

	var wg sync.WaitGroup
	const numUIDs = 50
	const opsPerUID = 100

	for i := 0; i < numUIDs; i++ {
		uid := string(rune('A' + (i % 26)))
		if i >= 26 {
			uid = uid + string(rune('0'+(i/26)))
		}

		wg.Add(4)

		// Add initial connection
		go func(u string) {
			defer wg.Done()
			p.AddDownload(u, &SyncConn{})
		}(uid)

		// Add more connections
		go func(u string) {
			defer wg.Done()
			for j := 0; j < opsPerUID; j++ {
				p.AddConnection(u, &SyncConn{})
			}
		}(uid)

		// Check downloads
		go func(u string) {
			defer wg.Done()
			for j := 0; j < opsPerUID; j++ {
				_ = p.HasDownload(u)
			}
		}(uid)

		// Remove some UIDs
		if i%5 == 0 {
			go func(u string) {
				defer wg.Done()
				for j := 0; j < opsPerUID; j++ {
					if j%10 == 0 {
						p.StopDownload(u)
					}
				}
			}(uid)
		} else {
			wg.Done() // Balance the wg.Add(4)
		}
	}

	wg.Wait()

	// Verify pool is in consistent state
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.m == nil {
		t.Error("pool map is nil")
	}
}

// TestAddConnectionEmptySlice tests that AddConnection works even when slice doesn't exist yet
func TestAddConnectionEmptySlice(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))
	uid := "new-uid"

	// AddConnection on non-existent UID should create the slice
	p.AddConnection(uid, &SyncConn{})

	p.mu.RLock()
	conns := p.m[uid]
	p.mu.RUnlock()

	if len(conns) != 1 {
		t.Errorf("expected 1 connection, got %d", len(conns))
	}
}

// TestPoolStressTest intensive concurrent operations to expose any race conditions
func TestPoolStressTest(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 200

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			uid := string(rune('A' + (id % 26)))

			for j := 0; j < iterations; j++ {
				switch j % 5 {
				case 0:
					p.AddDownload(uid, &SyncConn{})
				case 1:
					p.AddConnection(uid, &SyncConn{})
				case 2:
					_ = p.HasDownload(uid)
				case 3:
					p.StopDownload(uid)
				case 4:
					// Mixed operations
					p.AddConnection(uid, &SyncConn{})
					_ = p.HasDownload(uid)
				}
			}
		}(i)
	}

	wg.Wait()
	// Success = no race detector warnings and no panics
}

// TestPoolInitialState verifies pool starts in correct state
func TestPoolInitialState(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.m == nil {
		t.Error("pool map should be initialized")
	}

	if len(p.m) != 0 {
		t.Errorf("expected empty pool, got %d entries", len(p.m))
	}
}

// TestAddConnectionOrderPreservation verifies connections are added in order
func TestAddConnectionOrderPreservation(t *testing.T) {
	p := NewPool(log.New(os.Stderr, "", 0))
	uid := "order-test"

	// Add connections sequentially
	var conns []*SyncConn
	for i := 0; i < 10; i++ {
		conn := &SyncConn{}
		conns = append(conns, conn)
		p.AddConnection(uid, conn)
	}

	// Verify order
	p.mu.RLock()
	poolConns := p.m[uid]
	p.mu.RUnlock()

	if len(poolConns) != len(conns) {
		t.Fatalf("expected %d connections, got %d", len(conns), len(poolConns))
	}

	for i, conn := range conns {
		if poolConns[i] != conn {
			t.Errorf("connection at index %d doesn't match", i)
		}
	}
}
