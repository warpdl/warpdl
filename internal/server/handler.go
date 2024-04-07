package server

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

type HandlerFunc func(
	conn *SyncConn,
	pool *Pool,
	body json.RawMessage,
) (
	common.UpdateType,
	any,
	error,
)
