package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
)

// authCancelHandler aborts an in-flight auth flow so any goroutine
// waiting on Await (typically a plugin's Token() call) wakes up with
// an error rather than blocking forever.
//
// Cancelling an unknown or already-resolved flow is not an error from
// the user's perspective — the end state (flow is not running) is the
// same — so we only surface the lookup error when the flow id has
// never been seen.
func (s *Api) authCancelHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var p common.AuthCancelParams
	if err := json.Unmarshal(body, &p); err != nil {
		return common.UPDATE_AUTH_FAILED, nil, err
	}
	if p.FlowID == "" {
		return common.UPDATE_AUTH_FAILED, nil, errors.New("flow_id required")
	}

	_, prov, err := s.lookupFlow(p.FlowID)
	if err != nil {
		return common.UPDATE_AUTH_FAILED, nil, err
	}
	prov.FlowRegistry().Cancel(p.FlowID, errors.New("cancelled"))
	return common.UPDATE_AUTH_FAILED, nil, nil
}
