package api

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) flushHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.FlushParams
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_FLUSH, nil, err
	}
	if m.DownloadId == "" {
		s.manager.Flush()
	} else {
		err = s.manager.FlushOne(m.DownloadId)
	}
	return common.UPDATE_FLUSH, nil, err
}
