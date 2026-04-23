package warplib

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// pluginHeaderCtxKey is the request context key used to carry the set
// of plugin-supplied header names into the RedirectPolicy's CheckRedirect
// callback. This lets a per-download strip set be applied even though
// the underlying http.Client (and therefore CheckRedirect) is shared
// across downloads.
type pluginHeaderCtxKey struct{}

// WithPluginHeaderNames returns a copy of ctx carrying the given set of
// plugin-supplied header names. RedirectPolicy uses this to strip
// plugin headers on cross-origin redirects in addition to the standard
// unsafe-header list.
func WithPluginHeaderNames(ctx context.Context, names map[string]struct{}) context.Context {
	if len(names) == 0 {
		return ctx
	}
	return context.WithValue(ctx, pluginHeaderCtxKey{}, names)
}

// pluginHeaderNamesFromCtx returns the plugin header name set previously
// attached via WithPluginHeaderNames, or nil if none is present.
func pluginHeaderNamesFromCtx(ctx context.Context) map[string]struct{} {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(pluginHeaderCtxKey{}).(map[string]struct{})
	return v
}

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
				// Plugin-supplied header names travel via the request
				// context so we can also remove plugin headers that
				// happen to appear in the "safe" list (e.g. a plugin
				// that injects a User-Agent must not leak it to a
				// third-party redirect target).
				if names := pluginHeaderNamesFromCtx(req.Context()); len(names) > 0 {
					stripPluginHeaders(req, names)
				}
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

// stripPluginHeaders removes plugin-supplied headers from a request.
// Intended to be called on cross-origin redirects in addition to
// stripUnsafeHeaders: a plugin that injected a "safe" header (User-Agent,
// Accept) still shouldn't have that value forwarded to a redirect target
// the plugin never anticipated.
func stripPluginHeaders(req *http.Request, names map[string]struct{}) {
	if len(names) == 0 {
		return
	}
	for key := range req.Header {
		if _, ok := names[http.CanonicalHeaderKey(key)]; ok {
			req.Header.Del(key)
		}
	}
}

// StripUnsafeFromHeaders removes non-safe headers from a Headers slice.
// This is called when a cross-origin redirect is detected and d.headers
// needs to be cleaned so that subsequent requests (prepareDownloader,
// segment downloads) to the new origin don't leak credentials.
// Returns a new Headers slice containing only safe headers.
func StripUnsafeFromHeaders(hdrs Headers) Headers {
	return StripUnsafeFromHeadersCrossOrigin(hdrs, nil)
}

// StripUnsafeFromHeadersCrossOrigin removes non-safe headers AND any
// plugin-supplied header names from hdrs. Keys in extraNames are
// canonicalized (http.CanonicalHeaderKey) and matched case-insensitively.
//
// Plugin-supplied headers (e.g. an OAuth Authorization token a plugin
// injected) must be stripped on cross-origin redirects even if they
// appear in the standard safeHeaders list — otherwise a plugin-supplied
// User-Agent containing a token pattern or a Referer carrying a CSRF
// token would leak to an unanticipated origin.
//
// Returns a new Headers slice; the input is not mutated.
func StripUnsafeFromHeadersCrossOrigin(hdrs Headers, extraNames map[string]struct{}) Headers {
	out := make(Headers, 0, len(hdrs))
	for _, h := range hdrs {
		canon := http.CanonicalHeaderKey(h.Key)
		if _, isPlugin := extraNames[canon]; isPlugin {
			continue
		}
		if !safeHeaders[canon] {
			continue
		}
		out = append(out, h)
	}
	return out
}

// buildPluginHeaderSet returns a set of canonical header names for the
// given plugin-supplied headers. Returns nil when hdrs is empty so that
// the nil check in strip functions is cheap.
func buildPluginHeaderSet(hdrs Headers) map[string]struct{} {
	if len(hdrs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(hdrs))
	for _, h := range hdrs {
		if h.Key == "" {
			continue
		}
		set[http.CanonicalHeaderKey(h.Key)] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// mergePluginHeaders appends plugin-supplied headers into target, using
// the same last-write-wins semantics as Headers.Update so plugin headers
// override any user-provided entries with the same key. target must not
// be nil.
func mergePluginHeaders(target *Headers, plugin Headers) {
	if len(plugin) == 0 {
		return
	}
	for _, h := range plugin {
		if h.Key == "" {
			continue
		}
		target.Update(h.Key, h.Value)
	}
}
