package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) deleteExtHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.InputExtension
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_DELETE_EXT, nil, err
	}
	if m.ExtensionId == "" {
		return common.UPDATE_DELETE_EXT, nil, errors.New("extension id is required")
	}
	extName, err := s.elEngine.DeleteModule(m.ExtensionId)
	return common.UPDATE_DELETE_EXT, &common.ExtensionName{Name: extName}, err
}
