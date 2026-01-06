package warplib

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Work stealing constants define thresholds for dynamic part merging.
const (
	// WORK_STEAL_SPEED_THRESHOLD is the minimum download speed (bytes/sec)
	// a part must achieve to be considered "fast" enough to steal work.
	// Parts completing faster than this may steal work from slower adjacent parts.
	WORK_STEAL_SPEED_THRESHOLD = 10 * MB // >10MB/s

	// WORK_STEAL_MIN_REMAINING is the minimum remaining bytes in an adjacent
	// part to be eligible for work stealing. This prevents excessive overhead
	// from stealing very small work amounts.
	WORK_STEAL_MIN_REMAINING = 5 * MB // >5MB
)

// isMergeCandidate checks if a part's download speed qualifies for work stealing.
// Returns true if the part downloaded faster than WORK_STEAL_SPEED_THRESHOLD.
//
// Parameters:
//   - bytesRead: total bytes downloaded by the part
//   - duration: time taken to download those bytes
//
// Returns true only if speed > 10MB/s (strictly greater than).
func isMergeCandidate(bytesRead int64, duration time.Duration) bool {
	// Guard against invalid inputs
	if bytesRead <= 0 || duration <= 0 {
		return false
	}

	// Calculate speed in bytes per second
	// speed = bytesRead / duration.Seconds()
	// To avoid floating point, use: speed = bytesRead * Second / duration
	speed := (bytesRead * int64(time.Second)) / int64(duration)

	// Must be strictly greater than threshold
	return speed > WORK_STEAL_SPEED_THRESHOLD
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

// shouldAttemptWorkSteal validates both speed and remaining bytes thresholds.
// Returns true only if both conditions are met:
//   - completionSpeed > WORK_STEAL_SPEED_THRESHOLD (10MB/s)
//   - adjacentRemaining > WORK_STEAL_MIN_REMAINING (5MB)
//
// Parameters:
//   - completionSpeed: the completing part's download speed in bytes/sec
//   - adjacentRemaining: bytes remaining in the adjacent part
func shouldAttemptWorkSteal(completionSpeed, adjacentRemaining int64) bool {
	// Both must be positive
	if completionSpeed <= 0 || adjacentRemaining <= 0 {
		return false
	}

	// Both thresholds must be exceeded (strictly greater than)
	return completionSpeed > WORK_STEAL_SPEED_THRESHOLD &&
		adjacentRemaining > WORK_STEAL_MIN_REMAINING
}

// activePartInfo tracks runtime state of an active downloading part for work stealing.
// This struct is used to coordinate work stealing between parts.
type activePartInfo struct {
	hash   string     // Unique identifier for the part
	offset int64      // Initial byte offset (starting position)
	foff   *int64     // Pointer to final offset (can be reduced by work stealing)
	read   *int64     // Pointer to part's atomic read counter
	mu     sync.Mutex // Protects foff modifications during steal operations
	stolen bool       // True if work was already stolen from this part
}

// getRemaining calculates the remaining bytes to download for this part.
// Returns the number of bytes left to download.
func (a *activePartInfo) getRemaining() int64 {
	currentPos := a.getCurrentPos()
	foff := atomic.LoadInt64(a.foff)
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
		// Skip if already had work stolen
		if info.stolen {
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
// Called when a part starts downloading.
func (d *Downloader) registerActivePart(part *Part, foff *int64) {
	if !d.enableWorkStealing {
		return
	}
	d.activeParts.Set(part.hash, &activePartInfo{
		hash:   part.hash,
		offset: part.offset,
		foff:   foff,
		read:   &part.read,
	})
}

// unregisterActivePart removes a completed part from the work stealing pool.
// Called when a part finishes downloading.
func (d *Downloader) unregisterActivePart(hash string) {
	if !d.enableWorkStealing {
		return
	}
	d.activeParts.Delete(hash)
}

// attemptWorkSteal tries to steal work from a slow part after fast completion.
// Returns true if work stealing was initiated.
//
// Parameters:
//   - stealerHash: the hash of the part that just completed
//   - partSpeed: the download speed achieved by the completed part (bytes/sec)
func (d *Downloader) attemptWorkSteal(stealerHash string, partSpeed int64) bool {
	if !d.enableWorkStealing {
		return false
	}

	// Check if part was fast enough to warrant work stealing
	if partSpeed <= WORK_STEAL_SPEED_THRESHOLD {
		return false
	}

	// Check connection limit (use atomic load for thread safety)
	if d.maxConn != 0 && atomic.LoadInt32(&d.numConn) >= d.maxConn {
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

	// Lock victim to perform atomic steal operation
	victim.mu.Lock()
	defer victim.mu.Unlock()

	// Double-check conditions under lock
	if victim.stolen {
		return false
	}

	remaining := victim.getRemaining()
	if remaining <= WORK_STEAL_MIN_REMAINING {
		return false
	}

	// Calculate steal range using 50/50 split
	stealStart, stealEnd, canSteal := calculateStealWork(
		victim.offset,
		atomic.LoadInt64(victim.foff),
		atomic.LoadInt64(victim.read),
	)
	if !canSteal {
		return false
	}

	// Reduce victim's final offset (atomic for part's benefit)
	newVictimFoff := stealStart - 1
	atomic.StoreInt64(victim.foff, newVictimFoff)
	victim.stolen = true

	d.Log("%s: stealing work from %s | bytes %d-%d", stealerHash, victim.hash, stealStart, stealEnd)
	d.handlers.WorkStealHandler(stealerHash, victim.hash, stealStart, stealEnd)

	// Spawn new part to handle stolen range
	d.wg.Add(1)
	go func(ioff, foff int64) {
		defer func() {
			if r := recover(); r != nil {
				d.Log("PANIC in work steal newPartDownload: %v", r)
				d.handlers.ErrorHandler("work-steal-part", fmt.Errorf("panic: %v", r))
				atomic.StoreInt32(&d.stopped, 1)
				d.cancel()
			}
		}()
		// Use half the speed threshold as expected speed for stolen part
		d.newPartDownload(ioff, foff, WORK_STEAL_SPEED_THRESHOLD/2)
	}(stealStart, stealEnd)

	return true
}
