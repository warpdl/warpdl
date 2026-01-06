package warplib

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// TDD Cycle 1: isMergeCandidate Tests (RED)
// =============================================================================

func TestIsMergeCandidate_SpeedAboveThreshold(t *testing.T) {
	tests := []struct {
		name            string
		bytesRead       int64
		duration        time.Duration
		expectCandidate bool
	}{
		{
			name:            "exactly at threshold (10MB/s) - not a candidate",
			bytesRead:       10 * MB,
			duration:        time.Second,
			expectCandidate: false, // must be >10MB/s, not >=
		},
		{
			name:            "above threshold (15MB/s)",
			bytesRead:       15 * MB,
			duration:        time.Second,
			expectCandidate: true,
		},
		{
			name:            "well above threshold (100MB/s)",
			bytesRead:       100 * MB,
			duration:        time.Second,
			expectCandidate: true,
		},
		{
			name:            "below threshold (5MB/s)",
			bytesRead:       5 * MB,
			duration:        time.Second,
			expectCandidate: false,
		},
		{
			name:            "very fast (1GB/s)",
			bytesRead:       1 * GB,
			duration:        time.Second,
			expectCandidate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMergeCandidate(tt.bytesRead, tt.duration)
			if got != tt.expectCandidate {
				t.Errorf("isMergeCandidate(%d, %v) = %v, want %v",
					tt.bytesRead, tt.duration, got, tt.expectCandidate)
			}
		})
	}
}

func TestIsMergeCandidate_EdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		bytesRead       int64
		duration        time.Duration
		expectCandidate bool
	}{
		{
			name:            "zero duration - should not panic, not candidate",
			bytesRead:       10 * MB,
			duration:        0,
			expectCandidate: false,
		},
		{
			name:            "zero bytes - not candidate",
			bytesRead:       0,
			duration:        time.Second,
			expectCandidate: false,
		},
		{
			name:            "negative bytes - not candidate",
			bytesRead:       -1,
			duration:        time.Second,
			expectCandidate: false,
		},
		{
			name:            "negative duration - not candidate",
			bytesRead:       10 * MB,
			duration:        -time.Second,
			expectCandidate: false,
		},
		{
			name:            "very small duration (1ms) with proportional bytes - exactly 10MB/s",
			bytesRead:       10 * KB, // 10KB in 1ms = 10MB/s
			duration:        time.Millisecond,
			expectCandidate: false, // exactly at threshold
		},
		{
			name:            "very small duration (1ms) above threshold - 11MB/s",
			bytesRead:       11 * KB, // 11KB in 1ms = 11MB/s
			duration:        time.Millisecond,
			expectCandidate: true,
		},
		{
			name:            "both zero - not candidate",
			bytesRead:       0,
			duration:        0,
			expectCandidate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMergeCandidate(tt.bytesRead, tt.duration)
			if got != tt.expectCandidate {
				t.Errorf("isMergeCandidate(%d, %v) = %v, want %v",
					tt.bytesRead, tt.duration, got, tt.expectCandidate)
			}
		})
	}
}

// =============================================================================
// TDD Cycle 2: calculateStealWork Tests (RED)
// =============================================================================

