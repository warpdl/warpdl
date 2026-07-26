package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
)

type rpcRetainedBody struct {
	*bytes.Reader
	closed atomic.Bool
}

func (b *rpcRetainedBody) Close() error {
	b.closed.Store(true)
	return nil
}

type rpcRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f rpcRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failingRPCProtocol struct {
	*mockProtocolDownloader
	runErr error
}

func (d *failingRPCProtocol) Download(_ context.Context, _ *warplib.Handlers) error {
	return d.runErr
}

type closeFailingRPCProtocol struct {
	*mockProtocolDownloader
	closeErr error
}

func (d *closeFailingRPCProtocol) Close() error {
	return d.closeErr
}

type cancellingRPCProtocol struct {
	*mockProtocolDownloader
	probeCtx context.Context
	started  chan struct{}
}

func (d *cancellingRPCProtocol) Probe(ctx context.Context) (warplib.ProbeResult, error) {
	d.probeCtx = ctx
	return d.mockProtocolDownloader.Probe(ctx)
}

func (d *cancellingRPCProtocol) Download(ctx context.Context, handlers *warplib.Handlers) error {
	close(d.started)
	<-ctx.Done()
	if handlers != nil && handlers.ErrorHandler != nil {
		handlers.ErrorHandler(d.hash, ctx.Err())
	}
	return ctx.Err()
}

func newRPCQueueTestManager(t *testing.T) (*warplib.Manager, *Pool) {
	t.Helper()
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return manager, NewPool(log.New(io.Discard, "", 0))
}

func TestRPCDownloadAddHonorsPausedQueueAndClosesValidatorlessProbe(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	var starts atomic.Int32
	manager.SetMaxConcurrentDownloads(1, func(string) { starts.Add(1) })
	queue := manager.GetQueue()
	queue.Pause()

	payload := []byte("validatorless queued representation")
	body := &rpcRetainedBody{Reader: bytes.NewReader(payload)}
	client := &http.Client{Transport: rpcRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          body,
			ContentLength: int64(len(payload)),
			Request:       req,
		}, nil
	})}
	rs := &RPCServer{manager: manager, client: client, pool: pool}

	result, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: "http://representation.test/rpc-queued.bin",
		Dir: warplib.ConfigDir,
	})
	if err != nil {
		t.Fatalf("downloadAdd: %v", err)
	}
	if starts.Load() != 0 {
		t.Fatal("paused queue started an RPC download")
	}
	if !queue.IsWaiting(result.GID) {
		t.Fatal("RPC download was not left waiting")
	}
	item := manager.GetItem(result.GID)
	if item == nil || item.IsDownloading() {
		t.Fatalf("waiting item = %v, live downloader = %v", item, item != nil && item.IsDownloading())
	}
	if !body.closed.Load() {
		t.Fatal("waiting RPC download retained its validator-less metadata body")
	}
	if !pool.HasDownload(result.GID) {
		t.Fatal("waiting RPC download was removed from the attach pool")
	}
}

func TestRPCDownloadAddCleansUpReturnedHTTPStartError(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	payload := []byte("cannot create destination")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "25")
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	rs := &RPCServer{
		manager: manager,
		client:  server.Client(),
		pool:    pool,
	}

	result, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: server.URL + "/file.bin",
		Dir: filepath.Join(warplib.ConfigDir, "missing", "directory"),
	})
	if err != nil {
		t.Fatalf("downloadAdd: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		return !pool.HasDownload(result.GID) && manager.GetItem(result.GID) == nil
	})
}

