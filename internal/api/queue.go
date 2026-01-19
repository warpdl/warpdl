package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

// queueStatusHandler returns the current queue status including active and waiting downloads.
func (s *Api) queueStatusHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	queue := s.manager.GetQueue()
	if queue == nil {
		// Queue not enabled, return empty response
		return common.UPDATE_QUEUE_STATUS, &common.QueueStatusResponse{
			Active:  []string{},
			Waiting: []common.QueueItemInfo{},
		}, nil
	}

	// Get active hashes
	activeHashes := queue.GetActiveHashes()

	// Get waiting items and convert to QueueItemInfo
	waitingItems := queue.GetWaitingItems()
	waiting := make([]common.QueueItemInfo, len(waitingItems))
	for i, item := range waitingItems {
		waiting[i] = common.QueueItemInfo{
			Hash:     item.Hash,
			Priority: int(item.Priority),
			Position: i,
		}
	}

	return common.UPDATE_QUEUE_STATUS, &common.QueueStatusResponse{
		MaxConcurrent: queue.MaxConcurrent(),
		ActiveCount:   queue.ActiveCount(),
		WaitingCount:  queue.WaitingCount(),
		Paused:        queue.IsPaused(),
		Active:        activeHashes,
		Waiting:       waiting,
	}, nil
}

// queuePauseHandler pauses the download queue, preventing auto-start of waiting downloads.
func (s *Api) queuePauseHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	queue := s.manager.GetQueue()
	if queue == nil {
		return common.UPDATE_QUEUE_PAUSE, nil, errors.New("queue not enabled")
	}
	queue.Pause()
	return common.UPDATE_QUEUE_PAUSE, nil, nil
}

// queueResumeHandler resumes the download queue, allowing auto-start of waiting downloads.
func (s *Api) queueResumeHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	queue := s.manager.GetQueue()
	if queue == nil {
		return common.UPDATE_QUEUE_RESUME, nil, errors.New("queue not enabled")
	}
	queue.Resume()
	return common.UPDATE_QUEUE_RESUME, nil, nil
}

// queueMoveHandler moves a queued download to a new position in the waiting queue.
func (s *Api) queueMoveHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.QueueMoveParams
	if err := json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_QUEUE_MOVE, nil, err
	}
	if m.Hash == "" {
		return common.UPDATE_QUEUE_MOVE, nil, errors.New("hash is required")
	}

	queue := s.manager.GetQueue()
	if queue == nil {
		return common.UPDATE_QUEUE_MOVE, nil, errors.New("queue not enabled")
	}

	if err := queue.Move(m.Hash, m.Position); err != nil {
		return common.UPDATE_QUEUE_MOVE, nil, err
	}
	return common.UPDATE_QUEUE_MOVE, nil, nil
}
