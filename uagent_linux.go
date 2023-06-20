package main

import "github.com/warpdl/warplib"

var UserAgents = map[string]string{
	"warp":    warplib.DEF_USER_AGENT,
	"firefox": "Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/114.0",
	"chrome":  "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36",
}