func TestRPCDownloadAddAdmissionRejectionReleasesPendingRunLease(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	payload := []byte("pending run lease")
	body := &rpcRetainedBody{Reader: bytes.NewReader(payload)}
	client := &http.Client{Transport: rpcRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          body,
			ContentLength: int64(len(payload)),
			Request:       req,
		}, nil
	})}
	rs := &RPCServer{
		manager: manager,
		client:  client,
		pool:    pool,
		transferLauncher: func(func(context.Context)) bool {
			return false
		},
	}

	_, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: "http://representation.test/rejected.bin",
		Dir: warplib.ConfigDir,
	})
	if err == nil || !strings.Contains(err.Error(), warplib.ErrManagerShuttingDown.Error()) {
		t.Fatalf("downloadAdd error = %v, want %v", err, warplib.ErrManagerShuttingDown)
	}
	if !body.closed.Load() {
		t.Fatal("admission rejection retained the metadata response body")
	}
	if items := manager.GetItems(); len(items) != 0 {
		t.Fatalf("admission rejection retained %d manager item(s)", len(items))
	}
	pool.mu.RLock()
	active := len(pool.m)
	pool.mu.RUnlock()
	if active != 0 {
		t.Fatalf("admission rejection retained %d pool registration(s)", active)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.WaitTransfers(waitCtx); err != nil {
		t.Fatalf("pending run lease did not drain: %v", err)
	}
}

func TestRPCDownloadAddCleansUpReturnedProtocolError(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	router := warplib.NewSchemeRouter(http.DefaultClient)
	runErr := errors.New("injected protocol start failure")
	router.Register("ftp", func(string, *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		return &failingRPCProtocol{
			mockProtocolDownloader: &mockProtocolDownloader{
				hash:          "rpc-failing-protocol",
				fileName:      "failure.bin",
				downloadDir:   warplib.ConfigDir,
				contentLength: 10,
				doneCh:        make(chan struct{}, 1),
			},
			runErr: runErr,
		}, nil
	})
	rs := &RPCServer{
		manager:      manager,
		client:       http.DefaultClient,
		pool:         pool,
		schemeRouter: router,
	}

	result, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: "ftp://example.invalid/failure.bin",
		Dir: warplib.ConfigDir,
	})
	if err != nil {
		t.Fatalf("downloadAdd: %v", err)
	}
	waitForCondition(t, time.Second, func() bool {
		return !pool.HasDownload(result.GID) && manager.GetItem(result.GID) == nil
	})
}

func TestRPCProtocolCancellationUsesManagerLifetimeWithoutCriticalError(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	router := warplib.NewSchemeRouter(http.DefaultClient)
	allocation := &cancellingRPCProtocol{
		mockProtocolDownloader: &mockProtocolDownloader{
			hash:          "rpc-cancelled-protocol",
			fileName:      "cancelled.bin",
			downloadDir:   warplib.ConfigDir,
			contentLength: 1024,
			doneCh:        make(chan struct{}, 1),
		},
		started: make(chan struct{}),
	}
	router.Register("ftp", func(string, *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		return allocation, nil
	})
	rs := &RPCServer{
		manager:      manager,
		client:       http.DefaultClient,
		pool:         pool,
		schemeRouter: router,
	}

	result, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: "ftp://example.invalid/cancelled.bin",
		Dir: warplib.ConfigDir,
	})
	if err != nil {
		t.Fatalf("downloadAdd: %v", err)
	}
	select {
	case <-allocation.started:
	case <-time.After(time.Second):
		t.Fatal("protocol transfer did not start")
	}
	if allocation.probeCtx != manager.TransferContext() {
		t.Fatal("protocol probe did not use the manager transfer lifetime")
	}

	manager.CancelTransfers()
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.WaitTransfers(waitCtx); err != nil {
		t.Fatalf("WaitTransfers: %v", err)
	}
	if pool.HasDownload(result.GID) {
		t.Fatal("cancelled protocol transfer remained active")
	}
	if critical := pool.GetError(result.GID); critical != nil {
		t.Fatalf("cancelled protocol transfer recorded a critical error: %+v", critical)
	}
	item := manager.GetItem(result.GID)
	if item == nil {
		t.Fatal("shutdown cancellation purged protocol resume state")
	}
	if err := item.CloseDownloader(); err != nil {
		t.Fatalf("CloseDownloader: %v", err)
	}
}

