package cookies

import "time"

// CookieFormat identifies the format of a browser cookie store.
type CookieFormat int

const (
	// FormatUnknown means the cookie store format could not be detected.
	FormatUnknown CookieFormat = 0
	// FormatFirefox means the cookie store uses the Firefox moz_cookies SQLite schema.
	FormatFirefox CookieFormat = 1
	// FormatChrome means the cookie store uses the Chrome cookies SQLite schema.
	// Only unencrypted cookies (value != '') are usable (FR-014).
	FormatChrome CookieFormat = 2
	// FormatNetscape means the cookie store uses the Netscape tab-separated text format.
	FormatNetscape CookieFormat = 3
)

// Cookie represents a single HTTP cookie imported from a browser cookie store.
// IMPORTANT: Cookie values are SENSITIVE — they MUST NEVER be logged at any
// level, stored to disk, or formatted into error messages (FR-020, FR-023).
// Only Name and Domain may appear in debug logs.
type Cookie struct {
	// Name is the cookie name.
	Name string
	// Value is the cookie value. SENSITIVE — never log.
	Value string
	// Domain is the cookie domain (may have leading dot for subdomain-inclusive cookies).
	Domain string
	// Path is the cookie path scope.
	Path string
	// Expiry is the cookie expiration time.
	Expiry time.Time
	// Secure indicates the cookie should only be sent over HTTPS.
	Secure bool
	// HttpOnly indicates the cookie is not accessible via JavaScript.
	HttpOnly bool
}

// CookieSource describes where cookies were imported from.
// Used for logging and display in warpdl list (browser name only, not full path).
type CookieSource struct {
	// Path is the filesystem path to the cookie store file.
	// Displayed only in debug mode (CHK038).
	Path string
	// Format is the detected cookie store format.
	Format CookieFormat
	// Browser is the detected browser name (e.g., "Firefox", "Chrome", "Netscape").
	Browser string
}
