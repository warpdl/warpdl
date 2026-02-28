package cookies

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDetectFormat_FirefoxSQLite(t *testing.T) {
	// Create a temp SQLite file with moz_cookies table
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE moz_cookies (
        id INTEGER PRIMARY KEY,
        name TEXT,
        value TEXT,
        host TEXT,
        path TEXT,
        expiry INTEGER,
        isSecure INTEGER,
        isHttpOnly INTEGER
    )`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	db.Close()

	format, err := DetectFormat(dbPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatFirefox {
		t.Errorf("expected FormatFirefox (%d), got %d", FormatFirefox, format)
	}
}

func TestDetectFormat_ChromeSQLite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "Cookies")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE cookies (
        creation_utc INTEGER,
        host_key TEXT,
        name TEXT,
        value TEXT,
        encrypted_value BLOB,
        path TEXT,
        expires_utc INTEGER,
        is_secure INTEGER,
        is_httponly INTEGER
    )`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	db.Close()

	format, err := DetectFormat(dbPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatChrome {
		t.Errorf("expected FormatChrome (%d), got %d", FormatChrome, format)
	}
}

func TestDetectFormat_Netscape(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "cookies.txt")
	content := "# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tFALSE\t0\tsid\tabc123\n"
	if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	format, err := DetectFormat(fpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatNetscape {
		t.Errorf("expected FormatNetscape (%d), got %d", FormatNetscape, format)
	}
}

func TestDetectFormat_NetscapeAltHeader(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "cookies.txt")
	content := "# HTTP Cookie File\n.example.com\tTRUE\t/\tFALSE\t0\tsid\tabc123\n"
	if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	format, err := DetectFormat(fpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != FormatNetscape {
		t.Errorf("expected FormatNetscape (%d), got %d", FormatNetscape, format)
	}
}

func TestDetectFormat_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "empty")
	if err := os.WriteFile(fpath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := DetectFormat(fpath)
	if err == nil {
		t.Fatal("expected error for empty file, got nil")
	}
}

