package api

import (
	"encoding/json"
	"testing"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// newTestApiWithQueue creates a test API with queue enabled.
func newTestApiWithQueue(t *testing.T, maxConcurrent int) (*Api, func()) {
	t.Helper()
	api, _, cleanup := newTestApi(t)
	api.manager.SetMaxConcurrentDownloads(maxConcurrent, nil)
	return api, cleanup
}

// TestQueueStatusHandler tests the queue status handler.
func TestQueueStatusHandler(t *testing.T) {
	t.Run("with queue enabled", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 2)
		defer cleanup()

		queue := api.manager.GetQueue()
		if queue == nil {
			t.Fatal("expected queue to be enabled")
		}

		// Add some items to the queue
		queue.Add("hash1", warplib.PriorityNormal)
		queue.Add("hash2", warplib.PriorityHigh)
		queue.Add("hash3", warplib.PriorityLow) // This should be waiting

		_, msg, err := api.queueStatusHandler(nil, nil, nil)
		if err != nil {
			t.Fatalf("queueStatusHandler: %v", err)
		}

		resp := msg.(*common.QueueStatusResponse)
		if resp.MaxConcurrent != 2 {
			t.Fatalf("expected MaxConcurrent=2, got %d", resp.MaxConcurrent)
		}
		if resp.ActiveCount != 2 {
			t.Fatalf("expected ActiveCount=2, got %d", resp.ActiveCount)
		}
		if resp.WaitingCount != 1 {
			t.Fatalf("expected WaitingCount=1, got %d", resp.WaitingCount)
		}
		if len(resp.Active) != 2 {
			t.Fatalf("expected 2 active hashes, got %d", len(resp.Active))
		}
		if len(resp.Waiting) != 1 {
			t.Fatalf("expected 1 waiting item, got %d", len(resp.Waiting))
		}
		if resp.Waiting[0].Hash != "hash3" {
			t.Fatalf("expected waiting hash=hash3, got %s", resp.Waiting[0].Hash)
		}
		if resp.Waiting[0].Priority != int(warplib.PriorityLow) {
			t.Fatalf("expected priority=%d, got %d", warplib.PriorityLow, resp.Waiting[0].Priority)
		}
		if resp.Waiting[0].Position != 0 {
			t.Fatalf("expected position=0, got %d", resp.Waiting[0].Position)
		}
	})

	t.Run("without queue", func(t *testing.T) {
		api, _, cleanup := newTestApi(t)
		defer cleanup()

		// Ensure queue is nil (default state)
		if api.manager.GetQueue() != nil {
			t.Fatal("expected queue to be nil by default")
		}

		_, msg, err := api.queueStatusHandler(nil, nil, nil)
		if err != nil {
			t.Fatalf("queueStatusHandler: %v", err)
		}

		resp := msg.(*common.QueueStatusResponse)
		if len(resp.Active) != 0 {
			t.Fatalf("expected empty Active, got %d", len(resp.Active))
		}
		if len(resp.Waiting) != 0 {
			t.Fatalf("expected empty Waiting, got %d", len(resp.Waiting))
		}
	})

	t.Run("paused state reflected", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 2)
		defer cleanup()

		queue := api.manager.GetQueue()
		queue.Pause()

		_, msg, err := api.queueStatusHandler(nil, nil, nil)
		if err != nil {
			t.Fatalf("queueStatusHandler: %v", err)
		}

		resp := msg.(*common.QueueStatusResponse)
		if !resp.Paused {
			t.Fatal("expected Paused=true")
		}
	})
}

// TestQueuePauseHandler tests the queue pause handler.
func TestQueuePauseHandler(t *testing.T) {
	t.Run("pause queue", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 2)
		defer cleanup()

		queue := api.manager.GetQueue()
		if queue.IsPaused() {
			t.Fatal("queue should not be paused initially")
		}

		updateType, _, err := api.queuePauseHandler(nil, nil, nil)
		if err != nil {
			t.Fatalf("queuePauseHandler: %v", err)
		}
		if updateType != common.UPDATE_QUEUE_PAUSE {
			t.Fatalf("expected UPDATE_QUEUE_PAUSE, got %v", updateType)
		}

		if !queue.IsPaused() {
			t.Fatal("expected queue to be paused")
		}
	})

	t.Run("pause without queue", func(t *testing.T) {
		api, _, cleanup := newTestApi(t)
		defer cleanup()

		_, _, err := api.queuePauseHandler(nil, nil, nil)
		if err == nil {
			t.Fatal("expected error when pausing nil queue")
		}
		if err.Error() != "queue not enabled" {
			t.Fatalf("expected 'queue not enabled' error, got: %v", err)
		}
	})
}

