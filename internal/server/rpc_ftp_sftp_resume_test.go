package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
)

// TestRPCDownloadResume_FTP_HandlersFired verifies that FTP downloads
// resumed via RPC download.resume invoke handler callbacks (progress/complete).
func TestRPCDownloadResume_FTP_HandlersFired(t *testing.T) {
	router := warplib.NewSchemeRouter(http.DefaultClient)
	var mock *mockProtocolDownloader

	// Register a mock factory for "ftp" scheme.
	// The factory is called TWICE: once for download.add, once for download.resume
	// (ResumeDownload creates a new ProtocolDownloader via SchemeRouter).
	router.Register("ftp", func(rawURL string, opts *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		dlDir := opts.DownloadDirectory
		if dlDir == "" {
			dlDir = t.TempDir()
		}
		mock = &mockProtocolDownloader{
			hash:          "test-ftp-resume",
			fileName:      "resume.bin",
			downloadDir:   dlDir,
			contentLength: 2048,
			doneCh:        make(chan struct{}, 1),
		}
		return mock, nil
	})

	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithRouter(t, router)
	defer cleanup()

	// Phase 1: Add an FTP download and wait for it to complete.
	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url": "ftp://example.com/resume.bin",
		"dir": dlDir,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("download.add: expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	gid, ok := result["gid"].(string)
	if !ok || gid == "" {
		t.Fatalf("expected non-empty gid, got %v", result["gid"])
	}

	// Wait for initial download to complete.
	select {
	case <-mock.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for initial download to complete")
	}

	// Create a dummy destination file so ResumeDownload's integrity check passes.
	// The mock doesn't actually write files, but resume validates the dest file exists
	// when item.Downloaded > 0.
	destFile := filepath.Join(dlDir, "resume.bin")
	if err := os.WriteFile(destFile, make([]byte, 2048), 0644); err != nil {
		t.Fatalf("failed to create dummy dest file: %v", err)
	}

	// Phase 2: Pause the download.
	code, resp = rpcCall(t, handler, "download.pause", map[string]any{
		"gid": gid,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("download.pause: expected 200, got %d", code)
	}

	// Phase 3: Resume the download.
	// The factory will be called again, creating a fresh mock.
	// Reset counters happen automatically because a new mock is created.
	code, resp = rpcCall(t, handler, "download.resume", map[string]any{
		"gid": gid,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("download.resume: expected 200, got %d; resp=%v", code, resp)
	}
	// Verify the RPC response contains a result (not an error).
	resumeResult := rpcResult(t, resp)
	t.Logf("download.resume result: %v", resumeResult)

	// Wait for resume to complete via the NEW mock's doneCh.
	select {
	case <-mock.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for resumed download to complete")
	}

	// Assert handler callbacks were invoked during resume.
	pc := atomic.LoadInt32(&mock.progressCalled)
	cc := atomic.LoadInt32(&mock.completeCalled)
	t.Logf("resume mock: progressCalled=%d completeCalled=%d hash=%s", pc, cc, mock.hash)
	if pc <= 0 {
		t.Fatalf("expected progressCalled > 0, got %d — DownloadProgressHandler NOT invoked during resume", pc)
	}
	if cc <= 0 {
		t.Fatalf("expected completeCalled > 0, got %d — DownloadCompleteHandler NOT invoked during resume", cc)
	}

	// Verify item state updated.
	item := m.GetItem(gid)
	if item == nil {
		t.Fatal("item not found in manager after resume")
	}
	if item.GetDownloaded() <= 0 {
		t.Fatalf("expected Downloaded > 0 after resume, got %d", item.GetDownloaded())
	}
}

// TestRPCDownloadResume_SFTP_HandlersFired verifies that SFTP downloads
// resumed via RPC download.resume invoke handler callbacks.
func TestRPCDownloadResume_SFTP_HandlersFired(t *testing.T) {
	router := warplib.NewSchemeRouter(http.DefaultClient)
	var mock *mockProtocolDownloader

	router.Register("sftp", func(rawURL string, opts *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		dlDir := opts.DownloadDirectory
		if dlDir == "" {
			dlDir = t.TempDir()
		}
		mock = &mockProtocolDownloader{
			hash:          "test-sftp-resume",
			fileName:      "resume-sftp.bin",
			downloadDir:   dlDir,
			contentLength: 4096,
			doneCh:        make(chan struct{}, 1),
		}
		return mock, nil
	})

	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithRouter(t, router)
	defer cleanup()

	// Phase 1: Add an SFTP download and wait for it to complete.
	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url": "sftp://user:pass@example.com/resume-sftp.bin",
		"dir": dlDir,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("download.add: expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	gid, ok := result["gid"].(string)
	if !ok || gid == "" {
		t.Fatalf("expected non-empty gid, got %v", result["gid"])
	}

	select {
	case <-mock.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for initial SFTP download to complete")
	}

	// Create a dummy destination file so ResumeDownload's integrity check passes.
	destFile := filepath.Join(dlDir, "resume-sftp.bin")
	if err := os.WriteFile(destFile, make([]byte, 4096), 0644); err != nil {
		t.Fatalf("failed to create dummy dest file: %v", err)
	}

	// Phase 2: Pause.
	code, resp = rpcCall(t, handler, "download.pause", map[string]any{
		"gid": gid,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("download.pause: expected 200, got %d", code)
	}

	// Phase 3: Resume.
	code, resp = rpcCall(t, handler, "download.resume", map[string]any{
		"gid": gid,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("download.resume: expected 200, got %d; resp=%v", code, resp)
	}
	resumeResult := rpcResult(t, resp)
	t.Logf("download.resume result: %v", resumeResult)

	select {
	case <-mock.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for resumed SFTP download to complete")
	}

	pc := atomic.LoadInt32(&mock.progressCalled)
	cc := atomic.LoadInt32(&mock.completeCalled)
	t.Logf("resume mock: progressCalled=%d completeCalled=%d hash=%s", pc, cc, mock.hash)
	if pc <= 0 {
		t.Fatalf("expected progressCalled > 0, got %d — DownloadProgressHandler NOT invoked during SFTP resume", pc)
	}
	if cc <= 0 {
		t.Fatalf("expected completeCalled > 0, got %d — DownloadCompleteHandler NOT invoked during SFTP resume", cc)
	}

	item := m.GetItem(gid)
	if item == nil {
		t.Fatal("item not found in manager after SFTP resume")
	}
	if item.GetDownloaded() <= 0 {
		t.Fatalf("expected Downloaded > 0 after SFTP resume, got %d", item.GetDownloaded())
	}
}