func TestRPCQueuedProtocolRejectsPasswordAndPersistsUsernameOnly(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	manager.SetMaxConcurrentDownloads(1, nil)
	manager.GetQueue().Pause()

	router := warplib.NewSchemeRouter(http.DefaultClient)
	var factoryCalls atomic.Int32
	router.Register("sftp", func(string, *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		factoryCalls.Add(1)
		return &mockProtocolDownloader{
			hash:          "rpc-queued-sftp",
			fileName:      "archive.bin",
			downloadDir:   warplib.ConfigDir,
			contentLength: 1024,
			doneCh:        make(chan struct{}, 1),
		}, nil
	})
	rs := &RPCServer{
		manager:      manager,
		client:       http.DefaultClient,
		pool:         pool,
		schemeRouter: router,
	}

	_, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: "sftp://alice:secret@example.invalid/archive.bin",
		Dir: warplib.ConfigDir,
	})
	if err == nil || !strings.Contains(err.Error(), "protocol secrets are not persisted") {
		t.Fatalf("password-bearing queued SFTP error = %v", err)
	}
	if factoryCalls.Load() != 0 {
		t.Fatal("password-bearing queued SFTP reached the protocol factory")
	}

	result, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: "sftp://alice@example.invalid/archive.bin",
		Dir: warplib.ConfigDir,
	})
	if err != nil {
		t.Fatalf("username-only queued SFTP: %v", err)
	}
	item := manager.GetItem(result.GID)
	if item == nil {
		t.Fatal("username-only queued SFTP was not persisted")
	}
	snapshot := item.Snapshot()
	if strings.Contains(snapshot.URL, "alice@") {
		t.Fatalf("persisted URL retained userinfo: %q", snapshot.URL)
	}
	if snapshot.TransferConfig.ProtocolUsername != "alice" ||
		snapshot.TransferConfig.ProtocolCredentialsRequired {
		t.Fatalf("protocol reconstruction config = %+v", snapshot.TransferConfig)
	}
	if item.IsDownloading() || !manager.GetQueue().IsWaiting(result.GID) {
		t.Fatal("queued protocol retained a live probed downloader")
	}
}

func TestRPCDownloadPauseDefersQueueAndPoolReleaseUntilWorkerExit(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	started := make(chan string, 2)
	manager.SetMaxConcurrentDownloads(1, func(hash string) { started <- hash })

	router := warplib.NewSchemeRouter(http.DefaultClient)
	var sequence atomic.Int32
	router.Register("ftp", func(_ string, opts *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		n := sequence.Add(1)
		return &mockProtocolDownloader{
			hash:          "rpc-ftp-" + string(rune('0'+n)),
			fileName:      "file.bin",
			downloadDir:   opts.DownloadDirectory,
			contentLength: 1024,
			doneCh:        make(chan struct{}, 1),
		}, nil
	})
	rs := &RPCServer{
		manager:      manager,
		client:       http.DefaultClient,
		pool:         pool,
		schemeRouter: router,
	}

	first, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: "ftp://example.invalid/first.bin",
		Dir: filepath.Join(warplib.ConfigDir, "downloads"),
	})
	if err != nil {
		t.Fatalf("first downloadAdd: %v", err)
	}
	if got := <-started; got != first.GID {
		t.Fatalf("first queue start = %q, want %q", got, first.GID)
	}
	second, err := rs.downloadAdd(t.Context(), &AddParams{
		URL: "ftp://example.invalid/second.bin",
		Dir: filepath.Join(warplib.ConfigDir, "downloads"),
	})
	if err != nil {
		t.Fatalf("second downloadAdd: %v", err)
	}
	if !manager.GetQueue().IsWaiting(second.GID) {
		t.Fatal("second RPC download did not wait for a slot")
	}

	if _, err := rs.downloadPause(t.Context(), &GIDParam{GID: first.GID}); err != nil {
		t.Fatalf("downloadPause: %v", err)
	}
	select {
	case got := <-started:
		t.Fatalf("pause promoted %q before the old worker exited", got)
	default:
	}
	if !manager.GetQueue().IsActive(first.GID) || !manager.GetQueue().IsWaiting(second.GID) {
		t.Fatalf("queue changed before worker exit: first active=%v second waiting=%v",
			manager.GetQueue().IsActive(first.GID), manager.GetQueue().IsWaiting(second.GID))
	}
	if !pool.HasDownload(first.GID) {
		t.Fatal("pause removed pool generation before worker exit")
	}
	if _, err := rs.downloadResume(t.Context(), &GIDParam{GID: first.GID}); err == nil {
		t.Fatal("resume overlapped a worker that was still stopping")
	}

	// Model the manager-patched stopped callback that runs as the old
	// downloader exits.
	manager.GetQueue().OnStopped(first.GID)
	if got := <-started; got != second.GID {
		t.Fatalf("promoted queue start = %q, want %q", got, second.GID)
	}
	if manager.GetQueue().IsActive(first.GID) || !manager.GetQueue().IsActive(second.GID) {
		t.Fatalf("queue state after pause: first active=%v second active=%v",
			manager.GetQueue().IsActive(first.GID), manager.GetQueue().IsActive(second.GID))
	}
	pool.BroadcastTerminal(first.GID, []byte("stopped"))
	if pool.HasDownload(first.GID) {
		t.Fatal("worker terminal callback retained the pool generation")
	}
}

