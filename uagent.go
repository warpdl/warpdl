package main

import (
	"strings"
)

func getUserAgent(s string) (ua string) {
	r, ok := UserAgents[strings.ToLower(s)]
	if !ok {
		ua = s
		return
	}
	ua = r
	return
}
