package server

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warpdl/warpdl/pkg/warplib"
)

// TestProcessDownload_RedirectLoopBlocked verifies that processDownload
// enforces the redirect policy from Phase 1. A URL that creates a redirect
// loop must fail with a redirect error, not loop indefinitely.
func TestProcessDownload_RedirectLoopBlocked(t *testing.T) {
	// Create a server that always redirects to itself
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer srv.Close()

	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	pool := NewPool(log.New(io.Discard, "", 0))
	ws := &WebServer{
		l:    log.New(io.Discard, "", 0),
		m:    m,
		pool: pool,
	}

	err = ws.processDownload(&capturedDownload{
		Url: srv.URL + "/redirect-loop.bin",
	})

	// Should fail because the redirect loop exceeds DefaultMaxRedirects
	if err == nil {
		t.Fatal("expected error from redirect loop, got nil")
	}

	// After the fix, warplib.RedirectPolicy produces an error containing "redirect loop"
	if !strings.Contains(err.Error(), "redirect loop") {
		t.Fatalf("expected error to contain 'redirect loop', got: %v", err)
	}
	t.Logf("redirect error (expected): %v", err)
}
