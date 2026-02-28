package cookies

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// chromeEpochOffsetSeconds is the number of seconds between the Windows NT epoch
// (1601-01-01 00:00:00 UTC) and the Unix epoch (1970-01-01 00:00:00 UTC).
const chromeEpochOffsetSeconds int64 = 11_644_473_600

// chromeToUnix converts a Chrome timestamp (microseconds since 1601-01-01)
// to a Unix timestamp (seconds since 1970-01-01).
func chromeToUnix(chromeUSec int64) int64 {
	return (chromeUSec / 1_000_000) - chromeEpochOffsetSeconds
}

// ParseChrome reads cookies from a Chrome Cookies SQLite file for the given domain.
// Only unencrypted cookies (where value != ”) are returned. Encrypted cookies are skipped.
// The dbPath should be a path to a copied (not in-use) SQLite database.
func ParseChrome(dbPath string, domain string) ([]Cookie, error) {
	dsn := fmt.Sprintf("file:%s?immutable=1", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("error: cannot open Chrome cookie database: %w", err)
	}
	defer db.Close()

	now := time.Now().Unix()
	dotDomain := "." + domain
	wildcardDomain := "%." + domain
	// Convert current time to Chrome format for comparison
	nowChrome := (now + chromeEpochOffsetSeconds) * 1_000_000

	rows, err := db.Query(`
        SELECT name, value, host_key, path, expires_utc, is_secure, is_httponly
        FROM cookies
        WHERE (host_key = ? OR host_key = ? OR host_key LIKE ?)
          AND value != ''
          AND expires_utc > ?
        ORDER BY path DESC, name ASC
    `, domain, dotDomain, wildcardDomain, nowChrome)
	if err != nil {
		return nil, fmt.Errorf("error: failed to query Chrome cookies: %w", err)
	}
	defer rows.Close()

	var cookies []Cookie
	for rows.Next() {
		var (
			name, value, hostKey, path string
			expiresUTC                 int64
			isSecure, isHttpOnly       int
		)
		if err := rows.Scan(&name, &value, &hostKey, &path, &expiresUTC, &isSecure, &isHttpOnly); err != nil {
			return nil, fmt.Errorf("error: failed to scan Chrome cookie row: %w", err)
		}
		unixExpiry := chromeToUnix(expiresUTC)
		cookies = append(cookies, Cookie{
			Name:     name,
			Value:    value,
			Domain:   hostKey,
			Path:     path,
			Expiry:   time.Unix(unixExpiry, 0),
			Secure:   isSecure != 0,
			HttpOnly: isHttpOnly != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error: failed to iterate Chrome cookie rows: %w", err)
	}

	return cookies, nil
}
