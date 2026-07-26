package warplib

import (
	"fmt"
	"net/http"
	"strings"
)

// strongETag returns an RFC-style strong entity tag or an empty string when a
// response provides no usable representation validator. Weak validators are
// intentionally excluded because If-Range requires strong comparison.
func strongETag(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 || strings.HasPrefix(strings.ToLower(value), "w/") {
		return ""
	}
	if value[0] != '"' || value[len(value)-1] != '"' {
		return ""
	}
	return value
}

func validateResourceIdentity(resp *http.Response, expectedETag string) error {
	if expectedETag == "" {
		return nil
	}
	if resp.StatusCode == http.StatusOK {
		// A conforming server returns the full representation when If-Range no
		// longer matches. Never write that response into a segment file.
		return fmt.Errorf("%w: server rejected If-Range validator", ErrResourceChanged)
	}
	if raw := strings.TrimSpace(resp.Header.Get("ETag")); raw != "" {
		got := strongETag(raw)
		if got == "" || got != expectedETag {
			return fmt.Errorf("%w: expected ETag %s, got %s",
				ErrResourceChanged, expectedETag, raw)
		}
	} else if resp.StatusCode == http.StatusPartialContent {
		return fmt.Errorf("%w: partial response omitted the required strong ETag",
			ErrResourceChanged)
	}
	return nil
}
