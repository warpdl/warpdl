package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

func (s *Api) loadExtHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.LoadExtensionParams
	var err error
	if err = json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_LOAD_EXT, nil, err
	}
	if m.Path == "" {
		return common.UPDATE_LOAD_EXT, nil, errors.New("extension path is required")
	}
	ext, err := s.elEngine.AddModule(m.Path)
	if err != nil {
		return common.UPDATE_LOAD_EXT, nil, err
	}
	return common.UPDATE_LOAD_EXT, &common.ExtensionInfo{
		ExtensionId: ext.ModuleId,
		Name:        ext.Name,
		Version:     ext.Version,
		Description: ext.Description,
		Matches:     ext.Matches,
	}, nil
}