func TestRPCDownloadPauseWaitingCloseErrorStillCleansPool(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	manager.SetMaxConcurrentDownloads(1, nil)
	queue := manager.GetQueue()
	queue.Pause()

	closeErr := errors.New("injected close failure")
	downloader := &closeFailingRPCProtocol{
		mockProtocolDownloader: &mockProtocolDownloader{
			hash:          "rpc-waiting-close-error",
			fileName:      "waiting.bin",
			downloadDir:   warplib.ConfigDir,
			contentLength: 1024,
			doneCh:        make(chan struct{}, 1),
		},
		closeErr: closeErr,
	}
	handlers := &warplib.Handlers{}
	if err := manager.AddProtocolDownload(
		downloader,
		warplib.ProbeResult{
			ContentLength: 1024,
			FileName:      "waiting.bin",
			Resumable:     true,
		},
		"ftp://example.invalid/waiting.bin",
		warplib.ProtoFTP,
		handlers,
		&warplib.AddDownloadOpts{
			AbsoluteLocation: warplib.ConfigDir,
			SkipQueue:        true,
		},
	); err != nil {
		t.Fatalf("AddProtocolDownload: %v", err)
	}
	queue.Add(downloader.GetHash(), warplib.PriorityNormal)
	pool.AddDownload(downloader.GetHash(), nil)

	_, err := (&RPCServer{
		manager: manager,
		pool:    pool,
	}).downloadPause(t.Context(), &GIDParam{GID: downloader.GetHash()})
	if err == nil || !strings.Contains(err.Error(), closeErr.Error()) {
		t.Fatalf("downloadPause error = %v, want close failure", err)
	}
	if queue.IsWaiting(downloader.GetHash()) || queue.IsActive(downloader.GetHash()) {
		t.Fatal("waiting item retained queue membership after pause")
	}
	if pool.HasDownload(downloader.GetHash()) {
		t.Fatal("waiting item retained pool membership after close failure")
	}
	item := manager.GetItem(downloader.GetHash())
	if item == nil {
		t.Fatal("paused item was unexpectedly removed from history")
	}
	if item.IsDownloading() {
		t.Fatal("paused item retained its failed downloader allocation")
	}
}

