package service

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func (s *Api) listHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m common.ListParams
	if err := json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_LIST, nil, err
	}
	var items []*warplib.Item
	switch {
	case m.ShowCompleted && m.ShowPending:
		items = s.manager.GetItems()
	case m.ShowCompleted:
		items = s.manager.GetCompletedItems()
	default:
		items = s.manager.GetIncompleteItems()
	}
	return common.UPDATE_LIST, &common.ListResponse{
		Items: items,
	}, nil
}
