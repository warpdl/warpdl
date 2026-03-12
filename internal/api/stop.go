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

	// Handle scheduled items: cancel the schedule instead of stopping a download.
	// T070: For recurring items (CronExpr set), print a specific cancellation message.
	if item.ScheduleState == warplib.ScheduleStateScheduled {
		item.ScheduleState = warplib.ScheduleStateCancelled
		s.manager.UpdateItem(item)
		if s.scheduler != nil {
			s.scheduler.Remove(item.Hash)
		}
		if item.CronExpr != "" {
			return common.UPDATE_STOP, fmt.Sprintf("Cancelled recurring schedule for %s", item.Name), nil
		}
		return common.UPDATE_STOP, fmt.Sprintf("Cancelled scheduled download: %s", item.Name), nil
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

	return common.UPDATE_STOP, nil, err
}
