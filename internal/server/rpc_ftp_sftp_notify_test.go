package server

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
)

// mockProtocolDownloader implements all 14 ProtocolDownloader interface methods
// for testing FTP/SFTP handler wiring in RPC downloadAdd.
type mockProtocolDownloader struct {
	hash          string
	fileName      string
	downloadDir   string
	contentLength int64
	probed        bool
	stopped       bool
	doneCh        chan struct{} // buffered(1), signals Download completed

	progressCalled int32 // atomic counter
	completeCalled int32 // atomic counter
}

func (m *mockProtocolDownloader) Probe(_ context.Context) (warplib.ProbeResult, error) {
	m.probed = true
	return warplib.ProbeResult{
		FileName:      m.fileName,
		ContentLength: m.contentLength,
		Resumable:     true,
	}, nil
}

func (m *mockProtocolDownloader) Download(_ context.Context, h *warplib.Handlers) error {
	if !m.probed {
		return warplib.ErrProbeRequired
	}
	if h != nil {
		if h.DownloadProgressHandler != nil {
			atomic.AddInt32(&m.progressCalled, 1)
			h.DownloadProgressHandler(m.hash, 512)
		}
		if h.DownloadCompleteHandler != nil {
			atomic.AddInt32(&m.completeCalled, 1)
			// MUST use MAIN_HASH — patchProtocolHandlers only finalizes item
			// when hash == MAIN_HASH
			h.DownloadCompleteHandler(warplib.MAIN_HASH, m.contentLength)
		}
	}
	close(m.doneCh)
	return nil
}

func (m *mockProtocolDownloader) Resume(_ context.Context, _ map[int64]*warplib.ItemPart, _ *warplib.Handlers) error {
	return nil
}

func (m *mockProtocolDownloader) Capabilities() warplib.DownloadCapabilities {
	return warplib.DownloadCapabilities{SupportsParallel: false, SupportsResume: true}
}

func (m *mockProtocolDownloader) Close() error                 { return nil }
func (m *mockProtocolDownloader) Stop()                        { m.stopped = true }
func (m *mockProtocolDownloader) IsStopped() bool              { return m.stopped }
func (m *mockProtocolDownloader) GetMaxConnections() int32     { return 1 }
func (m *mockProtocolDownloader) GetMaxParts() int32           { return 1 }
func (m *mockProtocolDownloader) GetHash() string              { return m.hash }
func (m *mockProtocolDownloader) GetFileName() string          { return m.fileName }
func (m *mockProtocolDownloader) GetDownloadDirectory() string { return m.downloadDir }
func (m *mockProtocolDownloader) GetSavePath() string          { return m.downloadDir + "/" + m.fileName }
func (m *mockProtocolDownloader) GetContentLength() warplib.ContentLength {
	return warplib.ContentLength(m.contentLength)
}

// newTestRPCHandlerWithRouter creates an RPC handler backed by a real Manager
// and a custom SchemeRouter. Returns the handler, auth secret, cleanup func,
// manager, and download directory.
func newTestRPCHandlerWithRouter(t *testing.T, router *warplib.SchemeRouter) (http.Handler, string, func(), *warplib.Manager, string) {
	t.Helper()
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	dlDir := filepath.Join(base, "downloads")
	if err := os.MkdirAll(dlDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	secret := "test-rpc-secret"
	pool := NewPool(log.New(io.Discard, "", 0))
	client := &http.Client{
		CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects),
	}
	cfg := &RPCConfig{
		Secret:    secret,
		Version:   "1.0.0",
		Commit:    "abc123",
		BuildType: "release",
	}
	rs := NewRPCServer(cfg, m, client, pool, router, log.New(io.Discard, "", 0))
	handler := requireToken(secret, rs.bridge)
	cleanup := func() {
		rs.Close()
		m.Close()
	}
	return handler, secret, cleanup, m, dlDir
}

