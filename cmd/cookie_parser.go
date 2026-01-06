package cmd

import (
	"fmt"
	"strings"

	"github.com/warpdl/warpdl/pkg/warplib"
)

// ParseCookieFlags converts --cookie flag values into a Cookie header.
// Input: ["session=abc", "user=xyz"]
// Output: Header{Key: "Cookie", Value: "session=abc; user=xyz"}
//
// Returns an empty Header if flags is empty.
// Returns an error if any cookie is malformed (missing '=').
func ParseCookieFlags(flags []string) (warplib.Header, error) {
	if len(flags) == 0 {
		return warplib.Header{}, nil
	}

	var cookies []string
	for _, flag := range flags {
		trimmed := strings.TrimSpace(flag)
		if trimmed == "" || !strings.Contains(trimmed, "=") {
			return warplib.Header{}, fmt.Errorf("invalid cookie format: %q (expected 'name=value')", flag)
		}
		cookies = append(cookies, trimmed)
	}

	return warplib.Header{
		Key:   "Cookie",
		Value: strings.Join(cookies, "; "),
	}, nil
}

// AppendCookieHeader parses cookie flags and appends the resulting Cookie header
// to the given headers slice. If cookies is empty, returns headers unchanged.
// Returns an error if any cookie is malformed.
func AppendCookieHeader(headers warplib.Headers, cookies []string) (warplib.Headers, error) {
	if len(cookies) == 0 {
		return headers, nil
	}

	cookieHeader, err := ParseCookieFlags(cookies)
	if err != nil {
		return nil, err
	}

	return append(headers, cookieHeader), nil
}
