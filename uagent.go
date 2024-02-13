package main

import (
	"strings"

	"github.com/warpdl/warpdl/pkg/warplib"
)

var UserAgents = map[string]string{
	"warp":    warplib.DEF_USER_AGENT,
	"firefox": "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/114.0",
	"chrome":  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36",
}

func getUserAgent(s string) (ua string) {
	r, ok := UserAgents[strings.ToLower(s)]
	if !ok {
		ua = s
		return
	}
	ua = r
	return
}
