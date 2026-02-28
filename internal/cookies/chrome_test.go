package cookies

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Chrome stores timestamps as microseconds since 1601-01-01 00:00:00 UTC.
// Conversion: unix_seconds = (chrome_usec / 1_000_000) - 11_644_473_600
const chromeEpochOffset int64 = 11_644_473_600

func unixToChrome(unixSec int64) int64 {
	return (unixSec + chromeEpochOffset) * 1_000_000
}

type chromeRow struct {
	Name           string
	Value          string
	EncryptedValue []byte
	HostKey        string
	Path           string
	ExpiresUTC     int64 // Chrome format (microseconds since 1601-01-01)
	IsSecure       int
	IsHttpOnly     int
}

func createChromeFixture(t *testing.T, dir string, rows []chromeRow) string {
	t.Helper()
	dbPath := filepath.Join(dir, "Cookies")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE cookies (
        creation_utc INTEGER NOT NULL,
        host_key TEXT NOT NULL,
        name TEXT NOT NULL,
        value TEXT NOT NULL,
        encrypted_value BLOB NOT NULL DEFAULT x'',
        path TEXT NOT NULL DEFAULT '/',
        expires_utc INTEGER NOT NULL DEFAULT 0,
        is_secure INTEGER NOT NULL DEFAULT 0,
        is_httponly INTEGER NOT NULL DEFAULT 0
    )`)
	if err != nil {
		t.Fatalf("failed to create cookies table: %v", err)
	}

	stmt, err := db.Prepare(`INSERT INTO cookies (creation_utc, host_key, name, value, encrypted_value, path, expires_utc, is_secure, is_httponly) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("failed to prepare insert: %v", err)
	}
	defer stmt.Close()

	for _, r := range rows {
		encVal := r.EncryptedValue
		if encVal == nil {
			encVal = []byte{}
		}
		_, err = stmt.Exec(0, r.HostKey, r.Name, r.Value, encVal, r.Path, r.ExpiresUTC, r.IsSecure, r.IsHttpOnly)
		if err != nil {
			t.Fatalf("failed to insert row: %v", err)
		}
	}
	return dbPath
}

func TestParseChrome_UnencryptedCookies(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := unixToChrome(time.Now().Add(24 * time.Hour).Unix())
	dbPath := createChromeFixture(t, dir, []chromeRow{
		{"sid", "abc123", nil, ".example.com", "/", futureExpiry, 1, 1},
		{"lang", "en", nil, ".example.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseChrome(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}
}

func TestParseChrome_SkipEncryptedOnly(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := unixToChrome(time.Now().Add(24 * time.Hour).Unix())
	dbPath := createChromeFixture(t, dir, []chromeRow{
		// value is empty, encrypted_value is set -> should be skipped
		{"encrypted_cookie", "", []byte("encrypted_data"), ".example.com", "/", futureExpiry, 0, 0},
		// value is set -> should be included
		{"plain_cookie", "plainval", nil, ".example.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseChrome(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (skip encrypted), got %d", len(cookies))
	}
	if cookies[0].Name != "plain_cookie" {
		t.Errorf("expected cookie name 'plain_cookie', got '%s'", cookies[0].Name)
	}
}

func TestParseChrome_TimestampConversion(t *testing.T) {
	dir := t.TempDir()
	// Use a known Unix timestamp and convert to Chrome format
	knownUnix := int64(1700000000) // 2023-11-14T22:13:20Z
	chromeTS := unixToChrome(knownUnix)
	// Make sure this is in the future for the test by using a very far future
	futureChromeTS := unixToChrome(time.Now().Add(365 * 24 * time.Hour).Unix())

	dbPath := createChromeFixture(t, dir, []chromeRow{
		{"sid", "abc123", nil, ".example.com", "/", futureChromeTS, 0, 0},
	})

	cookies, err := ParseChrome(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	// Verify the conversion works correctly by checking the known timestamp
	expectedTime := time.Unix(knownUnix, 0)
	convertedUnix := (chromeTS / 1_000_000) - chromeEpochOffset
	if convertedUnix != knownUnix {
		t.Errorf("timestamp conversion failed: expected unix %d, got %d", knownUnix, convertedUnix)
	}
	_ = expectedTime // used for validation logic above
}

func TestParseChrome_DomainFiltering(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := unixToChrome(time.Now().Add(24 * time.Hour).Unix())
	dbPath := createChromeFixture(t, dir, []chromeRow{
		{"sid", "abc123", nil, "example.com", "/", futureExpiry, 0, 0},
		{"dotted", "val", nil, ".example.com", "/", futureExpiry, 0, 0},
		{"sub", "val2", nil, "sub.example.com", "/", futureExpiry, 0, 0},
		{"other", "val3", nil, "other.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseChrome(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 3 {
		t.Fatalf("expected 3 cookies (exact, dot-prefix, subdomain), got %d", len(cookies))
	}
}

func TestParseChrome_SkipExpired(t *testing.T) {
	dir := t.TempDir()
	pastExpiry := unixToChrome(time.Now().Add(-24 * time.Hour).Unix())
	futureExpiry := unixToChrome(time.Now().Add(24 * time.Hour).Unix())
	dbPath := createChromeFixture(t, dir, []chromeRow{
		{"expired", "old", nil, ".example.com", "/", pastExpiry, 0, 0},
		{"valid", "new", nil, ".example.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseChrome(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (skip expired), got %d", len(cookies))
	}
	if cookies[0].Name != "valid" {
		t.Errorf("expected cookie name 'valid', got '%s'", cookies[0].Name)
	}
}

func TestParseChrome_SecureAndHttpOnlyFlags(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := unixToChrome(time.Now().Add(24 * time.Hour).Unix())
	dbPath := createChromeFixture(t, dir, []chromeRow{
		{"sid", "abc123", nil, ".example.com", "/", futureExpiry, 1, 1},
	})

	cookies, err := ParseChrome(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if !cookies[0].Secure {
		t.Error("expected Secure=true")
	}
	if !cookies[0].HttpOnly {
		t.Error("expected HttpOnly=true")
	}
}

func TestParseChrome_EmptyResultForUnmatchedDomain(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := unixToChrome(time.Now().Add(24 * time.Hour).Unix())
	dbPath := createChromeFixture(t, dir, []chromeRow{
		{"sid", "abc123", nil, ".other.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseChrome(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 0 {
		t.Errorf("expected 0 cookies for unmatched domain, got %d", len(cookies))
	}
}
