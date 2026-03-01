package cookies

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// sqliteMagic is the first 16 bytes of any SQLite database file.
var sqliteMagic = []byte("SQLite format 3\x00")

// DetectFormat determines the cookie store format of the file at the given path.
// It returns FormatFirefox, FormatChrome, or FormatNetscape, or an error if the
// format cannot be determined.
func DetectFormat(path string) (CookieFormat, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FormatUnknown, fmt.Errorf("error: cookie file not found: %s", path)
	}
	if info.IsDir() {
		return FormatUnknown, fmt.Errorf("error: %s is a directory, expected a cookie file path or 'auto'", path)
	}
	if info.Size() == 0 {
		return FormatUnknown, fmt.Errorf("error: cookie file at %s is empty or corrupted", path)
	}

	// Read first 16 bytes to check for SQLite magic
	f, err := os.Open(path)
	if err != nil {
		return FormatUnknown, fmt.Errorf("error: cannot open cookie file: %w", err)
	}
	defer f.Close()

	header := make([]byte, 16)
	n, err := f.Read(header)
	if err != nil {
		return FormatUnknown, fmt.Errorf("error: cannot read cookie file: %w", err)
	}

	// Check if it's a SQLite file
	if n >= 16 && string(header[:16]) == string(sqliteMagic) {
		return detectSQLiteFormat(path)
	}

	// Not SQLite — check for Netscape text format
	// Re-read from start for line check
	f.Seek(0, 0)
	buf := make([]byte, 512)
	n, _ = f.Read(buf)
	firstLine := string(buf[:n])
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = strings.TrimRight(firstLine, "\r")

	if firstLine == "# Netscape HTTP Cookie File" || firstLine == "# HTTP Cookie File" {
		return FormatNetscape, nil
	}

	return FormatUnknown, fmt.Errorf("error: unsupported cookie database schema at %s", path)
}

// detectSQLiteFormat opens the SQLite file and checks which cookie table exists.
func detectSQLiteFormat(path string) (CookieFormat, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", path))
	if err != nil {
		return FormatUnknown, fmt.Errorf("error: cannot open SQLite database: %w", err)
	}
	defer db.Close()

	// Check for Firefox moz_cookies table
	var tableName string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='moz_cookies'`).Scan(&tableName)
	if err == nil {
		return FormatFirefox, nil
	}

	// Check for Chrome cookies table
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='cookies'`).Scan(&tableName)
	if err == nil {
		return FormatChrome, nil
	}

	return FormatUnknown, fmt.Errorf("error: unsupported cookie database schema at %s", path)
}
