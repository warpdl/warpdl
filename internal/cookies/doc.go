// Package cookies provides browser cookie import functionality for WarpDL.
// It supports reading cookies from Firefox (moz_cookies SQLite), Chrome (cookies SQLite,
// unencrypted only), and Netscape text format cookie files. Cookies are imported
// in-memory and injected as HTTP headers into download requests.
//
// Cookie values are never persisted or logged. Only the source path is stored
// in the Item for re-import on resume/retry (FR-023, FR-024).
package cookies
