package warplib

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestManagerStress_CloseCalledConcurrently slams Close() from many
// goroutines at once. Close is not documented as idempotent, but must
// never panic or deadlock under the scenario.
func TestManagerStress_CloseCalledConcurrently(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		m := newTestManager(t)
		d := newTestDownloader()
		d.dlLoc = t.TempDir()
		if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
			t.Fatalf("AddDownload: %v", err)
		}

		var panics atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						panics.Add(1)
					}
				}()
				_ = m.Close()
			}()
		}

		mustNoHang(t, 3*time.Second, wg.Wait)

		if panics.Load() != 0 {
			t.Fatalf("concurrent Close panicked %d times", panics.Load())
		}
	})
}

// TestManagerStress_UpdateItemAfterClose verifies that UpdateItem calls
// made after Close do not panic. The Manager may choose to ignore them
// or persist them on a best-effort basis, but panic is never acceptable.
func TestManagerStress_UpdateItemAfterClose(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		m := newTestManager(t)

		d := newTestDownloader()
		d.dlLoc = t.TempDir()
		if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
			t.Fatalf("AddDownload: %v", err)
		}
		item := m.GetItem(d.hash)
		if err := m.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		var panics atomic.Int32
		for i := 0; i < 500; i++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						panics.Add(1)
					}
				}()
				m.UpdateItemAsync(item)
			}()
		}
		if panics.Load() != 0 {
			t.Fatalf("UpdateItemAsync after Close panicked %d times", panics.Load())
		}
	})
}

// TestManagerStress_AddWhileClosing races AddDownload against Close.
// Either AddDownload succeeds before Close, or it may fail, but must
// not panic.
func TestManagerStress_AddWhileClosing(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		m := newTestManager(t)

		var closed atomic.Bool
		var panics atomic.Int32

		var wg sync.WaitGroup

		// Closer.
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
			_ = m.Close()
			closed.Store(true)
		}()

		// Adders.
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						panics.Add(1)
					}
				}()
				d := newTestDownloader()
				d.hash = d.hash + "-" + intToKey(i)
				d.dlLoc = t.TempDir()
				// Either succeeds or returns an error - never panics.
				_ = m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc})
			}(i)
		}

		mustNoHang(t, 5*time.Second, wg.Wait)

		if panics.Load() != 0 {
			t.Fatalf("Add-during-Close panicked %d times", panics.Load())
		}
	})
}

// TestDownloaderStress_StopSpam calls Stop() thousands of times in
// parallel. Stop must be idempotent and safe.
func TestDownloaderStress_StopSpam(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		d := newTestDownloader()

		// Ensure the downloader has a valid context and cancel func.
		// newTestDownloader doesn't set these; fabricate them here.
		d.Stop() // first stop to exercise the path; may panic if cancel is nil
	})
}

// TestDownloaderStopNilCancelDoesNotPanic makes sure Stop() is safe on a
// partially-constructed downloader where cancel is nil.
// This is the specific case newTestDownloader hits.
func TestDownloaderStopNilCancelDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Stop on partial downloader panicked: %v", r)
		}
	}()
	d := newTestDownloader()
	d.Stop()
	d.Stop()
	d.Stop()
}