func TestCalculateStealWork_Basic(t *testing.T) {
	tests := []struct {
		name              string
		adjacentStartOff  int64
		adjacentEndOff    int64
		adjacentBytesRead int64
		expectStealStart  int64
		expectStealEnd    int64
		expectCanSteal    bool
	}{
		{
			name:              "steal from part with 10MB remaining",
			adjacentStartOff:  0,
			adjacentEndOff:    20*MB - 1,
			adjacentBytesRead: 10 * MB, // read 10MB, remaining 10MB
			expectStealStart:  15 * MB, // steal second half: 15MB to end
			expectStealEnd:    20*MB - 1,
			expectCanSteal:    true,
		},
		{
			name:              "steal from part with exactly 5MB remaining - not eligible",
			adjacentStartOff:  0,
			adjacentEndOff:    10*MB - 1,
			adjacentBytesRead: 5 * MB, // exactly 5MB remaining
			expectStealStart:  0,
			expectStealEnd:    0,
			expectCanSteal:    false, // must be >5MB, not >=
		},
		{
			name:              "steal from part with 5MB+1 remaining - eligible",
			adjacentStartOff:  0,
			adjacentEndOff:    10*MB + 1 - 1,          // total: 10MB+1 bytes
			adjacentBytesRead: 5 * MB,                 // remaining: 5MB+1 bytes
			expectStealStart:  (5*MB + 10*MB + 1) / 2, // midpoint of remaining
			expectStealEnd:    10*MB + 1 - 1,
			expectCanSteal:    true,
		},
		{
			name:              "steal from half-complete adjacent with 500 bytes remaining",
			adjacentStartOff:  1000,
			adjacentEndOff:    1999,
			adjacentBytesRead: 500, // read bytes 1000-1499, remaining 1500-1999 (500 bytes)
			expectStealStart:  0,
			expectStealEnd:    0,
			expectCanSteal:    false, // only 500 bytes remaining, below 5MB threshold
		},
		{
			name:              "adjacent already complete - cannot steal",
			adjacentStartOff:  0,
			adjacentEndOff:    10*MB - 1,
			adjacentBytesRead: 10 * MB, // fully read
			expectStealStart:  0,
			expectStealEnd:    0,
			expectCanSteal:    false,
		},
		{
			name:              "steal from part with 20MB remaining - take second half",
			adjacentStartOff:  0,
			adjacentEndOff:    30*MB - 1,
			adjacentBytesRead: 10 * MB, // remaining 20MB
			expectStealStart:  20 * MB, // steal second half: 20MB to end
			expectStealEnd:    30*MB - 1,
			expectCanSteal:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd, gotCanSteal := calculateStealWork(
				tt.adjacentStartOff, tt.adjacentEndOff, tt.adjacentBytesRead)

			if gotCanSteal != tt.expectCanSteal {
				t.Errorf("calculateStealWork() canSteal = %v, want %v",
					gotCanSteal, tt.expectCanSteal)
			}

			if gotCanSteal && tt.expectCanSteal {
				if gotStart != tt.expectStealStart || gotEnd != tt.expectStealEnd {
					t.Errorf("calculateStealWork() = (%d, %d), want (%d, %d)",
						gotStart, gotEnd, tt.expectStealStart, tt.expectStealEnd)
				}
			}
		})
	}
}

func TestCalculateStealWork_EdgeCases(t *testing.T) {
	tests := []struct {
		name              string
		adjacentStartOff  int64
		adjacentEndOff    int64
		adjacentBytesRead int64
		expectCanSteal    bool
	}{
		{
			name:              "negative bytes read - treat as zero, full range available",
			adjacentStartOff:  0,
			adjacentEndOff:    20*MB - 1,
			adjacentBytesRead: -1,
			expectCanSteal:    true, // full 20MB available
		},
		{
			name:              "bytes read exceeds range - corruption, cannot steal",
			adjacentStartOff:  0,
			adjacentEndOff:    10*MB - 1,
			adjacentBytesRead: 15 * MB, // more than total
			expectCanSteal:    false,
		},
		{
			name:              "zero-length range",
			adjacentStartOff:  1000,
			adjacentEndOff:    1000, // single byte
			adjacentBytesRead: 0,
			expectCanSteal:    false, // only 1 byte remaining
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, gotCanSteal := calculateStealWork(
				tt.adjacentStartOff, tt.adjacentEndOff, tt.adjacentBytesRead)
			if gotCanSteal != tt.expectCanSteal {
				t.Errorf("calculateStealWork() canSteal = %v, want %v",
					gotCanSteal, tt.expectCanSteal)
			}
		})
	}
}

// =============================================================================
// TDD Cycle 3: shouldAttemptWorkSteal Tests (RED)
// =============================================================================