func TestRPCDownloadResumeHonorsPausedQueue(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	var starts atomic.Int32
	manager.SetMaxConcurrentDownloads(1, func(string) { starts.Add(1) })
	manager.GetQueue().Pause()

	router := warplib.NewSchemeRouter(http.DefaultClient)
	mock := &mockProtocolDownloader{
		hash:          "paused-resume-live",
		fileName:      "resume.bin",
		downloadDir:   warplib.ConfigDir,
		contentLength: 1024,
		doneCh:        make(chan struct{}, 1),
	}
	router.Register("ftp", func(string, *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		return mock, nil
	})
	manager.SetSchemeRouter(router)

	const hash = "paused-resume"
	manager.UpdateItem(&warplib.Item{
		Hash:             hash,
		Name:             "resume.bin",
		Url:              "ftp://example.invalid/resume.bin",
		Protocol:         warplib.ProtoFTP,
		Resumable:        true,
		TotalSize:        1024,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Parts:            make(map[int64]*warplib.ItemPart),
	})
	rs := &RPCServer{
		manager:      manager,
		client:       http.DefaultClient,
		pool:         pool,
		schemeRouter: router,
	}

	if _, err := rs.downloadResume(t.Context(), &GIDParam{GID: hash}); err != nil {
		t.Fatalf("downloadResume: %v", err)
	}
	if starts.Load() != 0 || atomic.LoadInt32(&mock.progressCalled) != 0 {
		t.Fatal("paused queue started resumed RPC transfer")
	}
	if !manager.GetQueue().IsWaiting(hash) {
		t.Fatal("resumed RPC transfer was not left waiting")
	}
	if item := manager.GetItem(hash); item == nil || item.IsDownloading() {
		t.Fatalf("waiting resumed item = %v, live downloader = %v", item, item != nil && item.IsDownloading())
	}
	if !pool.HasDownload(hash) {
		t.Fatal("waiting resumed item was removed from pool")
	}
}

func TestRPCDownloadResumeAdmissionRejectionPreservesExistingItem(t *testing.T) {
	manager, pool := newRPCQueueTestManager(t)
	router := warplib.NewSchemeRouter(http.DefaultClient)
	allocation := &mockProtocolDownloader{
		hash:          "rejected-resume-allocation",
		fileName:      "resume.bin",
		downloadDir:   warplib.ConfigDir,
		contentLength: 1024,
		doneCh:        make(chan struct{}, 1),
	}
	router.Register("ftp", func(string, *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		return allocation, nil
	})
	manager.SetSchemeRouter(router)

	const hash = "rejected-resume"
	manager.UpdateItem(&warplib.Item{
		Hash:             hash,
		Name:             "resume.bin",
		Url:              "ftp://example.invalid/resume.bin",
		Protocol:         warplib.ProtoFTP,
		Resumable:        true,
		TotalSize:        1024,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Parts:            make(map[int64]*warplib.ItemPart),
	})
	rs := &RPCServer{
		manager:      manager,
		client:       http.DefaultClient,
		pool:         pool,
		schemeRouter: router,
		transferLauncher: func(func(context.Context)) bool {
			return false
		},
	}

	_, err := rs.downloadResume(t.Context(), &GIDParam{GID: hash})
	if err == nil || !strings.Contains(err.Error(), warplib.ErrManagerShuttingDown.Error()) {
		t.Fatalf("downloadResume error = %v, want manager shutdown", err)
	}
	item := manager.GetItem(hash)
	if item == nil {
		t.Fatal("admission rejection purged existing resume state")
	}
	if item.IsDownloading() {
		t.Fatal("admission rejection retained reconstructed allocation")
	}
	if pool.HasDownload(hash) {
		t.Fatal("admission rejection retained pool registration")
	}
	if critical := pool.GetError(hash); critical != nil {
		t.Fatalf("admission rejection recorded a critical error: %+v", critical)
	}
	if !allocation.stopped {
		t.Fatal("admission rejection did not stop its exact allocation")
	}
}
