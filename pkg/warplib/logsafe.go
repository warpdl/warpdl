package warplib

import "net/url"

// logSafeURL removes URL components that commonly carry credentials. It is
// only for diagnostics; the downloader continues using the original URL for
// requests. Returning a fixed marker on parse failure avoids accidentally
// logging an unparseable string that still contains a secret.
func logSafeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid URL redacted]"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

// sanitizeHTTPError removes credential-bearing URL components from the
// *url.Error values returned by net/http while preserving their concrete type,
// timeout behavior and Unwrap chain. net/http replaces URL with the raw
// redirect target when CheckRedirect rejects a response, so policy messages
// alone are not sufficient to keep signed queries out of logs and API errors.
func sanitizeHTTPError(err error) error {
	if err == nil {
		return nil
	}
	urlErr, ok := err.(*url.Error)
	if !ok {
		return err
	}
	safe := *urlErr
	safe.URL = logSafeURL(urlErr.URL)
	safe.Err = sanitizeHTTPError(urlErr.Err)
	return &safe
}
