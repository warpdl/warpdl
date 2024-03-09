package service

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"

	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) stopHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
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
	if !pool.HasDownload(m.DownloadId) {
		return common.UPDATE_STOP, nil, errors.New("download not running")
	}
	item.StopDownload()
	return common.UPDATE_STOP, nil, err
}
