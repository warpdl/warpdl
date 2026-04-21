package warplib

import (
	"fmt"
	"sync"
	"time"
)

// DefaultPersistInterval is the debounce window the Manager uses between
// background persistence writes. It is a package-level variable so tests
// can lower it without blocking the real download path.
var DefaultPersistInterval = 500 * time.Millisecond

// persister coalesces high-frequency Manager.UpdateItem calls into a
// bounded number of disk writes.
//
// Previously, every DownloadProgressHandler call (once per 32 KB chunk)
// triggered a full GOB re-encode + file truncate + write of the entire
// userdata file. For a 1 GB download this produced hundreds of thousands
// of synchronous rewrites, dwarfing actual network I/O.
//
// With the persister, callers set a "dirty" flag non-blockingly; a single
// writer goroutine flushes at most once per debounce interval. Terminal
// events (Flush, FlushOne, Close) can bypass debouncing via flushNow.
//
// Concurrency model:
//   - dirty is guarded by mu.
//   - wake is a 1-buffered channel used as a non-blocking signal.
//   - stop is closed once in stop() to terminate the writer goroutine.
//   - flushNow is a request/ack pair used for synchronous flushes.
type persister struct {
	mu       sync.Mutex
	dirty    bool
	wake     chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{} // closed when writer exits
	writeFn  func() error  // actually persists state - called under writerLock
	logFn    func(string, ...any)

	interval time.Duration

	// flushNow serializes synchronous flush requests from Close/Flush
	// callers with the writer goroutine.
	flushReq chan chan error
}

// newPersister starts a background writer that calls writeFn whenever the
// dirty flag is set, rate-limited to `interval`. writeFn must take any
// locks it needs internally.
func newPersister(writeFn func() error, interval time.Duration, logFn func(string, ...any)) *persister {
	if interval <= 0 {
		interval = DefaultPersistInterval
	}
	p := &persister{
		wake:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		writeFn:  writeFn,
		logFn:    logFn,
		interval: interval,
		flushReq: make(chan chan error),
	}
	go p.loop()
	return p
}

// markDirty signals that state has changed and needs persistence. Cheap and
// non-blocking - safe to call at high frequency from the progress handler.
func (p *persister) markDirty() {
	p.mu.Lock()
	p.dirty = true
	p.mu.Unlock()
	// Non-blocking wake. The writer will pick up the dirty flag on its
	// next tick - this just shortens latency for the first write after
	// idle.
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// flush waits for any pending dirty state to be persisted and returns
// the error (if any) from the flush.
func (p *persister) flush() error {
	ack := make(chan error, 1)
	select {
	case p.flushReq <- ack:
	case <-p.stop:
		// Persister is shut down; no writer to serve the request.
		return nil
	}
	select {
	case err := <-ack:
		return err
	case <-p.stop:
		return nil
	}
}

// shutdown stops the writer goroutine after performing one final flush.
// Safe to call multiple times.
func (p *persister) shutdown() error {
	var finalErr error
	p.stopOnce.Do(func() {
		// Request a synchronous flush first so callers that rely on
		// Close() to persist state still see their data on disk.
		finalErr = p.flush()
		close(p.stop)
	})
	<-p.done
	return finalErr
}

func (p *persister) loop() {
	defer close(p.done)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stop:
			return
		case ack := <-p.flushReq:
			ack <- p.drainAndWrite()
		case <-p.wake:
			// A writer signaled new dirt. Sleep until the next tick
			// to coalesce a burst of markDirty calls.
			select {
			case <-p.stop:
				return
			case <-ticker.C:
			case ack := <-p.flushReq:
				ack <- p.drainAndWrite()
				continue
			}
			if err := p.drainAndWrite(); err != nil {
				p.log("persist: %v", err)
			}
		case <-ticker.C:
			if err := p.drainAndWrite(); err != nil {
				p.log("persist: %v", err)
			}
		}
	}
}

// drainAndWrite clears the dirty flag and calls the writeFn. If the flag
// was not set it is a no-op and returns nil.
//
// writeFn is supplied by external callers (e.g. Manager) and any panic
// it throws would otherwise crash the background writer goroutine and
// take the daemon down with it. We catch panics here and surface them
// as an error so the writer survives and subsequent flushes keep
// working.
func (p *persister) drainAndWrite() (err error) {
	p.mu.Lock()
	if !p.dirty {
		p.mu.Unlock()
		return nil
	}
	p.dirty = false
	p.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("persist: writeFn panic: %v", r)
		}
	}()
	return p.writeFn()
}

func (p *persister) log(format string, args ...any) {
	if p.logFn != nil {
		p.logFn(format, args...)
	}
}
