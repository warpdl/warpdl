package api

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) listExtHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.ListExtensionsParams
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_LIST_EXT, nil, err
	}
	exts := s.elEngine.ListModules(m.All)
	return common.UPDATE_LIST_EXT, exts, nil
}
