package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"

	"github.com/warpdl/warpdl/internal/server"
)

func finishStoppedGeneration(pool *server.Pool, uid string) {
	frame := server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
		DownloadId: uid,
		Action:     common.DownloadStopped,
	})
	if generation, ok := pool.ManagedGeneration(uid); ok {
		generation.RecordTerminal(frame)
		generation.Finish(frame)
		return
	}
	pool.BroadcastTerminal(uid, frame)
}

func (s *Api) stopHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.InputDownloadId
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_STOP, nil, err
	}
	if m.DownloadId == "" {
		return common.UPDATE_STOP, nil, errors.New("download_id is required")
	}
	item := s.manager.GetItem(m.DownloadId)
	if item == nil {
		return common.UPDATE_STOP, nil, errors.New("download not found")
	}

	// Handle scheduled items by durably cancelling the schedule as part of
	// the explicit user stop. A one-shot item remains Triggered while it is
	// queued/running so a daemon crash can restore it; leaving that state
	// behind after a user stop would incorrectly restart it on the next boot.
	// SetScheduleStateIf also closes the trigger-vs-cancel read/write race.
	scheduleInfo, _ := s.manager.GetScheduleInfo(m.DownloadId)
	cancelOneShot := scheduleInfo.CronExpr == "" &&
		(scheduleInfo.State == warplib.ScheduleStateScheduled ||
			scheduleInfo.State == warplib.ScheduleStateMissed ||
			scheduleInfo.State == warplib.ScheduleStateTriggered)
	cancelRecurring := scheduleInfo.CronExpr != "" &&
		scheduleInfo.State == warplib.ScheduleStateScheduled
	if cancelOneShot || cancelRecurring {
		cancelled, transitionErr := s.manager.SetScheduleStateIf(
			m.DownloadId,
			warplib.ScheduleStateCancelled,
			warplib.ScheduleStateScheduled,
			warplib.ScheduleStateMissed,
			warplib.ScheduleStateTriggered,
		)
		if transitionErr != nil {
			return common.UPDATE_STOP, nil, transitionErr
		}
		if !cancelled {
			return common.UPDATE_STOP, nil, errors.New("schedule is no longer cancellable")
		}
		if s.scheduler != nil {
			s.scheduler.Remove(m.DownloadId)
		}
		// The next recurring occurrence may already be running while its
		// persisted state points at the following occurrence. Stop that active
		// transfer as part of cancellation. Let an active transfer broadcast
		// DownloadStopped before its pool entry is removed; idle schedules have
		// no worker callback and are unregistered directly below.
		if item.IsDownloading() {
			if err := item.StopDownload(); err != nil {
				return common.UPDATE_STOP, nil, err
			}
			deadline := time.Now().Add(5 * time.Second)
			for pool.HasDownload(m.DownloadId) && time.Now().Before(deadline) {
				time.Sleep(25 * time.Millisecond)
			}
			if pool.HasDownload(m.DownloadId) {
				// Stop is only a cancellation request. Keep the queue slot and
				// pool generation while the old worker is still draining; its
				// terminal callback owns both lifecycle transitions.
				if scheduleInfo.CronExpr != "" {
					return common.UPDATE_STOP, fmt.Sprintf("Cancelled recurring schedule for %s", scheduleInfo.Name), nil
				}
				return common.UPDATE_STOP, fmt.Sprintf("Cancelled scheduled download: %s", scheduleInfo.Name), nil
			}
		}
		finishStoppedGeneration(pool, m.DownloadId)
		s.manager.ReleaseQueueSlot(m.DownloadId)
		if scheduleInfo.CronExpr != "" {
			return common.UPDATE_STOP, fmt.Sprintf("Cancelled recurring schedule for %s", scheduleInfo.Name), nil
		}
		return common.UPDATE_STOP, fmt.Sprintf("Cancelled scheduled download: %s", scheduleInfo.Name), nil
	}

	// Waiting items have an allocated but not-yet-running downloader, so no
	// worker exists to emit DownloadStopped. Remove them directly and release
	// their resources without waiting for a terminal callback.
	if s.manager.GetQueue() != nil {
		waiting, closeErr := s.manager.RemoveWaitingDownloader(m.DownloadId)
		if waiting {
			finishStoppedGeneration(pool, m.DownloadId)
			return common.UPDATE_STOP, nil, closeErr
		}
	}

	if !pool.HasDownload(m.DownloadId) {
		return common.UPDATE_STOP, nil, errors.New("download not running")
	}
	if err = item.StopDownload(); err != nil {
		return common.UPDATE_STOP, nil, err
	}

	// Wait until the canceled downloader finishes broadcasting its terminal
	// state and removes itself from the pool. Without this, a subsequent
	// resume can inherit a stale DownloadStopped event from the previous run.
	deadline := time.Now().Add(5 * time.Second)
	for pool.HasDownload(m.DownloadId) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if !pool.HasDownload(m.DownloadId) {
		// DownloadStopped already released the slot through Manager's patched
		// handler and atomically removed this pool generation.
		return common.UPDATE_STOP, nil, nil
	}

	return common.UPDATE_STOP, nil, errors.New("download is still stopping")
}
