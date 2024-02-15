package server

import (
	"encoding/json"
	"net"
)

type HandlerFunc func(
	conn net.Conn,
	pool *Pool,
	body json.RawMessage,
) (
	any,
	error,
)
