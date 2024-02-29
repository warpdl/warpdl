package server

import (
	"encoding/json"
)

type HandlerFunc func(
	conn *SyncConn,
	pool *Pool,
	body json.RawMessage,
) (
	string,
	any,
	error,
)
