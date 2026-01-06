package warplib

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestCookiePersistence_AcrossDaemonRestart verifies that cookies stored in
// Item.Headers survive manager close/reopen (simulating daemon restart).
func TestCookiePersistence_AcrossDaemonRestart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := []byte("authenticated-content-data")

	// Server that requires authentication (checks Cookie header)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	hash := "cookie-persist-test"

	// Phase 1: Create download with cookie
	m1, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	item := &Item{
		Hash:             hash,
		Name:             "secure-file.bin",
		Url:              srv.URL + "/secure.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers:          Headers{{Key: "Cookie", Value: "session=secret-token-123"}},
		mu:               m1.mu,
		memPart:          make(map[string]int64),
	}
	m1.UpdateItem(item)

	// Create dldata directory
	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Verify cookie stored before close
	storedItem := m1.GetItem(hash)
	if idx, ok := storedItem.Headers.Get("Cookie"); !ok {
		t.Fatal("Cookie not stored in initial item")
	} else if storedItem.Headers[idx].Value != "session=secret-token-123" {
		t.Fatal("Cookie value mismatch in initial item")
	}

	// Close manager (simulate daemon shutdown)
	m1.Close()

	// Phase 2: Reload manager (simulate daemon restart)
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager after restart: %v", err)
	}
	defer m2.Close()

	// Verify cookie survived restart
	reloadedItem := m2.GetItem(hash)
	if reloadedItem == nil {
		t.Fatal("Item not found after restart")
	}

	cookieIdx, ok := reloadedItem.Headers.Get("Cookie")
	if !ok {
		t.Fatal("Cookie header lost after restart")
	}
	if reloadedItem.Headers[cookieIdx].Value != "session=secret-token-123" {
		t.Fatalf("Cookie value corrupted after restart: got %q", reloadedItem.Headers[cookieIdx].Value)
	}
}

// TestCookiePersistence_UpdateOnResume verifies that when cookies are updated
// during resume, the new values are persisted to disk.
func TestCookiePersistence_UpdateOnResume(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := []byte("test-content")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	hash := "cookie-update-persist"

	// Phase 1: Create download with old cookie
	m1, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	item := &Item{
		Hash:             hash,
		Name:             "file.bin",
		Url:              srv.URL + "/file.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers:          Headers{{Key: "Cookie", Value: "session=old-token"}},
		mu:               m1.mu,
		memPart:          make(map[string]int64),
	}
	m1.UpdateItem(item)

	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Resume with UPDATED cookie
	resumedItem, err := m1.ResumeDownload(&http.Client{}, hash, &ResumeDownloadOpts{
		Headers: Headers{{Key: "Cookie", Value: "session=refreshed-token"}},
	})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}
	if resumedItem.dAlloc != nil {
		resumedItem.dAlloc.Close()
	}

	// Verify cookie updated in memory
	inMemoryItem := m1.GetItem(hash)
	cookieIdx, _ := inMemoryItem.Headers.Get("Cookie")
	if inMemoryItem.Headers[cookieIdx].Value != "session=refreshed-token" {
		t.Fatalf("Cookie not updated in memory: got %q", inMemoryItem.Headers[cookieIdx].Value)
	}

	// Close manager
	m1.Close()

	// Phase 2: Reload and verify persistence
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager after resume: %v", err)
	}
	defer m2.Close()

	reloadedItem := m2.GetItem(hash)
	if reloadedItem == nil {
		t.Fatal("Item not found after reload")
	}

	persistedIdx, ok := reloadedItem.Headers.Get("Cookie")
	if !ok {
		t.Fatal("Cookie header lost after reload")
	}

	// THIS IS THE KEY TEST: Updated cookie must persist
	if reloadedItem.Headers[persistedIdx].Value != "session=refreshed-token" {
		t.Fatalf("Updated cookie NOT persisted: got %q, want %q",
			reloadedItem.Headers[persistedIdx].Value, "session=refreshed-token")
	}
}

