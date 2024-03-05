package warpcli

import "encoding/json"

type Handler interface {
	Handle(json.RawMessage) error
}

func NewDownloadingHandler(action string, callback func(*DownloadingResponse) error) *DownloadingHandler {
	return &DownloadingHandler{
		Action:   action,
		Callback: callback,
	}
}

type DownloadingHandler struct {
	Action   string
	Callback func(*DownloadingResponse) error
}

func (h *DownloadingHandler) Handle(m json.RawMessage) error {
	var v DownloadingResponse
	err := json.Unmarshal(m, &v)
	if err != nil {
		return err
	}
	if h.Action != "" && v.Action != h.Action {
		return nil
	}
	return h.Callback(&v)
}
