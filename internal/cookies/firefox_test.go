package cookies

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// createFirefoxFixture creates a temp SQLite file with the moz_cookies schema
// and returns the file path. Caller is responsible for cleanup via t.TempDir().
func createFirefoxFixture(t *testing.T, dir string, rows []firefoxRow) string {
	t.Helper()
	dbPath := filepath.Join(dir, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE moz_cookies (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        value TEXT NOT NULL,
        host TEXT NOT NULL,
        path TEXT NOT NULL DEFAULT '/',
        expiry INTEGER NOT NULL DEFAULT 0,
        isSecure INTEGER NOT NULL DEFAULT 0,
        isHttpOnly INTEGER NOT NULL DEFAULT 0
    )`)
	if err != nil {
		t.Fatalf("failed to create moz_cookies table: %v", err)
	}

	stmt, err := db.Prepare(`INSERT INTO moz_cookies (name, value, host, path, expiry, isSecure, isHttpOnly) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("failed to prepare insert: %v", err)
	}
	defer stmt.Close()

	for _, r := range rows {
		_, err = stmt.Exec(r.Name, r.Value, r.Host, r.Path, r.Expiry, r.IsSecure, r.IsHttpOnly)
		if err != nil {
			t.Fatalf("failed to insert row: %v", err)
		}
	}
	return dbPath
}

type firefoxRow struct {
	Name       string
	Value      string
	Host       string
	Path       string
	Expiry     int64
	IsSecure   int
	IsHttpOnly int
}

func TestParseFirefox_BasicParse(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"sid", "abc123", ".example.com", "/", futureExpiry, 1, 1},
		{"lang", "en", ".example.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseFirefox(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}
}

func TestParseFirefox_DomainFiltering_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"sid", "abc123", "example.com", "/", futureExpiry, 0, 0},
		{"other", "val", "other.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseFirefox(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].Name != "sid" {
		t.Errorf("expected cookie name 'sid', got '%s'", cookies[0].Name)
	}
}

func TestParseFirefox_DomainFiltering_DotPrefix(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"sid", "abc123", ".example.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseFirefox(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (dot-prefix match), got %d", len(cookies))
	}
}

func TestParseFirefox_DomainFiltering_SubdomainWildcard(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"sid", "abc123", "sub.example.com", "/", futureExpiry, 0, 0},
		{"other", "val", "unrelated.org", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseFirefox(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (subdomain match), got %d", len(cookies))
	}
	if cookies[0].Name != "sid" {
		t.Errorf("expected cookie name 'sid', got '%s'", cookies[0].Name)
	}
}

func TestParseFirefox_SkipExpiredCookies(t *testing.T) {
	dir := t.TempDir()
	pastExpiry := time.Now().Add(-24 * time.Hour).Unix()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"expired", "old", ".example.com", "/", pastExpiry, 0, 0},
		{"valid", "new", ".example.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseFirefox(dbPath, "example.com")
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

func TestParseFirefox_EmptyResultForUnmatchedDomain(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"sid", "abc123", ".other.com", "/", futureExpiry, 0, 0},
	})

	cookies, err := ParseFirefox(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 0 {
		t.Errorf("expected 0 cookies for unmatched domain, got %d", len(cookies))
	}
}

func TestParseFirefox_SecureAndHttpOnlyFlags(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"sid", "abc123", ".example.com", "/", futureExpiry, 1, 1},
	})

	cookies, err := ParseFirefox(dbPath, "example.com")
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

func TestParseFirefox_FileNotFound(t *testing.T) {
	_, err := ParseFirefox("/nonexistent/cookies.sqlite", "example.com")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestParseFirefox_EmptyDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE moz_cookies (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        value TEXT NOT NULL,
        host TEXT NOT NULL,
        path TEXT NOT NULL DEFAULT '/',
        expiry INTEGER NOT NULL DEFAULT 0,
        isSecure INTEGER NOT NULL DEFAULT 0,
        isHttpOnly INTEGER NOT NULL DEFAULT 0
    )`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	db.Close()

	// Ensure the file is actually on disk
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("db file was not created at %s", dbPath)
	}

	cookies, err := ParseFirefox(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 0 {
		t.Errorf("expected 0 cookies from empty db, got %d", len(cookies))
	}
}
