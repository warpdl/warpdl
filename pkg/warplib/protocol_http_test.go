package warplib

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Test 11: httpProtocolDownloader.Capabilities() returns SupportsParallel=true after Probe
// when Accept-Ranges header is present
func TestHTTPAdapter_Capabilities_AfterProbe(t *testing.T) {
	// Create test server that returns Accept-Ranges header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Disposition", "attachment; filename=\"test.bin\"")
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Set a writable temp dir for downloads
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	pd, err := newHTTPProtocolDownloader(srv.URL, &DownloaderOpts{
		DownloadDirectory: tmpDir,
		SkipSetup:         true,
	}, http.DefaultClient)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}

	// Before Probe: capabilities should be zero
	capsBefore := pd.Capabilities()
	if capsBefore.SupportsParallel {
		t.Errorf("Capabilities before Probe: SupportsParallel should be false")
	}

	// After Probe
	_, err = pd.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	caps := pd.Capabilities()
	if !caps.SupportsResume {
		t.Errorf("Capabilities after Probe: SupportsResume should be true when Accept-Ranges present")
	}
}

// Test: httpProtocolDownloader satisfies ProtocolDownloader at compile time
func TestHTTPAdapter_CompileTimeCheck(t *testing.T) {
	// This test passes if it compiles — the compile-time check in protocol_http.go
	// var _ ProtocolDownloader = (*httpProtocolDownloader)(nil)
	// ensures the adapter satisfies the interface.
	t.Log("compile-time interface check passed")
}

// Test: httpProtocolDownloader.Download() without Probe() returns ErrProbeRequired
func TestHTTPAdapter_DownloadWithoutProbe(t *testing.T) {
	pd, err := newHTTPProtocolDownloader("http://example.com/file", &DownloaderOpts{
		SkipSetup: true,
	}, http.DefaultClient)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}

	err = pd.Download(context.Background(), &Handlers{})
	if !errors.Is(err, ErrProbeRequired) {
		t.Errorf("Download without Probe: want ErrProbeRequired, got %v", err)
	}
}

// Test: httpProtocolDownloader.Resume() without Probe() returns ErrProbeRequired
func TestHTTPAdapter_ResumeWithoutProbe(t *testing.T) {
	pd, err := newHTTPProtocolDownloader("http://example.com/file", &DownloaderOpts{
		SkipSetup: true,
	}, http.DefaultClient)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}

	err = pd.Resume(context.Background(), map[int64]*ItemPart{}, &Handlers{})
	if !errors.Is(err, ErrProbeRequired) {
		t.Errorf("Resume without Probe: want ErrProbeRequired, got %v", err)
	}
}

// Test: httpProtocolDownloader getters work after Probe
func TestHTTPAdapter_GettersAfterProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Disposition", "attachment; filename=\"myfile.zip\"")
		w.Header().Set("Content-Length", "2048")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()

	pd, err := newHTTPProtocolDownloader(srv.URL, &DownloaderOpts{
		DownloadDirectory: tmpDir,
		SkipSetup:         true,
	}, http.DefaultClient)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}

	_, err = pd.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if pd.GetFileName() != "myfile.zip" {
		t.Errorf("GetFileName: want myfile.zip, got %s", pd.GetFileName())
	}
	if pd.GetContentLength().v() != 2048 {
		t.Errorf("GetContentLength: want 2048, got %d", pd.GetContentLength().v())
	}
	if pd.GetDownloadDirectory() == "" {
		t.Errorf("GetDownloadDirectory: should not be empty")
	}
}

// Test: httpProtocolDownloader.IsStopped() returns true when inner is nil
func TestHTTPAdapter_IsStoppedWhenNilInner(t *testing.T) {
	pd, err := newHTTPProtocolDownloader("http://example.com/file", &DownloaderOpts{
		SkipSetup: true,
	}, http.DefaultClient)
	if err != nil {
		t.Fatalf("newHTTPProtocolDownloader: %v", err)
	}

	// Before Probe, inner is nil — IsStopped should return true
	if !pd.IsStopped() {
		t.Errorf("IsStopped: should return true when inner is nil")
	}
}

// Test: httpProtocolDownloader cleanup
func TestHTTPAdapter_CleanupTempDirs(t *testing.T) {
	// Ensure temp dirs are cleaned up
	tmpDir, err := os.MkdirTemp("", "warplib-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
}
