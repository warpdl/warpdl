package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) deactivateExtHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.InputExtension
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_DEACTIVATE_EXT, nil, err
	}
	if m.ExtensionId == "" {
		return common.UPDATE_DEACTIVATE_EXT, nil, errors.New("extension id is required")
	}
	err = s.elEngine.DeactiveModule(m.ExtensionId)
	return common.UPDATE_DEACTIVATE_EXT, nil, err
}
