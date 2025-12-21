// Package types defines common data structures used throughout the credman
// package for credential management.
package types

import (
	"time"
)

// Cookie represents an HTTP cookie with its associated metadata. It mirrors
// the standard http.Cookie structure but includes only the fields relevant
// for credential storage. The Value field is stored encrypted when persisted
// by the CookieManager.
type Cookie struct {
	// Name is the cookie's unique identifier.
	Name string
	// Value is the cookie's content, stored encrypted when persisted.
	Value string
	// Domain specifies the hosts to which the cookie will be sent.
	Domain string
	// Expires is the maximum lifetime of the cookie as an absolute timestamp.
	Expires time.Time
	// MaxAge specifies the number of seconds until the cookie expires.
	// A zero or negative value means the cookie should be deleted immediately.
	MaxAge int
	// HttpOnly indicates whether the cookie is accessible only through HTTP(S)
	// requests and not via client-side scripts.
	HttpOnly bool
}
