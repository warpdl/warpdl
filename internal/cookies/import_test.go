package cookies

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestImportCookies_Firefox(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"sid", "abc123", ".example.com", "/", futureExpiry, 1, 1},
		{"lang", "en", ".example.com", "/settings", futureExpiry, 0, 0},
	})

	cookies, source, err := ImportCookies(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}
	if source == nil {
		t.Fatal("expected non-nil source")
	}
	if source.Format != FormatFirefox {
		t.Errorf("expected FormatFirefox, got %d", source.Format)
	}
	if source.Browser != "Firefox" {
		t.Errorf("expected browser 'Firefox', got '%s'", source.Browser)
	}
	if source.Path != dbPath {
		t.Errorf("expected source path '%s', got '%s'", dbPath, source.Path)
	}
}

func TestImportCookies_Netscape(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	fpath := filepath.Join(dir, "cookies.txt")
	content := fmt.Sprintf("# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tTRUE\t%d\tsid\tabc123\n", futureExpiry)
	if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	cookies, source, err := ImportCookies(fpath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if source.Format != FormatNetscape {
		t.Errorf("expected FormatNetscape, got %d", source.Format)
	}
	if source.Browser != "Netscape" {
		t.Errorf("expected browser 'Netscape', got '%s'", source.Browser)
	}
}

func TestImportCookies_Chrome(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := unixToChrome(time.Now().Add(24 * time.Hour).Unix())
	dbPath := filepath.Join(dir, "Cookies")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
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
		t.Fatalf("failed to create table: %v", err)
	}
	_, err = db.Exec(`INSERT INTO cookies (creation_utc, host_key, name, value, path, expires_utc, is_secure, is_httponly) VALUES (0, '.example.com', 'sid', 'abc123', '/', ?, 1, 0)`, futureExpiry)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}
	db.Close()

	cookies, source, err := ImportCookies(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if source.Format != FormatChrome {
		t.Errorf("expected FormatChrome, got %d", source.Format)
	}
	if source.Browser != "Chrome" {
		t.Errorf("expected browser 'Chrome', got '%s'", source.Browser)
	}
}

func TestImportCookies_FileNotFound(t *testing.T) {
	_, _, err := ImportCookies("/nonexistent/cookies.sqlite", "example.com")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestImportCookies_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "empty")
	if err := os.WriteFile(fpath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, _, err := ImportCookies(fpath, "example.com")
	if err == nil {
		t.Fatal("expected error for empty file, got nil")
	}
}

func TestBuildCookieHeader(t *testing.T) {
	cookies := []Cookie{
		{Name: "sid", Value: "abc123"},
		{Name: "lang", Value: "en"},
		{Name: "theme", Value: "dark"},
	}

	header := BuildCookieHeader(cookies)
	if header != "sid=abc123; lang=en; theme=dark" {
		t.Errorf("unexpected header: %s", header)
	}
}

func TestBuildCookieHeader_Empty(t *testing.T) {
	header := BuildCookieHeader(nil)
	if header != "" {
		t.Errorf("expected empty header for nil cookies, got '%s'", header)
	}
}

func TestBuildCookieHeader_Single(t *testing.T) {
	cookies := []Cookie{
		{Name: "sid", Value: "abc123"},
	}

	header := BuildCookieHeader(cookies)
	if header != "sid=abc123" {
		t.Errorf("expected 'sid=abc123', got '%s'", header)
	}
}

// T075: cookie import benchmark — 10k-row SQLite fixture must complete in < 2s (SC-005)
func TestImportCookies_LargeFirefoxDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large DB test in short mode")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
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
		db.Close()
		t.Fatalf("create table: %v", err)
	}

	// Insert 10k rows in a transaction for speed
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		t.Fatalf("begin tx: %v", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO moz_cookies (name, value, host, path, expiry, isSecure, isHttpOnly) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		db.Close()
		t.Fatalf("prepare: %v", err)
	}
	for i := 0; i < 10000; i++ {
		_, err = stmt.Exec(fmt.Sprintf("cookie%d", i), fmt.Sprintf("val%d", i), ".example.com", "/", futureExpiry, 0, 0)
		if err != nil {
			stmt.Close()
			tx.Rollback()
			db.Close()
			t.Fatalf("insert row %d: %v", i, err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		db.Close()
		t.Fatalf("commit: %v", err)
	}
	db.Close()

	start := time.Now()
	cookies, _, err := ImportCookies(dbPath, "example.com")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("ImportCookies: %v", err)
	}
	if len(cookies) != 10000 {
		t.Errorf("expected 10000 cookies, got %d", len(cookies))
	}
	if elapsed > 2*time.Second {
		t.Errorf("ImportCookies took %v, want < 2s (SC-005)", elapsed)
	}
}

func TestImportCookies_EndToEnd_BuildsHeader(t *testing.T) {
	dir := t.TempDir()
	futureExpiry := time.Now().Add(24 * time.Hour).Unix()
	dbPath := createFirefoxFixture(t, dir, []firefoxRow{
		{"sid", "abc123", ".example.com", "/", futureExpiry, 1, 0},
		{"lang", "en", ".example.com", "/", futureExpiry, 0, 0},
	})

	cookies, _, err := ImportCookies(dbPath, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	header := BuildCookieHeader(cookies)
	// Should contain both cookies separated by "; "
	if !strings.Contains(header, "sid=abc123") {
		t.Errorf("header missing sid cookie: %s", header)
	}
	if !strings.Contains(header, "lang=en") {
		t.Errorf("header missing lang cookie: %s", header)
	}
}
