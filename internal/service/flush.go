package service

import (
	"encoding/json"

	"github.com/warpdl/warpdl/internal/server"
)

const UPDATE_FLUSH = "flush"

type FlushMessage struct {
	Hash string `json:"hash,omitempty"`
}

func (s *Service) flushHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m FlushMessage
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return UPDATE_FLUSH, nil, err
	}
	if m.Hash == "" {
		s.manager.Flush()
	} else {
		err = s.manager.FlushOne(m.Hash)
	}
	return UPDATE_FLUSH, nil, err
}
