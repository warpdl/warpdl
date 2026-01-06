package warplib

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestResumeDownload_HeaderMerge_AddNew verifies that new headers passed during
// resume are ADDED to existing headers, not silently ignored.
// This tests the bug at manager.go:314-322 where the header merge logic
// only updates when ih == oh (entire struct match), never adding new headers.
func TestResumeDownload_HeaderMerge_AddNew(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	content := []byte("test-content-12345")

	// Server that accepts any request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	// Create item with one existing header
	item := &Item{
		Hash:             "test-add-new-headers",
		Name:             "file.bin",
		Url:              srv.URL + "/file.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers:          Headers{{Key: "X-Existing", Value: "old-value"}},
		mu:               m.mu,
		memPart:          make(map[string]int64),
	}
	m.UpdateItem(item)

	// Create dldata directory for integrity validation
	if err := os.MkdirAll(filepath.Join(DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Resume with NEW headers including Cookie
	_, err = m.ResumeDownload(&http.Client{}, item.Hash, &ResumeDownloadOpts{
		Headers: Headers{
			{Key: "Cookie", Value: "session=abc123"},
			{Key: "X-New", Value: "new-value"},
		},
	})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}

	// Verify: Should have 3 headers (1 existing + 2 new)
	resumed := m.GetItem(item.Hash)
	if resumed == nil {
		t.Fatal("item not found after resume")
	}
	if resumed.dAlloc != nil {
		defer resumed.dAlloc.Close()
	}

	if len(resumed.Headers) != 3 {
		t.Errorf("expected 3 headers, got %d: %+v", len(resumed.Headers), resumed.Headers)
	}

	// Verify Cookie header was added
	if idx, ok := resumed.Headers.Get("Cookie"); !ok {
		t.Error("Cookie header not found - new headers were NOT added")
	} else if resumed.Headers[idx].Value != "session=abc123" {
		t.Errorf("Cookie value mismatch: got %q, want %q", resumed.Headers[idx].Value, "session=abc123")
	}

	// Verify X-New header was added
	if idx, ok := resumed.Headers.Get("X-New"); !ok {
		t.Error("X-New header not found - new headers were NOT added")
	} else if resumed.Headers[idx].Value != "new-value" {
		t.Errorf("X-New value mismatch: got %q, want %q", resumed.Headers[idx].Value, "new-value")
	}

	// Verify X-Existing header still present
	if idx, ok := resumed.Headers.Get("X-Existing"); !ok {
		t.Error("X-Existing header was lost")
	} else if resumed.Headers[idx].Value != "old-value" {
		t.Errorf("X-Existing value changed unexpectedly: got %q", resumed.Headers[idx].Value)
	}
}

// TestResumeDownload_HeaderMerge_UpdateExisting verifies that headers with
// matching keys are UPDATED (not duplicated) during resume.
func TestResumeDownload_HeaderMerge_UpdateExisting(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	content := []byte("test-content")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	// Create item with Cookie and Authorization headers
	item := &Item{
		Hash:             "test-update-existing",
		Name:             "file.bin",
		Url:              srv.URL + "/file.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers: Headers{
			{Key: "Cookie", Value: "session=old-token"},
			{Key: "Authorization", Value: "Bearer old-jwt"},
		},
		mu:      m.mu,
		memPart: make(map[string]int64),
	}
	m.UpdateItem(item)

	if err := os.MkdirAll(filepath.Join(DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Resume with UPDATED Cookie header (refreshed token)
	_, err = m.ResumeDownload(&http.Client{}, item.Hash, &ResumeDownloadOpts{
		Headers: Headers{{Key: "Cookie", Value: "session=new-token"}},
	})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}

	resumed := m.GetItem(item.Hash)
	if resumed == nil {
		t.Fatal("item not found after resume")
	}
	if resumed.dAlloc != nil {
		defer resumed.dAlloc.Close()
	}

	// Verify: Should still have 2 headers (not 3 - no duplication!)
	if len(resumed.Headers) != 2 {
		t.Errorf("expected 2 headers (no duplication), got %d: %+v", len(resumed.Headers), resumed.Headers)
	}

	// Verify Cookie was updated
	idx, ok := resumed.Headers.Get("Cookie")
	if !ok {
		t.Fatal("Cookie header not found")
	}
	if resumed.Headers[idx].Value != "session=new-token" {
		t.Errorf("Cookie not updated: got %q, want %q", resumed.Headers[idx].Value, "session=new-token")
	}

	// Verify Authorization was NOT changed
	authIdx, ok := resumed.Headers.Get("Authorization")
	if !ok {
		t.Fatal("Authorization header was lost")
	}
	if resumed.Headers[authIdx].Value != "Bearer old-jwt" {
		t.Errorf("Authorization was modified: got %q, want %q", resumed.Headers[authIdx].Value, "Bearer old-jwt")
	}
}

// TestResumeDownload_HeaderMerge_NilHeaders verifies that adding headers
// to an item with nil Headers doesn't panic.
func TestResumeDownload_HeaderMerge_NilHeaders(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	content := []byte("test")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	// Create item with NO headers (nil)
	item := &Item{
		Hash:             "test-nil-headers",
		Name:             "file.bin",
		Url:              srv.URL + "/file.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers:          nil, // Explicitly nil
		mu:               m.mu,
		memPart:          make(map[string]int64),
	}
	m.UpdateItem(item)

	if err := os.MkdirAll(filepath.Join(DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Resume with Cookie header - should not panic
	_, err = m.ResumeDownload(&http.Client{}, item.Hash, &ResumeDownloadOpts{
		Headers: Headers{{Key: "Cookie", Value: "session=abc"}},
	})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}

	resumed := m.GetItem(item.Hash)
	if resumed == nil {
		t.Fatal("item not found after resume")
	}
	if resumed.dAlloc != nil {
		defer resumed.dAlloc.Close()
	}

	// Verify: Should have 1 header (the new Cookie)
	if len(resumed.Headers) != 1 {
		t.Errorf("expected 1 header, got %d: %+v", len(resumed.Headers), resumed.Headers)
	}

	// Verify Cookie header was added
	if idx, ok := resumed.Headers.Get("Cookie"); !ok {
		t.Error("Cookie header not found")
	} else if resumed.Headers[idx].Value != "session=abc" {
		t.Errorf("Cookie value mismatch: got %q", resumed.Headers[idx].Value)
	}
}

// TestResumeDownload_HeaderMerge_NilOpts verifies that nil ResumeDownloadOpts
// doesn't cause issues.
func TestResumeDownload_HeaderMerge_NilOpts(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	content := []byte("test")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	// Create item with existing Cookie header
	item := &Item{
		Hash:             "test-nil-opts",
		Name:             "file.bin",
		Url:              srv.URL + "/file.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		Headers:          Headers{{Key: "Cookie", Value: "session=preserved"}},
		mu:               m.mu,
		memPart:          make(map[string]int64),
	}
	m.UpdateItem(item)

	if err := os.MkdirAll(filepath.Join(DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Resume with nil opts - existing headers should be preserved
	_, err = m.ResumeDownload(&http.Client{}, item.Hash, nil)
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}

	resumed := m.GetItem(item.Hash)
	if resumed == nil {
		t.Fatal("item not found after resume")
	}
	if resumed.dAlloc != nil {
		defer resumed.dAlloc.Close()
	}

	// Verify existing header preserved
	if len(resumed.Headers) != 1 {
		t.Errorf("expected 1 header, got %d", len(resumed.Headers))
	}
	if idx, ok := resumed.Headers.Get("Cookie"); !ok {
		t.Error("Cookie header was lost")
	} else if resumed.Headers[idx].Value != "session=preserved" {
		t.Errorf("Cookie value changed: got %q", resumed.Headers[idx].Value)
	}
}