func TestShouldAttemptWorkSteal_CombinedChecks(t *testing.T) {
	tests := []struct {
		name              string
		completionSpeed   int64 // bytes/sec
		adjacentRemaining int64 // bytes remaining in adjacent
		expectAttempt     bool
	}{
		{
			name:              "fast with large remaining - should steal",
			completionSpeed:   15 * MB, // 15MB/s
			adjacentRemaining: 10 * MB,
			expectAttempt:     true,
		},
		{
			name:              "fast but small remaining - should not steal",
			completionSpeed:   15 * MB,
			adjacentRemaining: 4 * MB,
			expectAttempt:     false,
		},
		{
			name:              "slow with large remaining - should not steal",
			completionSpeed:   5 * MB,
			adjacentRemaining: 10 * MB,
			expectAttempt:     false,
		},
		{
			name:              "slow with small remaining - should not steal",
			completionSpeed:   5 * MB,
			adjacentRemaining: 4 * MB,
			expectAttempt:     false,
		},
		{
			name:              "exactly at speed threshold with large remaining - should not steal",
			completionSpeed:   10 * MB,
			adjacentRemaining: 10 * MB,
			expectAttempt:     false, // >10MB/s required
		},
		{
			name:              "above speed threshold with exactly minimum remaining - should not steal",
			completionSpeed:   15 * MB,
			adjacentRemaining: 5 * MB,
			expectAttempt:     false, // >5MB required
		},
		{
			name:              "just above both thresholds - should steal",
			completionSpeed:   10*MB + 1,
			adjacentRemaining: 5*MB + 1,
			expectAttempt:     true,
		},
		{
			name:              "zero speed - should not steal",
			completionSpeed:   0,
			adjacentRemaining: 10 * MB,
			expectAttempt:     false,
		},
		{
			name:              "zero remaining - should not steal",
			completionSpeed:   15 * MB,
			adjacentRemaining: 0,
			expectAttempt:     false,
		},
		{
			name:              "negative speed - should not steal",
			completionSpeed:   -1,
			adjacentRemaining: 10 * MB,
			expectAttempt:     false,
		},
		{
			name:              "negative remaining - should not steal",
			completionSpeed:   15 * MB,
			adjacentRemaining: -1,
			expectAttempt:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAttemptWorkSteal(tt.completionSpeed, tt.adjacentRemaining)
			if got != tt.expectAttempt {
				t.Errorf("shouldAttemptWorkSteal(%d, %d) = %v, want %v",
					tt.completionSpeed, tt.adjacentRemaining, got, tt.expectAttempt)
			}
		})
	}
}

// =============================================================================
// TDD Cycle 5: activePartInfo Tests (RED)
// =============================================================================

func TestActivePartInfo_GetRemaining(t *testing.T) {
	tests := []struct {
		name            string
		offset          int64
		foff            int64
		read            int64
		expectRemaining int64
	}{
		{
			name:            "part at start with all remaining",
			offset:          0,
			foff:            20*MB - 1,
			read:            0,
			expectRemaining: 20 * MB,
		},
		{
			name:            "part half complete",
			offset:          0,
			foff:            20*MB - 1,
			read:            10 * MB,
			expectRemaining: 10 * MB,
		},
		{
			name:            "part almost complete",
			offset:          10 * MB,
			foff:            20*MB - 1,
			read:            9*MB + 500*KB,
			expectRemaining: 10*MB - (9*MB + 500*KB), // remaining = total - read
		},
		{
			name:            "part complete",
			offset:          0,
			foff:            10*MB - 1,
			read:            10 * MB,
			expectRemaining: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			foff := tt.foff
			read := tt.read
			info := &activePartInfo{
				hash:   "test",
				offset: tt.offset,
				foff:   &foff,
				read:   &read,
			}
			got := info.getRemaining()
			if got != tt.expectRemaining {
				t.Errorf("getRemaining() = %d, want %d", got, tt.expectRemaining)
			}
		})
	}
}

func TestActivePartInfo_GetCurrentPos(t *testing.T) {
	tests := []struct {
		name      string
		offset    int64
		read      int64
		expectPos int64
	}{
		{
			name:      "at start",
			offset:    0,
			read:      0,
			expectPos: 0,
		},
		{
			name:      "with offset",
			offset:    10 * MB,
			read:      5 * MB,
			expectPos: 15 * MB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := tt.read
			info := &activePartInfo{
				offset: tt.offset,
				read:   &read,
			}
			got := info.getCurrentPos()
			if got != tt.expectPos {
				t.Errorf("getCurrentPos() = %d, want %d", got, tt.expectPos)
			}
		})
	}
}

// =============================================================================
// TDD Cycle 6: findAdjacentPartForStealing Tests (RED)
// =============================================================================

// =============================================================================
// TDD Cycle 8: WorkStealHandler Tests (RED)
// =============================================================================