// TestCookiePersistence_MultipleCookies verifies persistence with multiple cookies
// in a single Cookie header (separated by "; ").
func TestCookiePersistence_MultipleCookies(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := []byte("data")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	hash := "multi-cookie-test"
	cookieValue := "session=abc123; user_id=xyz789; auth_token=jwt-here"

	m1, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	item := &Item{
		Hash:             hash,
		Name:             "file.bin",
		Url:              srv.URL + "/file.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers:          Headers{{Key: "Cookie", Value: cookieValue}},
		mu:               m1.mu,
		memPart:          make(map[string]int64),
	}
	m1.UpdateItem(item)
	m1.Close()

	// Reload and verify
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m2.Close()

	reloaded := m2.GetItem(hash)
	if reloaded == nil {
		t.Fatal("Item not found")
	}

	idx, ok := reloaded.Headers.Get("Cookie")
	if !ok {
		t.Fatal("Cookie header lost")
	}

	if reloaded.Headers[idx].Value != cookieValue {
		t.Errorf("Cookie value corrupted:\ngot:  %q\nwant: %q", reloaded.Headers[idx].Value, cookieValue)
	}
}

// TestCookiePersistence_SpecialCharacters verifies persistence with special characters
// that might cause encoding issues.
func TestCookiePersistence_SpecialCharacters(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Cookie with URL-encoded special characters
	specialCookie := "token=abc%3D123%3Btest%26value; session=x%2Fy%2Fz"

	m1, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	item := &Item{
		Hash:             "special-chars",
		Name:             "file.bin",
		Url:              "http://example.com/file.bin",
		TotalSize:        100,
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers:          Headers{{Key: "Cookie", Value: specialCookie}},
		mu:               m1.mu,
		memPart:          make(map[string]int64),
	}
	m1.UpdateItem(item)
	m1.Close()

	m2, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m2.Close()

	reloaded := m2.GetItem("special-chars")
	idx, ok := reloaded.Headers.Get("Cookie")
	if !ok {
		t.Fatal("Cookie lost")
	}

	if reloaded.Headers[idx].Value != specialCookie {
		t.Errorf("Special chars corrupted:\ngot:  %q\nwant: %q", reloaded.Headers[idx].Value, specialCookie)
	}
}

// TestCookiePersistence_AddNewOnResume verifies that new headers added during
// resume are persisted (not just updates to existing headers).
func TestCookiePersistence_AddNewOnResume(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := []byte("test")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	hash := "add-new-cookie-persist"

	// Phase 1: Create download WITHOUT cookie
	m1, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	item := &Item{
		Hash:             hash,
		Name:             "file.bin",
		Url:              srv.URL + "/file.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers:          Headers{{Key: "User-Agent", Value: "WarpDL/1.0"}}, // No cookie initially
		mu:               m1.mu,
		memPart:          make(map[string]int64),
	}
	m1.UpdateItem(item)

	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Resume with NEW Cookie header
	resumedItem, err := m1.ResumeDownload(&http.Client{}, hash, &ResumeDownloadOpts{
		Headers: Headers{{Key: "Cookie", Value: "session=brand-new-cookie"}},
	})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}
	if resumedItem.dAlloc != nil {
		resumedItem.dAlloc.Close()
	}

	m1.Close()

	// Phase 2: Reload and verify NEW cookie was persisted
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m2.Close()

	reloaded := m2.GetItem(hash)
	if reloaded == nil {
		t.Fatal("Item not found")
	}

	// Should have 2 headers: User-Agent + Cookie
	if len(reloaded.Headers) != 2 {
		t.Errorf("expected 2 headers, got %d: %+v", len(reloaded.Headers), reloaded.Headers)
	}

	// Verify Cookie was persisted
	cookieIdx, ok := reloaded.Headers.Get("Cookie")
	if !ok {
		t.Fatal("NEW Cookie header not persisted")
	}
	if reloaded.Headers[cookieIdx].Value != "session=brand-new-cookie" {
		t.Errorf("Cookie value not persisted: got %q", reloaded.Headers[cookieIdx].Value)
	}

	// Verify User-Agent still present
	uaIdx, ok := reloaded.Headers.Get("User-Agent")
	if !ok {
		t.Fatal("User-Agent header lost")
	}
	if reloaded.Headers[uaIdx].Value != "WarpDL/1.0" {
		t.Errorf("User-Agent changed: got %q", reloaded.Headers[uaIdx].Value)
	}
}
