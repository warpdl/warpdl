package warplib

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestWorkStealStress_RegisterUnregisterSteal drives a very chaotic mix:
// many goroutines register parts, unregister them, and try to steal
// concurrently. Under -race, any lingering non-atomic access to the
// shared foff pointer or stolen flag will be surfaced.
//
// Invariants:
//   - no panic
//   - stolen count <= registrations (a part cannot be stolen from twice)
//   - no goroutine leak
func TestWorkStealStress_RegisterUnregisterSteal(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var activeParts VMap[string, *activePartInfo]
		activeParts.Make()

		var registered atomic.Int64
		var stolen atomic.Int64

		const (
			registrars = 16
			iters      = 500
			stealers   = 16
		)

		var wg sync.WaitGroup
		stop := make(chan struct{})

		// Registrars - register and eventually unregister parts.
		for w := 0; w < registrars; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < iters; i++ {
					hash := rand.IntN(100_000) // wide key space
					hashStr := intToKey(hash)
					foff := new(atomic.Int64)
					foff.Store(int64(50 * MB))
					var read int64
					activeParts.Set(hashStr, &activePartInfo{
						hash:   hashStr,
						offset: 0,
						foff:   foff,
						read:   &read,
					})
					registered.Add(1)
					// Most parts get unregistered soon after.
					if rand.IntN(2) == 0 {
						activeParts.Delete(hashStr)
					}
				}
			}(w)
		}

		// Stealers - constantly try to find victims.
		for w := 0; w < stealers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					victim := findBestVictimForStealing(&activeParts)
					if victim == nil {
						continue
					}
					victim.mu.Lock()
					if victim.stolen.Load() {
						victim.mu.Unlock()
						continue
					}
					if victim.getRemaining() > WORK_STEAL_MIN_REMAINING {
						victim.stolen.Store(true)
						newFoff := victim.offset + atomic.LoadInt64(victim.read) +
							victim.getRemaining()/2
						victim.foff.Store(newFoff)
						stolen.Add(1)
					}
					victim.mu.Unlock()
				}
			}()
		}

		// Let them fight for a bounded amount of time then tell stealers
		// to exit and wait for registrars to finish.
		go func() {
			// Registrars' work is bounded; wait for them to finish then stop stealers.
			// But we can't wait here from a goroutine that's not in wg - use a counter.
		}()
		time.Sleep(200 * time.Millisecond)
		close(stop)
		wg.Wait()

		if stolen.Load() > registered.Load() {
			t.Fatalf("stolen=%d > registered=%d", stolen.Load(), registered.Load())
		}
	})
}

// TestWorkStealStress_StealAfterUnregister exercises the race where a
// stealer's Range snapshot includes a part that gets unregistered
// between snapshot and claim. The stealer must not panic and the steal
// either succeeds (pointing at still-valid heap memory) or is skipped.
func TestWorkStealStress_StealAfterUnregister(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var activeParts VMap[string, *activePartInfo]
		activeParts.Make()

		// Seed with many victims.
		for i := 0; i < 100; i++ {
			foff := new(atomic.Int64)
			foff.Store(int64(100 * MB))
			var read int64
			hashStr := intToKey(i)
			activeParts.Set(hashStr, &activePartInfo{
				hash:   hashStr,
				offset: 0,
				foff:   foff,
				read:   &read,
			})
		}

		var wg sync.WaitGroup

		// Unregister the victims while stealers are picking them.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				activeParts.Delete(intToKey(i))
				runtimeGoschedShort()
			}
		}()

		// Stealers try to claim concurrently.
		var panics atomic.Int32
		for w := 0; w < 16; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						panics.Add(1)
					}
				}()
				for i := 0; i < 100; i++ {
					victim := findBestVictimForStealing(&activeParts)
					if victim == nil {
						continue
					}
					victim.mu.Lock()
					_ = victim.stolen.Load()
					_ = victim.foff.Load()
					_ = victim.getRemaining()
					victim.mu.Unlock()
				}
			}()
		}

		wg.Wait()
		if panics.Load() != 0 {
			t.Fatalf("stealer panicked %d times", panics.Load())
		}
	})
}

// TestWorkStealStress_OwnerStealerInterleave models the real concurrency
// between a part's owning goroutine (progressing reads and occasionally
// shrinking foff via respawn CAS) and multiple stealers. Invariant: the
// part's read position never exceeds foff.
func TestWorkStealStress_OwnerStealerInterleave(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		foff := new(atomic.Int64)
		foff.Store(int64(10 * MB))
		var read int64
		info := &activePartInfo{
			hash:   "owner",
			offset: 0,
			foff:   foff,
			read:   &read,
		}

		var violated atomic.Int32
		var wg sync.WaitGroup
		stop := make(chan struct{})

		// Owner: advance read up to foff without ever exceeding it.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				currentFoff := info.foff.Load()
				newRead := atomic.LoadInt64(info.read) + 1024
				if info.offset+newRead > currentFoff {
					continue
				}
				if !atomic.CompareAndSwapInt64(info.read,
					atomic.LoadInt64(info.read), newRead) {
					continue
				}
				// Cross-check invariant after the store.
				if info.offset+atomic.LoadInt64(info.read) > info.foff.Load() {
					violated.Add(1)
				}
			}
		}()

		// Owner CAS-based respawn: sometimes shrink foff.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				observed := info.foff.Load()
				poff := info.offset + atomic.LoadInt64(info.read)
				if observed-poff < 2*MB {
					runtimeGoschedShort()
					continue
				}
				newFoff := poff + (observed-poff)/2
				// Only shrink.
				for {
					cur := info.foff.Load()
					if newFoff >= cur {
						break
					}
					if info.foff.CompareAndSwap(cur, newFoff) {
						break
					}
				}
				runtimeGoschedShort()
			}
		}()

		// Stealers: shrink foff via atomic store (not CAS) to simulate
		// the attemptWorkSteal path.
		for s := 0; s < 4; s++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					info.mu.Lock()
					if !info.stolen.Load() && info.getRemaining() > WORK_STEAL_MIN_REMAINING {
						stealStart := info.offset +
							atomic.LoadInt64(info.read) + info.getRemaining()/2
						info.foff.Store(stealStart - 1)
						info.stolen.Store(true)
					} else if info.stolen.Load() && rand.IntN(10) == 0 {
						// Reset stolen to allow another round of stealing.
						info.stolen.Store(false)
					}
					info.mu.Unlock()
					runtimeGoschedShort()
				}
			}()
		}

		time.Sleep(100 * time.Millisecond)
		close(stop)
		wg.Wait()

		if violated.Load() != 0 {
			t.Fatalf("owner exceeded foff %d times", violated.Load())
		}
	})
}

// intToKey converts an int to a string key so we can reuse VMap[string, ...].
func intToKey(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}

// runtimeGoschedShort yields to the scheduler briefly so the test runs
// with realistic interleaving rather than monopolizing the CPU.
func runtimeGoschedShort() {
	// time.Sleep(0) is equivalent to runtime.Gosched() here and doesn't
	// require importing runtime in this file.
	time.Sleep(0)
}
