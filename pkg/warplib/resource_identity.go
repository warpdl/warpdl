package warplib

import (
	"fmt"
	"net/http"
	"strings"
	"time"
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

// resourceValidator normalizes a supplied or persisted If-Range validator: a
// strong entity tag, or a Last-Modified HTTP-date. Anything else yields "".
func resourceValidator(value string) string {
	if etag := strongETag(value); etag != "" {
		return etag
	}
	return lastModifiedValidator(value)
}

// lastModifiedValidator canonicalizes an HTTP-date so validators captured
// from different responses compare equal whatever date format the server
// used.
func lastModifiedValidator(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	t, err := http.ParseTime(value)
	if err != nil {
		return ""
	}
	return t.UTC().Format(http.TimeFormat)
}

// isDateValidator reports whether validator is a Last-Modified date rather
// than an entity tag. Entity tags are always quoted.
func isDateValidator(validator string) bool {
	return validator != "" && validator[0] != '"'
}

// strongLastModified returns the response's Last-Modified date as a validator
// when RFC 9110 section 8.8.2.2 treats it as strong: the origin's Date is at
// least one second later, so a second change within the same timestamp would
// have been visible to the server when it answered.
func strongLastModified(h http.Header) string {
	lastModified, err := http.ParseTime(strings.TrimSpace(h.Get("Last-Modified")))
	if err != nil {
		return ""
	}
	date, err := http.ParseTime(strings.TrimSpace(h.Get("Date")))
	if err != nil || date.Sub(lastModified) < time.Second {
		return ""
	}
	return lastModified.UTC().Format(http.TimeFormat)
}

// responseValidator extracts the validator from h that is comparable with
// expected. A bound validator keeps its kind; otherwise a strong ETag is
// preferred and a strong Last-Modified date is the fallback, so servers that
// send no ETag can still be downloaded in parallel segments.
func responseValidator(h http.Header, expected string) string {
	if isDateValidator(expected) {
		return lastModifiedValidator(h.Get("Last-Modified"))
	}
	if etag := strongETag(h.Get("ETag")); etag != "" {
		return etag
	}
	if expected != "" {
		return ""
	}
	return strongLastModified(h)
}

func validateResourceIdentity(resp *http.Response, expected string) error {
	if expected == "" {
		return nil
	}
	if resp.StatusCode == http.StatusOK {
		// A conforming server returns the full representation when If-Range no
		// longer matches. Never write that response into a segment file.
		return fmt.Errorf("%w: server rejected If-Range validator", ErrResourceChanged)
	}
	if isDateValidator(expected) {
		return validateLastModified(resp, expected)
	}
	if raw := strings.TrimSpace(resp.Header.Get("ETag")); raw != "" {
		got := strongETag(raw)
		if got == "" || got != expected {
			return fmt.Errorf("%w: expected ETag %s, got %s",
				ErrResourceChanged, expected, raw)
		}
	} else if resp.StatusCode == http.StatusPartialContent {
		return fmt.Errorf("%w: partial response omitted the required strong ETag",
			ErrResourceChanged)
	}
	return nil
}

func validateLastModified(resp *http.Response, expected string) error {
	if raw := strings.TrimSpace(resp.Header.Get("Last-Modified")); raw != "" {
		if got := lastModifiedValidator(raw); got != expected {
			return fmt.Errorf("%w: expected Last-Modified %s, got %s",
				ErrResourceChanged, expected, raw)
		}
	} else if resp.StatusCode == http.StatusPartialContent {
		return fmt.Errorf("%w: partial response omitted the required Last-Modified",
			ErrResourceChanged)
	}
	return nil
}
