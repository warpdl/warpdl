package types

import (
	"time"
)

type Cookie struct {
	Name     string
	Value    string
	Domain   string
	Expires  time.Time
	MaxAge   int
	HttpOnly bool
}

