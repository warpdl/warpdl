package service

import (
	"encoding/json"

	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

const UPDATE_LIST = "list"

type ListMessage struct {
	ShowCompleted bool `json:"show_completed"`
	ShowPending   bool `json:"show_pending"`
}

type ListResponse struct {
	Items []*warplib.Item `json:"items"`
}

func (s *Api) listHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m ListMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return UPDATE_LIST, nil, err
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
	return UPDATE_LIST, &ListResponse{
		Items: items,
	}, nil
}
