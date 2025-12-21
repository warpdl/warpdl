package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

// Handler defines the interface for processing daemon updates.
// Implementations receive raw JSON messages and are responsible for
// unmarshaling and processing them appropriately.
type Handler interface {
	Handle(json.RawMessage) error
}

// NewDownloadingHandler creates a new handler for download progress updates.
// The action parameter filters updates to only those matching the specified
// downloading action; pass an empty string to receive all actions.
// The callback is invoked for each matching update.
func NewDownloadingHandler(action common.DownloadingAction, callback func(*common.DownloadingResponse) error) *DownloadingHandler {
	return &DownloadingHandler{
		Action:   action,
		Callback: callback,
	}
}

// DownloadingHandler processes download progress updates from the daemon.
// It filters updates by action type and invokes a callback for matching updates.
type DownloadingHandler struct {
	Action   common.DownloadingAction
	Callback func(*common.DownloadingResponse) error
}

// Handle processes a raw JSON download progress message.
// It unmarshals the message, checks if it matches the configured action filter,
// and invokes the callback if applicable. Returns an error if unmarshaling fails
// or if the callback returns an error.
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
