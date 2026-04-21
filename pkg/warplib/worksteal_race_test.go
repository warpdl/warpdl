package warplib

import (
	"io"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestWorkStealConcurrentStealerAndOwner verifies that a stealer can safely
// reduce `foff` while the owner is reading/writing it through the shared
// *atomic.Int64 cell. Run with -race to catch any lingering non-atomic
// access.
func TestWorkStealConcurrentStealerAndOwner(t *testing.T) {
	foff := new(atomic.Int64)
	foff.Store(1_000_000)
	var read int64
	info := &activePartInfo{
		hash:   "owner",
		offset: 0,
		foff:   foff,
		read:   &read,
	}

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Owner goroutine: simulates copy-loop reads and the CAS we now use
	// in runPart when the slow-path splits.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			observed := info.foff.Load()
			// Simulate progress: increment read, never go past foff.
			newRead := atomic.LoadInt64(info.read) + 1024
			if info.offset+newRead > observed {
				continue
			}
			atomic.StoreInt64(info.read, newRead)
		}
	}()

	// Stealer goroutine: reduce foff while owner runs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			info.mu.Lock()
			if !info.stolen.Load() {
				remaining := info.getRemaining()
				if remaining > WORK_STEAL_MIN_REMAINING {
					newFoff := info.offset + atomic.LoadInt64(info.read) + remaining/2
					info.foff.Store(newFoff)
					info.stolen.Store(true)
					info.mu.Unlock()
					// Reset stolen so we can steal again in next iteration.
					time.Sleep(10 * time.Microsecond)
					info.stolen.Store(false)
					continue
				}
			}
			info.mu.Unlock()
			time.Sleep(1 * time.Microsecond)
		}
	}()

	// Let them run for a short while.
	time.Sleep(30 * time.Millisecond)
	close(done)
	wg.Wait()

	// Invariant: read never exceeded foff.
	finalFoff := info.foff.Load()
	finalRead := atomic.LoadInt64(info.read)
	if info.offset+finalRead > finalFoff {
		t.Fatalf("owner exceeded stolen bound: read=%d offset=%d foff=%d",
			finalRead, info.offset, finalFoff)
	}
}

// TestWorkStealDoubleStealPrevented verifies `stolen` is honored: a second
// steal attempt against the same victim must fail.
func TestWorkStealDoubleStealPrevented(t *testing.T) {
	d := &Downloader{
		enableWorkStealing: true,
		maxConn:            10,
		maxParts:           10,
	}
	d.activeParts.Make()
	atomic.StoreInt32(&d.numConn, 1)
	atomic.StoreInt32(&d.numParts, 1)

	foff := new(atomic.Int64)
	foff.Store(int64(50 * MB))
	var read int64
	d.activeParts.Set("victim", &activePartInfo{
		hash:   "victim",
		offset: 0,
		foff:   foff,
		read:   &read,
	})

	// First steal should find the victim and claim it.
	victim := findBestVictimForStealing(&d.activeParts)
	if victim == nil {
		t.Fatal("first steal: victim should be found")
	}
	victim.mu.Lock()
	victim.stolen.Store(true)
	victim.mu.Unlock()

	// Second findBest should skip the stolen victim.
	victim2 := findBestVictimForStealing(&d.activeParts)
	if victim2 != nil {
		t.Fatalf("second steal: expected nil (stolen victim should be skipped), got %v", victim2)
	}
}

