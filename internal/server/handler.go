package server

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

// HandlerFunc defines the signature for RPC request handlers.
// It receives a synchronized connection, connection pool, and the raw JSON message body.
// It returns the update type for the response, the response payload, and any error encountered.
type HandlerFunc func(
	conn *SyncConn,
	pool *Pool,
	body json.RawMessage,
) (
	common.UpdateType,
	any,
	error,
)
