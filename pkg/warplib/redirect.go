package warplib

import (
  "errors"
  "fmt"
  "net/http"
  "net/url"
)

const (
  // DefaultMaxRedirects is the maximum number of redirect hops allowed.
  // Matches Go's default http.Client behavior.
  DefaultMaxRedirects = 10
)

var (
  // ErrTooManyRedirects is returned when a redirect chain exceeds the configured max hops.
  ErrTooManyRedirects = errors.New("redirect loop detected")

  // ErrCrossProtocolRedirect is returned when a redirect crosses from HTTP/HTTPS
  // to a non-HTTP protocol (e.g., FTP).
  ErrCrossProtocolRedirect = errors.New("cross-protocol redirect not supported")
)

// isHTTPScheme returns true if the scheme is http or https.
func isHTTPScheme(scheme string) bool {
  return scheme == "http" || scheme == "https"
}

// isCrossOrigin returns true if two URLs have different hosts.
// Host includes port if specified (e.g., "example.com:8080").
func isCrossOrigin(a, b *url.URL) bool {
  return a.Host != b.Host
}

// RedirectPolicy returns a CheckRedirect function that:
// 1. Enforces a maximum number of redirect hops
// 2. Rejects cross-protocol redirects (HTTP/HTTPS -> non-HTTP)
// 3. Strips sensitive/custom headers on cross-origin redirects
//
// For cross-origin header stripping, Go 1.24+ already strips the Authorization
// header automatically (CVE-2024-45336 fix). This function additionally strips
// custom user headers while preserving safe standard headers.
func RedirectPolicy(maxRedirects int) func(*http.Request, []*http.Request) error {
  return func(req *http.Request, via []*http.Request) error {
    if len(via) >= maxRedirects {
      lastURL := via[len(via)-1].URL.String()
      return fmt.Errorf("%w: exceeded %d hops (last URL: %s)",
        ErrTooManyRedirects, maxRedirects, lastURL)
    }

    if len(via) > 0 {
      prev := via[len(via)-1]

      // Reject cross-protocol redirects
      if isHTTPScheme(prev.URL.Scheme) && !isHTTPScheme(req.URL.Scheme) {
        return fmt.Errorf("%w: %s -> %s",
          ErrCrossProtocolRedirect, prev.URL.Scheme, req.URL.Scheme)
      }

      // Strip custom headers on cross-origin redirects
      if isCrossOrigin(prev.URL, req.URL) {
        stripUnsafeHeaders(req)
      }
    }

    return nil
  }
}

// safeHeaders are headers that should be preserved on cross-origin redirects.
// These are standard headers that don't carry sensitive information.
var safeHeaders = map[string]bool{
  "User-Agent":      true,
  "Accept":          true,
  "Accept-Language": true,
  "Accept-Encoding": true,
  "Range":           true, // Required for segment downloads
}

// stripUnsafeHeaders removes all non-safe headers from the request.
// This is called on cross-origin redirects to prevent credential/token leakage.
// Note: Go 1.24+ already handles Authorization header stripping (CVE-2024-45336),
// but we strip custom user headers too.
func stripUnsafeHeaders(req *http.Request) {
  for key := range req.Header {
    if !safeHeaders[http.CanonicalHeaderKey(key)] {
      req.Header.Del(key)
    }
  }
}
