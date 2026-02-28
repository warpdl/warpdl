package cookies

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ParseFirefox reads cookies from a Firefox cookies.sqlite file for the given domain.
// The dbPath should be a path to a copied (not in-use) SQLite database.
// Expired cookies are skipped.
func ParseFirefox(dbPath string, domain string) ([]Cookie, error) {
	dsn := fmt.Sprintf("file:%s?immutable=1", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("error: cannot open Firefox cookie database: %w", err)
	}
	defer db.Close()

	now := time.Now().Unix()
	dotDomain := "." + domain
	wildcardDomain := "%." + domain

	rows, err := db.Query(`
        SELECT name, value, host, path, expiry, isSecure, isHttpOnly
        FROM moz_cookies
        WHERE (host = ? OR host = ? OR host LIKE ?)
          AND expiry > ?
        ORDER BY path DESC, name ASC
    `, domain, dotDomain, wildcardDomain, now)
	if err != nil {
		return nil, fmt.Errorf("error: failed to query Firefox cookies: %w", err)
	}
	defer rows.Close()

	var cookies []Cookie
	for rows.Next() {
		var (
			name, value, host, path string
			expiry                  int64
			isSecure, isHttpOnly    int
		)
		if err := rows.Scan(&name, &value, &host, &path, &expiry, &isSecure, &isHttpOnly); err != nil {
			return nil, fmt.Errorf("error: failed to scan Firefox cookie row: %w", err)
		}
		cookies = append(cookies, Cookie{
			Name:     name,
			Value:    value,
			Domain:   host,
			Path:     path,
			Expiry:   time.Unix(expiry, 0),
			Secure:   isSecure != 0,
			HttpOnly: isHttpOnly != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error: failed to iterate Firefox cookie rows: %w", err)
	}

	return cookies, nil
}