// TestQueueResumeHandler tests the queue resume handler.
func TestQueueResumeHandler(t *testing.T) {
	t.Run("resume paused queue", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 2)
		defer cleanup()

		queue := api.manager.GetQueue()
		queue.Pause()
		if !queue.IsPaused() {
			t.Fatal("queue should be paused")
		}

		updateType, _, err := api.queueResumeHandler(nil, nil, nil)
		if err != nil {
			t.Fatalf("queueResumeHandler: %v", err)
		}
		if updateType != common.UPDATE_QUEUE_RESUME {
			t.Fatalf("expected UPDATE_QUEUE_RESUME, got %v", updateType)
		}

		if queue.IsPaused() {
			t.Fatal("expected queue to be resumed")
		}
	})

	t.Run("resume without queue", func(t *testing.T) {
		api, _, cleanup := newTestApi(t)
		defer cleanup()

		_, _, err := api.queueResumeHandler(nil, nil, nil)
		if err == nil {
			t.Fatal("expected error when resuming nil queue")
		}
		if err.Error() != "queue not enabled" {
			t.Fatalf("expected 'queue not enabled' error, got: %v", err)
		}
	})
}

// TestQueueMoveHandler tests the queue move handler.
func TestQueueMoveHandler(t *testing.T) {
	t.Run("move valid hash", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 1)
		defer cleanup()

		queue := api.manager.GetQueue()
		// hash1 becomes active (only 1 slot), rest go to waiting
		queue.Add("hash1", warplib.PriorityNormal)
		queue.Add("hash2", warplib.PriorityNormal)
		queue.Add("hash3", warplib.PriorityNormal)
		queue.Add("hash4", warplib.PriorityNormal)

		// Initial waiting order: hash2, hash3, hash4
		waitingBefore := queue.GetWaitingItems()
		if len(waitingBefore) != 3 {
			t.Fatalf("expected 3 waiting items, got %d", len(waitingBefore))
		}
		if waitingBefore[0].Hash != "hash2" {
			t.Fatalf("expected hash2 at position 0, got %s", waitingBefore[0].Hash)
		}

		// Move hash4 to position 0
		body, _ := json.Marshal(common.QueueMoveParams{Hash: "hash4", Position: 0})
		updateType, _, err := api.queueMoveHandler(nil, nil, body)
		if err != nil {
			t.Fatalf("queueMoveHandler: %v", err)
		}
		if updateType != common.UPDATE_QUEUE_MOVE {
			t.Fatalf("expected UPDATE_QUEUE_MOVE, got %v", updateType)
		}

		// After move: hash4, hash2, hash3
		waitingAfter := queue.GetWaitingItems()
		if waitingAfter[0].Hash != "hash4" {
			t.Fatalf("expected hash4 at position 0 after move, got %s", waitingAfter[0].Hash)
		}
		if waitingAfter[1].Hash != "hash2" {
			t.Fatalf("expected hash2 at position 1 after move, got %s", waitingAfter[1].Hash)
		}
		if waitingAfter[2].Hash != "hash3" {
			t.Fatalf("expected hash3 at position 2 after move, got %s", waitingAfter[2].Hash)
		}
	})

	t.Run("move invalid hash", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 2)
		defer cleanup()

		body, _ := json.Marshal(common.QueueMoveParams{Hash: "nonexistent", Position: 0})
		_, _, err := api.queueMoveHandler(nil, nil, body)
		if err == nil {
			t.Fatal("expected error for invalid hash")
		}
		// The error comes from warplib.ErrQueueHashNotFound
		if err.Error() != "download not found in waiting queue" {
			t.Fatalf("expected 'download not found in waiting queue' error, got: %v", err)
		}
	})

	t.Run("move active hash", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 2)
		defer cleanup()

		queue := api.manager.GetQueue()
		queue.Add("active1", warplib.PriorityNormal) // Goes to active

		body, _ := json.Marshal(common.QueueMoveParams{Hash: "active1", Position: 0})
		_, _, err := api.queueMoveHandler(nil, nil, body)
		if err == nil {
			t.Fatal("expected error for moving active download")
		}
		if err.Error() != "cannot move active download, only waiting downloads can be moved" {
			t.Fatalf("expected 'cannot move active download' error, got: %v", err)
		}
	})

	t.Run("move without queue", func(t *testing.T) {
		api, _, cleanup := newTestApi(t)
		defer cleanup()

		body, _ := json.Marshal(common.QueueMoveParams{Hash: "hash1", Position: 0})
		_, _, err := api.queueMoveHandler(nil, nil, body)
		if err == nil {
			t.Fatal("expected error when moving in nil queue")
		}
		if err.Error() != "queue not enabled" {
			t.Fatalf("expected 'queue not enabled' error, got: %v", err)
		}
	})

	t.Run("move missing hash param", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 2)
		defer cleanup()

		body, _ := json.Marshal(common.QueueMoveParams{Hash: "", Position: 0})
		_, _, err := api.queueMoveHandler(nil, nil, body)
		if err == nil {
			t.Fatal("expected error for missing hash")
		}
		if err.Error() != "hash is required" {
			t.Fatalf("expected 'hash is required' error, got: %v", err)
		}
	})

	t.Run("move bad JSON", func(t *testing.T) {
		api, cleanup := newTestApiWithQueue(t, 2)
		defer cleanup()

		_, _, err := api.queueMoveHandler(nil, nil, []byte("{"))
		if err == nil {
			t.Fatal("expected error for bad JSON")
		}
	})
}
