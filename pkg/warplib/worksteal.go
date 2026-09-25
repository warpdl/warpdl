package warplib

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Work stealing constants define thresholds for dynamic part merging.
const (
	// WORK_STEAL_SPEED_THRESHOLD seeds the expected speed of a part created
	// by work stealing; a child slower than half of it becomes a candidate
	// for a slow-part split.
	WORK_STEAL_SPEED_THRESHOLD = 10 * MB

	// WORK_STEAL_MIN_REMAINING is the minimum remaining bytes in a part for
	// it to be eligible for work stealing. This prevents excessive overhead
	// from stealing very small work amounts while keeping the tail short on
	// slow per-connection links, where 2MB is still seconds of transfer.
	WORK_STEAL_MIN_REMAINING = 2 * MB // >2MB
)

// bytesPerSecond is bytesRead / duration in bytes per second.
// The integer form bytes*Second/duration overflows past about 8.6 GiB and
// wraps negative, which makes a fast multi-gigabyte part look too slow to
// steal. float64 is exact for byte counts up to 2^53 (about 9 PiB).
func bytesPerSecond(bytesRead int64, duration time.Duration) int64 {
	if bytesRead <= 0 || duration <= 0 {
		return 0
	}
	secs := duration.Seconds()
	if secs <= 0 {
		return math.MaxInt64
	}
	speed := float64(bytesRead) / secs
	if speed >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(speed)
}

// calculateStealWork computes the byte range to steal from an adjacent part.
// Uses a 50/50 split strategy: steals the second half of remaining bytes.
//
// Parameters:
//   - adjStart: adjacent part's starting offset
//   - adjEnd: adjacent part's ending offset (inclusive)
//   - adjBytesRead: bytes already downloaded by adjacent part
//
// Returns:
//   - stealStart: starting offset of stolen range
//   - stealEnd: ending offset of stolen range (inclusive)
//   - canSteal: true if there's enough remaining work to steal
func calculateStealWork(adjStart, adjEnd, adjBytesRead int64) (stealStart, stealEnd int64, canSteal bool) {
	// Handle negative bytesRead (corruption) - treat as zero
	if adjBytesRead < 0 {
		adjBytesRead = 0
	}

	// Calculate current position and remaining bytes
	currentPos := adjStart + adjBytesRead
	totalSize := adjEnd - adjStart + 1

	// Check for corruption: bytesRead exceeds total
	if adjBytesRead > totalSize {
		return 0, 0, false
	}

	// Calculate remaining bytes
	remaining := adjEnd - currentPos + 1

	// Must have more than minimum remaining to steal
	if remaining <= WORK_STEAL_MIN_REMAINING {
		return 0, 0, false
	}

	// Split remaining bytes 50/50 - steal second half
	// stealStart = currentPos + (remaining / 2)
	halfRemaining := remaining / 2
	stealStart = currentPos + halfRemaining
	stealEnd = adjEnd

	return stealStart, stealEnd, true
}

// activePartInfo tracks runtime state of an active downloading part for work stealing.
// All fields accessed across goroutines are either guarded by mu or stored in
// atomic-capable types.
type activePartInfo struct {
	hash            string        // Unique identifier for the part
	offset          int64         // Initial byte offset (starting position, never mutated)
	foff            *atomic.Int64 // Final offset - reduced atomically by stealer
	read            *int64        // Pointer to part's atomic read counter
	reservedThrough *atomic.Int64 // Inclusive end of an in-flight owner read
	mu              sync.Mutex    // Serializes reservations and boundary reductions
	stolen          atomic.Bool   // True if work was already stolen from this part
}

// getRemaining calculates the remaining bytes to download for this part.
// Returns the number of bytes left to download.
func (a *activePartInfo) getRemaining() int64 {
	currentPos := a.getSafeCurrentPos()
	foff := a.foff.Load()
	remaining := foff - currentPos + 1
	if remaining < 0 {
		return 0
	}
	return remaining
}

// getCurrentPos returns the current byte position (offset + bytes read).
func (a *activePartInfo) getCurrentPos() int64 {
	return a.offset + atomic.LoadInt64(a.read)
}

// getSafeCurrentPos returns the first byte that is neither completed nor
// reserved by the victim's in-flight read.
func (a *activePartInfo) getSafeCurrentPos() int64 {
	currentPos := a.getCurrentPos()
	if a.reservedThrough != nil {
		if reservedNext := a.reservedThrough.Load() + 1; reservedNext > currentPos {
			currentPos = reservedNext
		}
	}
	return currentPos
}

