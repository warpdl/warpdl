package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// ReportAsyncDownloadError is the seam daemon-side code (queue auto-start,
// scheduler) uses to surface async errors to the CLI. This test locks in
// the three things it MUST do: broadcast a typed error update on the pool,
// record a critical error keyed by uid, and stop the download. Without
// all three the CLI polls forever showing 0 B/s.
func TestReportAsyncDownloadError_BroadcastsStopsAndRecords(t *testing.T) {
	p := server.NewPool(nil)
	// Wire a subscriber to the broadcast.
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	sconn := server.NewSyncConn(c1)
	p.AddDownload("hash1", sconn)

	type readResult struct {
		msg []byte
		err error
	}
	peer := server.NewSyncConn(c2)
	ch := make(chan readResult, 1)
	go func() {
		msg, err := peer.Read()
		ch <- readResult{msg, err}
	}()

	ReportAsyncDownloadError(p, "hash1", errors.New("file already exists"), nil)

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("peer read err: %v", r.err)
		}
		if len(r.msg) == 0 {
			t.Fatal("broadcast payload empty")
		}
		if !contains(r.msg, "file already exists") {
			t.Fatalf("broadcast missing underlying error: %s", r.msg)
		}
		var frame struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    common.UpdateType            `json:"type"`
				Message common.DownloadErrorResponse `json:"message"`
			} `json:"update"`
		}
		if err := json.Unmarshal(r.msg, &frame); err != nil {
			t.Fatalf("decode broadcast: %v", err)
		}
		if !frame.Ok || frame.Update.Type != common.UPDATE_DOWNLOAD_ERROR {
			t.Fatalf("broadcast was not a typed async error: %s", r.msg)
		}
		if frame.Update.Message.DownloadId != "hash1" ||
			frame.Update.Message.Error != "file already exists" {
			t.Fatalf("unexpected async error payload: %+v", frame.Update.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast within 1s")
	}

	rec := p.GetError("hash1")
	if rec == nil {
		t.Fatal("expected recorded error on pool")
	}
	if rec.Message != "file already exists" {
		t.Errorf("recorded message = %q, want %q", rec.Message, "file already exists")
	}
	if rec.Type != server.ErrorTypeCritical {
		t.Errorf("recorded type = %v, want Critical", rec.Type)
	}
}

func TestReportAsyncDownloadError_ReplacesProgressBacklog(t *testing.T) {
	pool := server.NewPool(nil)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	pool.AddDownload("hash1", server.NewSyncConn(serverConn))

	progress := server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
		DownloadId: "hash1",
		Action:     common.DownloadProgress,
		Value:      1,
	})
	// With no reader, this deterministically fills the bounded pool queue.
	for range 128 {
		pool.Broadcast("hash1", progress)
	}
	ReportAsyncDownloadError(pool, "hash1", errors.New("disk is full"), nil)

	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	peer := server.NewSyncConn(clientConn)
	for range 2 {
		msg, err := peer.Read()
		if err != nil {
			t.Fatalf("terminal error was replaced by transport failure: %v", err)
		}
		var frame struct {
			Update struct {
				Type    common.UpdateType            `json:"type"`
				Message common.DownloadErrorResponse `json:"message"`
			} `json:"update"`
		}
		if err := json.Unmarshal(msg, &frame); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if frame.Update.Type != common.UPDATE_DOWNLOAD_ERROR {
			continue
		}
		if frame.Update.Message.Error != "disk is full" {
			t.Fatalf("terminal error = %q, want disk is full", frame.Update.Message.Error)
		}
		return
	}
	t.Fatal("terminal error did not replace queued progress")
}

// A failed download that never wrote a byte must be purged from the
// manager when ReportAsyncDownloadError is invoked with a non-nil
// manager. This is the "don't pollute history with ghost rows"
// behaviour the user asked for: retry of a failed download shouldn't
// accumulate "(1)" / "(2)" entries in the history.
func TestReportAsyncDownloadError_PurgesZeroByteFailure(t *testing.T) {
	// Real httptest server so NewDownloader succeeds and we can attach
	// a real *Downloader to the manager. The download is never started,
	// so item.Downloaded stays 0 — exactly the case we want to purge.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Disposition", `attachment; filename="x.bin"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	configDir := t.TempDir()
	if err := warplib.SetConfigDir(configDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	mgr, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer mgr.Close()

	d, err := warplib.NewDownloader(srv.Client(), srv.URL, &warplib.DownloaderOpts{
		DownloadDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := mgr.AddDownload(d, &warplib.AddDownloadOpts{AbsoluteLocation: t.TempDir()}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	hash := d.GetHash()
	if mgr.GetItem(hash) == nil {
		t.Fatal("setup: expected item to be in manager")
	}

	pool := server.NewPool(nil)
	ReportAsyncDownloadError(pool, hash, errors.New("simulated start failure"), mgr)

	if got := mgr.GetItem(hash); got != nil {
		t.Fatalf("zero-byte failure must purge from history; got %+v", got)
	}
}

// Mid-download failures (Downloaded > 0) must be KEPT so the user can
// resume. Regression guard against an over-eager purge.
func TestReportAsyncDownloadError_KeepsPartialDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Header().Set("Content-Disposition", `attachment; filename="y.bin"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	configDir := t.TempDir()
	if err := warplib.SetConfigDir(configDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	mgr, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer mgr.Close()

	d, err := warplib.NewDownloader(srv.Client(), srv.URL, &warplib.DownloaderOpts{
		DownloadDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := mgr.AddDownload(d, &warplib.AddDownloadOpts{AbsoluteLocation: t.TempDir()}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	hash := d.GetHash()
	// Simulate that some bytes were downloaded before the failure.
	item := mgr.GetItem(hash)
	if item == nil {
		t.Fatal("setup: expected item")
	}
	item.Downloaded = 42
	mgr.UpdateItem(item)

	pool := server.NewPool(nil)
	ReportAsyncDownloadError(pool, hash, errors.New("network died mid-download"), mgr)

	got := mgr.GetItem(hash)
	if got == nil {
		t.Fatal("partial download should NOT be purged from history")
	}
	if got.Downloaded != 42 {
		t.Errorf("Downloaded = %d, want 42 (preserved)", got.Downloaded)
	}
}

// A nil err must be a no-op — don't pollute the pool with empty errors
// if a caller accidentally invokes the reporter in a success path.
func TestReportAsyncDownloadError_NilIsNoop(t *testing.T) {
	p := server.NewPool(nil)
	ReportAsyncDownloadError(p, "hash1", nil, nil)
	if rec := p.GetError("hash1"); rec != nil {
		t.Fatalf("nil err should not record: %+v", rec)
	}
}

func contains(haystack []byte, needle string) bool {
	return indexOf(string(haystack), needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
