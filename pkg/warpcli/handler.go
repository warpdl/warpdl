package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

type Handler interface {
	Handle(json.RawMessage) error
}

func NewDownloadingHandler(action string, callback func(*common.DownloadingResponse) error) *DownloadingHandler {
	return &DownloadingHandler{
		Action:   action,
		Callback: callback,
	}
}

type DownloadingHandler struct {
	Action   string
	Callback func(*common.DownloadingResponse) error
}

func (h *DownloadingHandler) Handle(m json.RawMessage) error {
	var v common.DownloadingResponse
	err := json.Unmarshal(m, &v)
	if err != nil {
		return err
	}
	if h.Action != "" && v.Action != h.Action {
		return nil
	}
	return h.Callback(&v)
}
