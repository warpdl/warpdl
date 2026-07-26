package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) attachHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
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
	if !pool.AddConnection(m.DownloadId, sconn) {
		return common.UPDATE_ATTACH, nil, errors.New("download not running")
	}
	maxConn, err := item.GetMaxConnections()
	if err != nil {
		return common.UPDATE_ATTACH, nil, err
	}
	maxParts, err := item.GetMaxParts()
	if err != nil {
		return common.UPDATE_ATTACH, nil, err
	}
	snapshot := item.Snapshot()
	return common.UPDATE_ATTACH, &common.DownloadResponse{
		ContentLength:     snapshot.TotalSize,
		DownloadId:        snapshot.Hash,
		FileName:          snapshot.Name,
		SavePath:          item.GetSavePath(),
		DownloadDirectory: snapshot.DownloadLocation,
		Downloaded:        snapshot.Downloaded,
		MaxConnections:    maxConn,
		MaxSegments:       maxParts,
	}, nil
}
