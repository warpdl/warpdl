package api

import (
	"encoding/json"
	"errors"
	"fmt"

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
	item.StopDownload()
	return common.UPDATE_STOP, nil, err
}