func TestDetectFormat_UnknownFormat(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "random.bin")
	if err := os.WriteFile(fpath, []byte("this is not a cookie file at all"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := DetectFormat(fpath)
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
}

func TestDetectFormat_FileNotFound(t *testing.T) {
	_, err := DetectFormat("/nonexistent/path/cookies.sqlite")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestDetectFormat_SQLiteUnknownSchema(t *testing.T) {
	// SQLite file but with neither moz_cookies nor cookies table
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "unknown.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE some_other_table (id INTEGER PRIMARY KEY, data TEXT)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	db.Close()

	_, err = DetectFormat(dbPath)
	if err == nil {
		t.Fatal("expected error for unsupported schema, got nil")
	}
}

// --- T056: DetectBrowserCookies integration tests ---

// makeFirefoxDB creates a minimal Firefox cookies.sqlite in the given directory.
func makeFirefoxDB(t *testing.T, dir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("makeFirefoxDB: open: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE moz_cookies (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL DEFAULT '',
        value TEXT NOT NULL DEFAULT '',
        host TEXT NOT NULL DEFAULT '',
        path TEXT NOT NULL DEFAULT '/',
        expiry INTEGER NOT NULL DEFAULT 0,
        isSecure INTEGER NOT NULL DEFAULT 0,
        isHttpOnly INTEGER NOT NULL DEFAULT 0,
        baseDomain TEXT NOT NULL DEFAULT '',
        originAttributes TEXT NOT NULL DEFAULT '',
        sameSite INTEGER NOT NULL DEFAULT 0,
        rawSameSite INTEGER NOT NULL DEFAULT 0,
        schemeMap INTEGER NOT NULL DEFAULT 0
    )`)
	if err != nil {
		t.Fatalf("makeFirefoxDB: create table: %v", err)
	}
	db.Close()
	return dbPath
}

// makeChromeDB creates a minimal Chrome Cookies SQLite in the given directory.
func makeChromeDB(t *testing.T, dir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "Cookies")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("makeChromeDB: open: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE cookies (
        creation_utc INTEGER NOT NULL,
        host_key TEXT NOT NULL,
        name TEXT NOT NULL,
        value TEXT NOT NULL,
        encrypted_value BLOB NOT NULL DEFAULT '',
        path TEXT NOT NULL,
        expires_utc INTEGER NOT NULL,
        is_secure INTEGER NOT NULL,
        is_httponly INTEGER NOT NULL
    )`)
	if err != nil {
		t.Fatalf("makeChromeDB: create table: %v", err)
	}
	db.Close()
	return dbPath
}

// TestDetectBrowserCookies_NoFirefox verifies that when no browser cookie files
// exist, DetectBrowserCookies returns a descriptive error.
func TestDetectBrowserCookies_NoFirefox(t *testing.T) {
	// Pass an empty spec list — no browsers configured.
	_, _, err := detectWithSpecs("example.com", []browserSpec{})
	if err == nil {
		t.Fatal("expected error when no browser specs provided, got nil")
	}
}

// TestDetectBrowserCookies_NoBrowserFiles verifies that when browser specs are
// given but no files exist on disk, an error is returned.
func TestDetectBrowserCookies_NoBrowserFiles(t *testing.T) {
	specs := []browserSpec{
		{
			Name:             "Firefox",
			ProfilesIniPaths: []string{"/nonexistent/firefox/profiles.ini"},
		},
		{
			Name:        "Chrome",
			CookiePaths: []string{"/nonexistent/chrome/Default/Cookies"},
		},
	}

	_, _, err := detectWithSpecs("example.com", specs)
	if err == nil {
		t.Fatal("expected error when no cookie files exist on disk, got nil")
	}
}

// TestDetectBrowserCookies_PriorityOrder verifies that when both Firefox
// (via profiles.ini) and Chrome cookie files exist, Firefox is selected first.
func TestDetectBrowserCookies_PriorityOrder(t *testing.T) {
	dir := t.TempDir()

	// Set up Firefox profile via profiles.ini
	ffProfileDir := filepath.Join(dir, "ff-profiles", "abc.default")
	if err := os.MkdirAll(ffProfileDir, 0755); err != nil {
		t.Fatalf("mkdir firefox profile: %v", err)
	}
	makeFirefoxDB(t, ffProfileDir)

	ffIniDir := filepath.Join(dir, "ff-ini")
	if err := os.MkdirAll(ffIniDir, 0755); err != nil {
		t.Fatalf("mkdir firefox ini: %v", err)
	}
	// profiles.ini path is relative to its own directory — use absolute path trick
	// by making profiles.ini reference an absolute path via the InstallXXX section
	// Actually: parseProfilesIni joins iniDir + relative path, so we need relative path.
	// ffProfileDir relative to ffIniDir:
	relPath, err := filepath.Rel(ffIniDir, ffProfileDir)
	if err != nil {
		t.Fatalf("rel path: %v", err)
	}
	iniContent := "[InstallTEST]\nDefault=" + filepath.ToSlash(relPath) + "\n"
	iniPath := filepath.Join(ffIniDir, "profiles.ini")
	if err := os.WriteFile(iniPath, []byte(iniContent), 0644); err != nil {
		t.Fatalf("write profiles.ini: %v", err)
	}

	// Set up Chrome cookie file
	chromeDir := filepath.Join(dir, "chrome-default")
	if err := os.MkdirAll(chromeDir, 0755); err != nil {
		t.Fatalf("mkdir chrome: %v", err)
	}
	chromeCookiePath := makeChromeDB(t, chromeDir)

	specs := []browserSpec{
		{
			Name:             "Firefox",
			ProfilesIniPaths: []string{iniPath},
		},
		{
			Name:        "Chrome",
			CookiePaths: []string{chromeCookiePath},
		},
	}

	_, source, err := detectWithSpecs("example.com", specs)
	if err != nil {
		t.Fatalf("detectWithSpecs: unexpected error: %v", err)
	}
	if source.Browser != "Firefox" {
		t.Errorf("priority order: want Firefox selected first, got %q", source.Browser)
	}
}

// TestDetectBrowserCookies_FallsBackToChrome verifies that when Firefox is not
// available but Chrome is, Chrome is selected.
func TestDetectBrowserCookies_FallsBackToChrome(t *testing.T) {
	dir := t.TempDir()

	chromeDir := filepath.Join(dir, "chrome-default")
	if err := os.MkdirAll(chromeDir, 0755); err != nil {
		t.Fatalf("mkdir chrome: %v", err)
	}
	chromeCookiePath := makeChromeDB(t, chromeDir)

	specs := []browserSpec{
		{
			Name:             "Firefox",
			ProfilesIniPaths: []string{"/nonexistent/firefox/profiles.ini"},
		},
		{
			Name:        "Chrome",
			CookiePaths: []string{chromeCookiePath},
		},
	}

	_, source, err := detectWithSpecs("example.com", specs)
	if err != nil {
		t.Fatalf("detectWithSpecs: unexpected error: %v", err)
	}
	if source.Browser != "Chrome" {
		t.Errorf("fallback: want Chrome, got %q", source.Browser)
	}
}

// TestDetectBrowserCookies_ChromeNetworkCookiesTakenFirst verifies that
// Network/Cookies is checked before Cookies when both exist for Chrome.
func TestDetectBrowserCookies_ChromeNetworkCookiesTakenFirst(t *testing.T) {
	dir := t.TempDir()

	// Create both Network/Cookies and Cookies files
	networkDir := filepath.Join(dir, "Network")
	if err := os.MkdirAll(networkDir, 0755); err != nil {
		t.Fatalf("mkdir network: %v", err)
	}
	networkCookiePath := makeChromeDB(t, networkDir)
	// Rename to match expected filename
	netPath := filepath.Join(networkDir, "Cookies")
	if networkCookiePath != netPath {
		if err := os.Rename(networkCookiePath, netPath); err != nil {
			t.Fatalf("rename: %v", err)
		}
	}

	directCookiePath := makeChromeDB(t, dir)

	specs := []browserSpec{
		{
			Name:        "Chrome",
			CookiePaths: []string{netPath, directCookiePath},
		},
	}

	_, source, err := detectWithSpecs("example.com", specs)
	if err != nil {
		t.Fatalf("detectWithSpecs: unexpected error: %v", err)
	}
	if source.Path != netPath {
		t.Errorf("Network/Cookies should be preferred: want %q, got %q", netPath, source.Path)
	}
}

// TestDetectBrowserCookies_BrowserNameSetOnSource verifies the returned
// CookieSource has the correct Browser name from the spec.
func TestDetectBrowserCookies_BrowserNameSetOnSource(t *testing.T) {
	dir := t.TempDir()

	chromeDir := filepath.Join(dir, "chrome")
	if err := os.MkdirAll(chromeDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cookiePath := makeChromeDB(t, chromeDir)

	specs := []browserSpec{
		{
			Name:        "Chromium",
			CookiePaths: []string{cookiePath},
		},
	}

	_, source, err := detectWithSpecs("example.com", specs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source.Browser != "Chromium" {
		t.Errorf("browser name: want %q, got %q", "Chromium", source.Browser)
	}
}
