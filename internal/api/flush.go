package service

import (
	"encoding/json"

	"github.com/warpdl/warpdl/internal/server"
)

const UPDATE_FLUSH = "flush"

type FlushMessage struct {
	DownloadId string `json:"download_id,omitempty"`
}

func (s *Api) flushHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m FlushMessage
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return UPDATE_FLUSH, nil, err
	}
	if m.DownloadId == "" {
		s.manager.Flush()
	} else {
		err = s.manager.FlushOne(m.DownloadId)
	}
	return UPDATE_FLUSH, nil, err
}
