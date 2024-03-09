package service

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/internal/server"
)

type AttachMessage InputDownloadId

const UPDATE_ATTACH = "attach"

func (s *Service) attachHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m AttachMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return UPDATE_ATTACH, nil, err
	}
	if m.DownloadId == "" {
		return UPDATE_ATTACH, nil, errors.New("download_id is required")
	}
	item := s.manager.GetItem(m.DownloadId)
	if item == nil {
		return UPDATE_ATTACH, nil, errors.New("download not found")
	}
	if !pool.HasDownload(m.DownloadId) {
		return UPDATE_ATTACH, nil, errors.New("download not running")
	}
	pool.AddConnection(m.DownloadId, sconn)
	return UPDATE_ATTACH, &DownloadResponse{
		ContentLength:     item.TotalSize,
		DownloadId:        item.Hash,
		FileName:          item.Name,
		SavePath:          item.GetSavePath(),
		DownloadDirectory: item.DownloadLocation,
		Downloaded:        item.Downloaded,
	}, nil
}