// findBestVictimForStealing finds the best part to steal work from.
// Returns the part with the most remaining bytes above the threshold,
// or nil if no suitable victim is found.
//
// Selection criteria:
//   - Part must have > WORK_STEAL_MIN_REMAINING bytes remaining
//   - Part must not already have been stolen from
//   - Part with most remaining bytes wins
func findBestVictimForStealing(activeParts *VMap[string, *activePartInfo]) *activePartInfo {
	var best *activePartInfo
	var bestRemaining int64 = 0

	activeParts.Range(func(hash string, info *activePartInfo) bool {
		// Skip if already had work stolen. stolen is atomic.Bool so this
		// is a safe concurrent read.
		if info.stolen.Load() {
			return true // continue iteration
		}

		remaining := info.getRemaining()

		// Only consider if remaining exceeds threshold
		if remaining <= WORK_STEAL_MIN_REMAINING {
			return true // continue iteration
		}

		// Track the part with most remaining work
		if remaining > bestRemaining {
			best = info
			bestRemaining = remaining
		}

		return true // continue iteration
	})

	return best
}

// registerActivePart registers a part for potential work stealing.
// Called when a part starts downloading. foff must already be heap-allocated
// (so the pointer is valid across goroutines).
func (d *Downloader) registerActivePart(part *Part, foff *atomic.Int64) {
	if !d.enableWorkStealing {
		return
	}
	reservedThrough := new(atomic.Int64)
	reservedThrough.Store(part.offset - 1)
	info := &activePartInfo{
		hash:            part.hash,
		offset:          part.offset,
		foff:            foff,
		read:            &part.read,
		reservedThrough: reservedThrough,
	}
	// The owner briefly takes this mutex to publish the inclusive end of its
	// next read. A stealer takes the same lock and starts strictly after that
	// reservation, so the mutex is never held while network I/O may stall.
	part.boundaryMu = &info.mu
	part.reservedThrough = reservedThrough
	d.activeParts.Set(part.hash, info)
}

// unregisterActivePart removes a completed part from the work stealing pool.
// Called when a part finishes downloading.
func (d *Downloader) unregisterActivePart(hash string) {
	if !d.enableWorkStealing {
		return
	}
	d.activeParts.Delete(hash)
}

// attemptWorkSteal hands half of the largest remaining range to a new part
// when stealerHash completes. Returns true if work stealing was initiated.
//
// The completed part's speed is deliberately not consulted: the stolen range
// is fetched over a fresh connection whose throughput is unrelated to the
// finished one, and leaving the freed connection idle only lengthens the tail
// of the download.
func (d *Downloader) attemptWorkSteal(stealerHash string) bool {
	if !d.enableWorkStealing {
		return false
	}

	// The stealer is still counted in numConn until its worker returns, and
	// its connection is exactly the one being handed to the child. Only a
	// count above the limit means no connection is actually free.
	if d.maxConn != 0 && atomic.LoadInt32(&d.numConn) > d.maxConn {
		d.Log("%s: work steal skipped - connection limit reached", stealerHash)
		return false
	}

	// Check part limit (use atomic load for thread safety)
	if d.maxParts != 0 && atomic.LoadInt32(&d.numParts) >= d.maxParts {
		d.Log("%s: work steal skipped - part limit reached", stealerHash)
		return false
	}

	// Find best victim part
	victim := findBestVictimForStealing(&d.activeParts)
	if victim == nil {
		return false
	}

	// Lock victim to compute an atomic steal range and serialize the
	// persistence that follows. Releasing before RespawnPartHandler lets a
	// concurrent slow split shorten the same parent further and persist its
	// smaller boundary first; the stale callback then restores the larger
	// boundary and overlaps the stolen child on restart. The handler only
	// takes item.mu + manager persist (no run drain), so holding victim.mu
	// across it cannot deadlock the copy loop: the loop needs the same mutex
	// per 32KB chunk and simply waits one persist.
	victim.mu.Lock()
	defer victim.mu.Unlock()
	if victim.stolen.Load() {
		return false
	}
	remaining := victim.getRemaining()
	if remaining <= WORK_STEAL_MIN_REMAINING {
		return false
	}
	safeCurrentPos := victim.getSafeCurrentPos()
	stealStart, stealEnd, canSteal := calculateStealWork(
		victim.offset,
		victim.foff.Load(),
		safeCurrentPos-victim.offset,
	)
	if !canSteal {
		return false
	}
	child, err := d.openChildPart(stealStart, stealEnd)
	if err != nil {
		d.Log("%s: work steal skipped - child part was not created: %v", stealerHash, err)
		return false
	}
	newVictimFoff := stealStart - 1
	victim.foff.Store(newVictimFoff)
	victim.stolen.Store(true)
	victimHash := victim.hash
	victimOffset := victim.offset
	victimPos := victim.getCurrentPos()
	d.Log("%s: stealing work from %s | bytes %d-%d", stealerHash, victimHash, stealStart, stealEnd)
	// Publish the shortened parent and the child together. Doing this only
	// after the child file exists means a failed create cannot drop the tail.
	d.publishPartSplit(victimHash, victimOffset, victimPos, newVictimFoff, child.hash, stealStart, stealEnd)
	d.handlers.WorkStealHandler(stealerHash, victimHash, stealStart, stealEnd)

	d.wg.Add(1)
	go d.runNewPart(child, stealStart, stealEnd, WORK_STEAL_SPEED_THRESHOLD/2, nil)
	return true
}
