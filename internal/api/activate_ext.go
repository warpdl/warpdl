package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) activateExtHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.InputExtension
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_ACTIVATE_EXT, nil, err
	}
	if m.ExtensionId == "" {
		return common.UPDATE_ACTIVATE_EXT, nil, errors.New("extension id is required")
	}
	ext, err := s.elEngine.ActivateModule(m.ExtensionId)
	if err != nil {
		return common.UPDATE_ACTIVATE_EXT, nil, err
	}
	return common.UPDATE_ACTIVATE_EXT, &common.ExtensionInfo{
		ExtensionId: ext.ModuleId,
		Name:        ext.Name,
		Version:     ext.Version,
		Description: ext.Description,
		Matches:     ext.Matches,
	}, nil
}
