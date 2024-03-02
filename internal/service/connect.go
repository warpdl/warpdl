package service

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/internal/server"
)

type ConnectMessage struct {
	DownloadId string `json:"download_id"`
}

const UPDATE_CONNECT = "connect"

func (s *Service) connectHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m ConnectMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return UPDATE_CONNECT, nil, err
	}
	if m.DownloadId == "" {
		return UPDATE_CONNECT, nil, errors.New("download_id is required")
	}
	if !pool.HasDownload(m.DownloadId) {
		return UPDATE_CONNECT, nil, errors.New("download not running")
	}
	pool.AddDownload(m.DownloadId, sconn)
	return UPDATE_CONNECT, nil, nil
}
