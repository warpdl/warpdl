package service

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/internal/server"
)

const UPDATE_STOP = "stop"

type StopMessage InputDownloadId

func (s *Api) stopHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m StopMessage
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return UPDATE_STOP, nil, err
	}
	if m.DownloadId == "" {
		return UPDATE_STOP, nil, errors.New("download_id is required")
	}
	item := s.manager.GetItem(m.DownloadId)
	if item == nil {
		return UPDATE_STOP, nil, errors.New("download not found")
	}
	if !pool.HasDownload(m.DownloadId) {
		return UPDATE_STOP, nil, errors.New("download not running")
	}
	item.StopDownload()
	return UPDATE_STOP, nil, err
}
