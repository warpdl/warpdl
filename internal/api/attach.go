package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) attachHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m common.InputDownloadId
	if err := json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_ATTACH, nil, err
	}
	if m.DownloadId == "" {
		return common.UPDATE_ATTACH, nil, errors.New("download_id is required")
	}
	item := s.manager.GetItem(m.DownloadId)
	if item == nil {
		return common.UPDATE_ATTACH, nil, errors.New("download not found")
	}
	if !pool.HasDownload(m.DownloadId) {
		return common.UPDATE_ATTACH, nil, errors.New("download not running")
	}
	pool.AddConnection(m.DownloadId, sconn)
	return common.UPDATE_ATTACH, &common.DownloadResponse{
		ContentLength:     item.TotalSize,
		DownloadId:        item.Hash,
		FileName:          item.Name,
		SavePath:          item.GetSavePath(),
		DownloadDirectory: item.DownloadLocation,
		Downloaded:        item.Downloaded,
	}, nil
}
