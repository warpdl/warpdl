package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) getExtHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.InputExtension
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_GET_EXT, nil, err
	}
	if m.ExtensionId == "" {
		return common.UPDATE_GET_EXT, nil, errors.New("extension id is required")
	}
	ext := s.elEngine.GetModule(m.ExtensionId)
	if ext == nil {
		return common.UPDATE_GET_EXT, nil, errors.New("extension not found")
	}
	return common.UPDATE_GET_EXT, &common.ExtensionInfo{
		ExtensionId: ext.ModuleId,
		Name:        ext.Name,
		Version:     ext.Version,
		Description: ext.Description,
		Matches:     ext.Matches,
	}, nil
}