// TestRPCDownloadAdd_FTP_ProgressTracked verifies that FTP downloads via
// RPC download.add update item.Downloaded during download (not just at completion).
func TestRPCDownloadAdd_FTP_ProgressTracked(t *testing.T) {
	router := warplib.NewSchemeRouter(http.DefaultClient)
	var mock *mockProtocolDownloader

	// Register a mock factory for "ftp" scheme that captures the mock.
	router.Register("ftp", func(rawURL string, opts *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		dlDir := opts.DownloadDirectory
		if dlDir == "" {
			dlDir = t.TempDir()
		}
		mock = &mockProtocolDownloader{
			hash:          "test-ftp-hash",
			fileName:      "test.bin",
			downloadDir:   dlDir,
			contentLength: 1024,
			doneCh:        make(chan struct{}, 1),
		}
		return mock, nil
	})

	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithRouter(t, router)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url": "ftp://example.com/test.bin",
		"dir": dlDir,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	gid, ok := result["gid"].(string)
	if !ok || gid == "" {
		t.Fatalf("expected non-empty gid, got %v", result["gid"])
	}

	// Wait for mock.Download to complete (5s deadline).
	select {
	case <-mock.doneCh:
		// Download completed
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for mock.Download to complete")
	}

	// Verify item state was updated.
	item := m.GetItem(gid)
	if item == nil {
		t.Fatal("item not found in manager after download")
	}
	if item.GetTotalSize() <= 0 {
		t.Fatalf("expected TotalSize > 0, got %d", item.GetTotalSize())
	}
	if item.GetDownloaded() < item.GetTotalSize() {
		t.Fatalf("expected Downloaded >= TotalSize, got Downloaded=%d TotalSize=%d",
			item.GetDownloaded(), item.GetTotalSize())
	}
}

// TestRPCDownloadAdd_SFTP_ProgressTracked verifies that SFTP downloads via
// RPC download.add update item.Downloaded during download.
func TestRPCDownloadAdd_SFTP_ProgressTracked(t *testing.T) {
	router := warplib.NewSchemeRouter(http.DefaultClient)
	var mock *mockProtocolDownloader

	// Register a mock factory for "sftp" scheme.
	router.Register("sftp", func(rawURL string, opts *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		dlDir := opts.DownloadDirectory
		if dlDir == "" {
			dlDir = t.TempDir()
		}
		mock = &mockProtocolDownloader{
			hash:          "test-sftp-hash",
			fileName:      "test.bin",
			downloadDir:   dlDir,
			contentLength: 2048,
			doneCh:        make(chan struct{}, 1),
		}
		return mock, nil
	})

	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithRouter(t, router)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url": "sftp://user:pass@example.com/test.bin",
		"dir": dlDir,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	gid, ok := result["gid"].(string)
	if !ok || gid == "" {
		t.Fatalf("expected non-empty gid, got %v", result["gid"])
	}

	// Wait for mock.Download to complete (5s deadline).
	select {
	case <-mock.doneCh:
		// Download completed
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for mock.Download to complete")
	}

	// Verify item state was updated.
	item := m.GetItem(gid)
	if item == nil {
		t.Fatal("item not found in manager after download")
	}
	if item.GetTotalSize() <= 0 {
		t.Fatalf("expected TotalSize > 0, got %d", item.GetTotalSize())
	}
	if item.GetDownloaded() < item.GetTotalSize() {
		t.Fatalf("expected Downloaded >= TotalSize, got Downloaded=%d TotalSize=%d",
			item.GetDownloaded(), item.GetTotalSize())
	}
}

// TestRPCDownloadAdd_FTP_HandlerCallbacksFired verifies that handler callbacks
// (DownloadProgressHandler, DownloadCompleteHandler) are actually invoked during
// FTP RPC download.add -- not just that the item state changed.
func TestRPCDownloadAdd_FTP_HandlerCallbacksFired(t *testing.T) {
	router := warplib.NewSchemeRouter(http.DefaultClient)
	var mock *mockProtocolDownloader

	// Register a mock factory for "ftp" scheme.
	router.Register("ftp", func(rawURL string, opts *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		dlDir := opts.DownloadDirectory
		if dlDir == "" {
			dlDir = t.TempDir()
		}
		mock = &mockProtocolDownloader{
			hash:          "test-ftp-callbacks",
			fileName:      "callbacks.bin",
			downloadDir:   dlDir,
			contentLength: 4096,
			doneCh:        make(chan struct{}, 1),
		}
		return mock, nil
	})

	handler, secret, cleanup, _, dlDir := newTestRPCHandlerWithRouter(t, router)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url": "ftp://example.com/callbacks.bin",
		"dir": dlDir,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	gid, ok := result["gid"].(string)
	if !ok || gid == "" {
		t.Fatalf("expected non-empty gid, got %v", result["gid"])
	}

	// Wait for mock.Download to complete (5s deadline).
	select {
	case <-mock.doneCh:
		// Download completed
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for mock.Download to complete")
	}

	// Assert handler callbacks were invoked (atomic counters).
	if pc := atomic.LoadInt32(&mock.progressCalled); pc <= 0 {
		t.Fatalf("expected progressCalled > 0, got %d — DownloadProgressHandler was NOT invoked", pc)
	}
	if cc := atomic.LoadInt32(&mock.completeCalled); cc <= 0 {
		t.Fatalf("expected completeCalled > 0, got %d — DownloadCompleteHandler was NOT invoked", cc)
	}
}