func TestWorkStealHandler_Invocation(t *testing.T) {
	var handlerCalled int32
	var capturedStealer, capturedVictim string
	var capturedIoff, capturedFoff int64
	var mu sync.Mutex

	handler := WorkStealHandlerFunc(func(stealerHash, victimHash string, stolenIoff, stolenFoff int64) {
		mu.Lock()
		defer mu.Unlock()
		atomic.AddInt32(&handlerCalled, 1)
		capturedStealer = stealerHash
		capturedVictim = victimHash
		capturedIoff = stolenIoff
		capturedFoff = stolenFoff
	})

	// Simulate work steal event
	handler("partA", "partB", 10*MB, 20*MB-1)

	if atomic.LoadInt32(&handlerCalled) != 1 {
		t.Errorf("handler should be called once, got %d", atomic.LoadInt32(&handlerCalled))
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedStealer != "partA" {
		t.Errorf("stealer = %q, want %q", capturedStealer, "partA")
	}
	if capturedVictim != "partB" {
		t.Errorf("victim = %q, want %q", capturedVictim, "partB")
	}
	if capturedIoff != 10*MB {
		t.Errorf("stolenIoff = %d, want %d", capturedIoff, 10*MB)
	}
	if capturedFoff != 20*MB-1 {
		t.Errorf("stolenFoff = %d, want %d", capturedFoff, 20*MB-1)
	}
}

func TestFindBestVictimForStealing(t *testing.T) {
	tests := []struct {
		name        string
		parts       map[string]*activePartInfo
		expectHash  string
		expectFound bool
	}{
		{
			name: "find part with most remaining",
			parts: func() map[string]*activePartInfo {
				parts := make(map[string]*activePartInfo)
				// Part A: 5MB remaining (below threshold)
				foffA := int64(10*MB - 1)
				readA := int64(5 * MB)
				parts["partA"] = &activePartInfo{
					hash:   "partA",
					offset: 0,
					foff:   &foffA,
					read:   &readA,
				}
				// Part B: 15MB remaining (above threshold, best candidate)
				foffB := int64(20*MB - 1)
				readB := int64(5 * MB)
				parts["partB"] = &activePartInfo{
					hash:   "partB",
					offset: 0,
					foff:   &foffB,
					read:   &readB,
				}
				// Part C: 8MB remaining (above threshold, but less than B)
				foffC := int64(15*MB - 1)
				readC := int64(7 * MB)
				parts["partC"] = &activePartInfo{
					hash:   "partC",
					offset: 0,
					foff:   &foffC,
					read:   &readC,
				}
				return parts
			}(),
			expectHash:  "partB",
			expectFound: true,
		},
		{
			name: "no eligible parts (all below threshold)",
			parts: func() map[string]*activePartInfo {
				parts := make(map[string]*activePartInfo)
				foffA := int64(5*MB - 1)
				readA := int64(0)
				parts["partA"] = &activePartInfo{
					hash:   "partA",
					offset: 0,
					foff:   &foffA,
					read:   &readA,
				}
				return parts
			}(),
			expectHash:  "",
			expectFound: false,
		},
		{
			name: "skip already stolen parts",
			parts: func() map[string]*activePartInfo {
				parts := make(map[string]*activePartInfo)
				// Part A: 15MB remaining but already stolen
				foffA := int64(20*MB - 1)
				readA := int64(5 * MB)
				parts["partA"] = &activePartInfo{
					hash:   "partA",
					offset: 0,
					foff:   &foffA,
					read:   &readA,
					stolen: true,
				}
				// Part B: 8MB remaining, not stolen
				foffB := int64(10*MB - 1)
				readB := int64(2 * MB)
				parts["partB"] = &activePartInfo{
					hash:   "partB",
					offset: 0,
					foff:   &foffB,
					read:   &readB,
					stolen: false,
				}
				return parts
			}(),
			expectHash:  "partB",
			expectFound: true,
		},
		{
			name:        "empty parts map",
			parts:       make(map[string]*activePartInfo),
			expectHash:  "",
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activeParts := NewVMap[string, *activePartInfo]()
			for k, v := range tt.parts {
				activeParts.Set(k, v)
			}
			gotInfo := findBestVictimForStealing(&activeParts)
			if tt.expectFound {
				if gotInfo == nil {
					t.Errorf("findBestVictimForStealing() returned nil, want %q", tt.expectHash)
				} else if gotInfo.hash != tt.expectHash {
					t.Errorf("findBestVictimForStealing() = %q, want %q", gotInfo.hash, tt.expectHash)
				}
			} else {
				if gotInfo != nil {
					t.Errorf("findBestVictimForStealing() = %q, want nil", gotInfo.hash)
				}
			}
		})
	}
}