// TestWorkStealConcurrentClaims exercises the "find + claim" portion of
// attemptWorkSteal (without the goroutine spawn) to ensure many concurrent
// stealers all agree on who wins each victim. The claim logic lives in
// attemptWorkSteal, so we reproduce it here under a race-safe wrapper.
//
// Invariant: every victim can be claimed exactly once.
func TestWorkStealConcurrentClaims(t *testing.T) {
	var activeParts VMap[string, *activePartInfo]
	activeParts.Make()

	const numVictims = 8
	for i := 0; i < numVictims; i++ {
		foff := new(atomic.Int64)
		foff.Store(int64(100 * MB))
		read := new(int64)
		activeParts.Set(
			"v"+string(rune('A'+i)),
			&activePartInfo{
				hash:   "v" + string(rune('A'+i)),
				offset: 0,
				foff:   foff,
				read:   read,
			},
		)
	}

	// claim reproduces the victim.mu.Lock + stolen.CAS sequence from
	// attemptWorkSteal, without the side effects that require a live
	// Downloader.
	claim := func() bool {
		victim := findBestVictimForStealing(&activeParts)
		if victim == nil {
			return false
		}
		victim.mu.Lock()
		defer victim.mu.Unlock()
		if victim.stolen.Load() {
			return false
		}
		if victim.getRemaining() <= WORK_STEAL_MIN_REMAINING {
			return false
		}
		_, _, ok := calculateStealWork(
			victim.offset,
			victim.foff.Load(),
			atomic.LoadInt64(victim.read),
		)
		if !ok {
			return false
		}
		victim.foff.Store(victim.offset + atomic.LoadInt64(victim.read) + victim.getRemaining()/2)
		victim.stolen.Store(true)
		return true
	}

	var successes atomic.Int32
	var wg sync.WaitGroup
	stealerCount := 64
	for i := 0; i < stealerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if claim() {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()

	if successes.Load() > numVictims {
		t.Fatalf("too many successful steals: %d > %d victims",
			successes.Load(), numVictims)
	}

	stolenCount := 0
	activeParts.Range(func(_ string, info *activePartInfo) bool {
		if info.stolen.Load() {
			stolenCount++
		}
		return true
	})
	if stolenCount != int(successes.Load()) {
		t.Fatalf("stolen count mismatch: victims marked stolen=%d, steals=%d",
			stolenCount, successes.Load())
	}
}

// TestAttemptWorkStealHappyPath exercises the full attemptWorkSteal
// pipeline (including the spawned goroutine) using a Downloader with
// a stubbed newPartDownload path. We need to avoid actually running a
// download, so we set maxParts low and pre-saturate the counters to
// force the early-return "part limit reached" branch before the go
// routine fires.
func TestAttemptWorkStealRespectsPartLimit(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	d := &Downloader{
		enableWorkStealing: true,
		maxConn:            100,
		maxParts:           1, // deliberately tiny to block fresh spawns
		handlers:           &Handlers{},
		l:                  logger,
		wg:                 &sync.WaitGroup{},
	}
	d.activeParts.Make()
	d.handlers.setDefault(logger)
	atomic.StoreInt32(&d.numParts, 1) // at limit

	foff := new(atomic.Int64)
	foff.Store(int64(100 * MB))
	var read int64
	d.activeParts.Set("victim", &activePartInfo{
		hash:   "victim",
		offset: 0,
		foff:   foff,
		read:   &read,
	})

	if d.attemptWorkSteal("stealer", WORK_STEAL_SPEED_THRESHOLD*2) {
		t.Fatal("expected steal to be rejected when numParts == maxParts")
	}

	// Victim must not have been marked stolen.
	info, _ := func() (*activePartInfo, bool) {
		var v *activePartInfo
		d.activeParts.Range(func(_ string, i *activePartInfo) bool {
			v = i
			return false
		})
		return v, v != nil
	}()
	if info.stolen.Load() {
		t.Fatal("victim incorrectly marked stolen after rejected steal")
	}
}

// TestFoffAtomicSharedAcrossGoroutines sanity-checks that the atomic.Int64
// the stealer modifies is indeed the same cell the owner reads from.
func TestFoffAtomicSharedAcrossGoroutines(t *testing.T) {
	foff := new(atomic.Int64)
	foff.Store(1000)

	// Simulate registerActivePart storing the pointer, then a stealer
	// modifying it via that pointer.
	info := &activePartInfo{foff: foff}
	info.foff.Store(500)

	if foff.Load() != 500 {
		t.Fatalf("shared atomic divergence: original=%d via-info=%d",
			foff.Load(), info.foff.Load())
	}
}
