package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type lifecycleProtocolDownloader struct {
	hash        string
	fileName    string
	downloadDir string
	probeResult warplib.ProbeResult

	mu       sync.Mutex
	probeCtx context.Context

	downloadFn func(context.Context, *warplib.Handlers) error
	resumeFn   func(context.Context, map[int64]*warplib.ItemPart, *warplib.Handlers) error
	closeFn    func() error
	stopFn     func()

	closed        atomic.Bool
	stopped       atomic.Bool
	downloadCalls atomic.Int32
	resumeCalls   atomic.Int32
}

func (d *lifecycleProtocolDownloader) Probe(ctx context.Context) (warplib.ProbeResult, error) {
	d.mu.Lock()
	d.probeCtx = ctx
	d.mu.Unlock()
	return d.probeResult, nil
}

func (d *lifecycleProtocolDownloader) Download(ctx context.Context, handlers *warplib.Handlers) error {
	d.downloadCalls.Add(1)
	if d.downloadFn != nil {
		return d.downloadFn(ctx, handlers)
	}
	return nil
}

func (d *lifecycleProtocolDownloader) Resume(
	ctx context.Context,
	parts map[int64]*warplib.ItemPart,
	handlers *warplib.Handlers,
) error {
	d.resumeCalls.Add(1)
	if d.resumeFn != nil {
		return d.resumeFn(ctx, parts, handlers)
	}
	return nil
}

func (d *lifecycleProtocolDownloader) Capabilities() warplib.DownloadCapabilities {
	return warplib.DownloadCapabilities{SupportsResume: true}
}

func (d *lifecycleProtocolDownloader) Close() error {
	d.closed.Store(true)
	if d.closeFn != nil {
		return d.closeFn()
	}
	return nil
}

func (d *lifecycleProtocolDownloader) Stop() {
	d.stopped.Store(true)
	if d.stopFn != nil {
		d.stopFn()
	}
}

func (d *lifecycleProtocolDownloader) IsStopped() bool {
	return d.stopped.Load()
}

func (d *lifecycleProtocolDownloader) GetMaxConnections() int32 {
	return 1
}

func (d *lifecycleProtocolDownloader) GetMaxParts() int32 {
	return 1
}

func (d *lifecycleProtocolDownloader) GetHash() string {
	return d.hash
}

func (d *lifecycleProtocolDownloader) GetFileName() string {
	return d.fileName
}

func (d *lifecycleProtocolDownloader) GetDownloadDirectory() string {
	return d.downloadDir
}

func (d *lifecycleProtocolDownloader) GetSavePath() string {
	return filepath.Join(d.downloadDir, d.fileName)
}

func (d *lifecycleProtocolDownloader) GetContentLength() warplib.ContentLength {
	return warplib.ContentLength(d.probeResult.ContentLength)
}

func (d *lifecycleProtocolDownloader) capturedProbeContext() context.Context {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.probeCtx
}

func TestApiBeginShutdownLeavesManagerTransferOrderingToDaemon(t *testing.T) {
	api, _, cleanup := newTestApi(t)
	defer cleanup()

	managerCtx := api.manager.TransferContext()
	api.BeginShutdown()
	api.BeginShutdown()

	select {
	case <-api.daemonCtx().Done():
	default:
		t.Fatal("BeginShutdown did not cancel API background context")
	}
	if err := managerCtx.Err(); err != nil {
		t.Fatalf("BeginShutdown canceled Manager transfers before queue quiesce: %v", err)
	}

	run := make(chan struct{})
	if !api.manager.GoTransfer(func(context.Context) {
		close(run)
	}) {
		t.Fatal("BeginShutdown unexpectedly closed Manager transfer admission")
	}
	select {
	case <-run:
	case <-time.After(time.Second):
		t.Fatal("admitted transfer did not run")
	}
}

func TestProtocolDownloadAdmissionRejectionCleansRegistration(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	downloader := &lifecycleProtocolDownloader{
		hash:        "rejected-admission",
		fileName:    "archive.tar",
		downloadDir: warplib.ConfigDir,
		probeResult: warplib.ProbeResult{
			FileName:      "archive.tar",
			ContentLength: 32,
			Resumable:     true,
		},
	}
	generation, reserved := pool.BeginDownload(downloader.hash, nil)
	if !reserved {
		t.Fatal("failed to reserve transfer generation")
	}
	if err := api.manager.AddProtocolDownload(
		downloader,
		downloader.probeResult,
		"ftp://example.invalid/archive.tar",
		warplib.ProtoFTP,
		&warplib.Handlers{},
		&warplib.AddDownloadOpts{
			AbsoluteLocation: downloader.downloadDir,
			SkipQueue:        true,
		},
	); err != nil {
		t.Fatalf("AddProtocolDownload: %v", err)
	}
	runLease, err := api.manager.AcquireProtocolRunLease(
		downloader.hash,
		downloader,
	)
	if err != nil {
		t.Fatalf("AcquireProtocolRunLease: %v", err)
	}

	// Close admission only after the exact allocation has been claimed. This
	// deterministically exercises the rejection path between acquisition and
	// GoTransfer without exposing a production test hook.
	api.manager.CancelTransfers()
	err = api.launchInitialRunLease(generation, downloader.hash, runLease)
	if !errors.Is(err, warplib.ErrManagerShuttingDown) {
		t.Fatalf("launchInitialRunLease error = %v, want manager shutting down", err)
	}
	if downloader.downloadCalls.Load() != 0 {
		t.Fatal("rejected transfer callback ran")
	}
	if !downloader.closed.Load() {
		t.Fatal("rejected transfer allocation was not closed")
	}
	if api.manager.GetItem(downloader.hash) != nil {
		t.Fatal("rejected transfer registration remains in Manager")
	}
	if pool.HasDownload(downloader.hash) {
		t.Fatal("rejected transfer generation remains in pool")
	}
}

func TestProtocolAddShutdownRejectionPreservesSameHashReplacement(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	const hash = "rejected-protocol-add-aba"
	replacement := &warplib.Item{
		Hash:             hash,
		Name:             "replacement.tar",
		Url:              "ftp://example.invalid/replacement.tar",
		TotalSize:        10,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Protocol:         warplib.ProtoFTP,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(replacement)

	local := &lifecycleProtocolDownloader{
		hash:        hash,
		fileName:    "rejected.tar",
		downloadDir: warplib.ConfigDir,
		probeResult: warplib.ProbeResult{
			FileName:      "rejected.tar",
			ContentLength: 32,
			Resumable:     true,
		},
	}
	router := warplib.NewSchemeRouter(nil)
	router.Register("ftp", func(string, *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		return local, nil
	})
	api.schemeRouter = router

	// WaitTransfers closes registration admission without cancelling the
	// shared transfer context, allowing construction/probe to finish and the
	// Manager Add call itself to reject deterministically.
	if err := api.manager.WaitTransfers(context.Background()); err != nil {
		t.Fatalf("WaitTransfers: %v", err)
	}
	params := &common.DownloadParams{
		Url:               "ftp://example.invalid/rejected.tar",
		DownloadDirectory: warplib.ConfigDir,
	}
	_, _, err := api.downloadProtocolHandler(
		nil,
		pool,
		params.Url,
		"ftp",
		params,
	)
	if !errors.Is(err, warplib.ErrManagerShuttingDown) {
		t.Fatalf("protocol Add rejection = %v, want %v",
			err, warplib.ErrManagerShuttingDown)
	}
	if !local.closed.Load() {
		t.Fatal("unpublished protocol downloader was not closed")
	}
	if got := api.manager.GetItem(hash); got != replacement {
		t.Fatal("protocol Add rejection purged the same-hash replacement")
	}
	if pool.HasDownload(hash) {
		t.Fatal("rejected protocol pool generation was not aborted")
	}
}

func TestHTTPAddShutdownRejectionAbortsOnlyUnpublishedGeneration(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	content := []byte("http registration rejection")
	httpServer := newRangeServer(content)
	defer httpServer.Close()

	beforeEntries, err := os.ReadDir(warplib.DlDataDir)
	if err != nil {
		t.Fatalf("ReadDir before: %v", err)
	}
	before := make(map[string]struct{}, len(beforeEntries))
	for _, entry := range beforeEntries {
		before[entry.Name()] = struct{}{}
	}
	if err := api.manager.WaitTransfers(context.Background()); err != nil {
		t.Fatalf("WaitTransfers: %v", err)
	}
	params := &common.DownloadParams{
		Url:               httpServer.URL + "/rejected.bin",
		FileName:          "rejected.bin",
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    1,
		MaxSegments:       1,
	}
	_, _, err = api.downloadHTTPHandler(
		nil,
		pool,
		params.Url,
		params,
		nil,
		true,
	)
	if !errors.Is(err, warplib.ErrManagerShuttingDown) {
		t.Fatalf("HTTP Add rejection = %v, want %v",
			err, warplib.ErrManagerShuttingDown)
	}

	afterEntries, err := os.ReadDir(warplib.DlDataDir)
	if err != nil {
		t.Fatalf("ReadDir after: %v", err)
	}
	var localHash string
	for _, entry := range afterEntries {
		if _, existed := before[entry.Name()]; !existed && entry.IsDir() {
			if localHash != "" {
				t.Fatalf("multiple local registration directories: %q and %q",
					localHash, entry.Name())
			}
			localHash = entry.Name()
		}
	}
	if localHash == "" {
		t.Fatal("HTTP construction did not expose its local registration hash")
	}
	if pool.HasDownload(localHash) {
		t.Fatal("rejected HTTP pool generation was not aborted")
	}
	if item := api.manager.GetItem(localHash); item != nil {
		t.Fatal("rejected HTTP downloader was published to Manager")
	}
}

func TestProtocolDownloadRunClaimDrainsBeforeReconstructionClosesAllocation(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	started := make(chan struct{})
	stopped := make(chan struct{})
	releaseRun := make(chan struct{})
	returned := make(chan struct{})
	var (
		startOnce          sync.Once
		stopOnce           sync.Once
		returnOnce         sync.Once
		releaseOnce        sync.Once
		closedWhileRunning atomic.Bool
		optsCtx            context.Context
		downloader         *lifecycleProtocolDownloader
	)
	defer releaseOnce.Do(func() {
		close(releaseRun)
	})

	router := warplib.NewSchemeRouter(&http.Client{})
	router.Register("ftp", func(_ string, opts *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		optsCtx = opts.Context
		downloader = &lifecycleProtocolDownloader{
			hash:        "run-claim-reconstruction",
			fileName:    "archive.tar",
			downloadDir: warplib.ConfigDir,
			probeResult: warplib.ProbeResult{
				FileName:      "archive.tar",
				ContentLength: 32,
				Resumable:     true,
			},
			downloadFn: func(context.Context, *warplib.Handlers) error {
				startOnce.Do(func() {
					close(started)
				})
				<-stopped
				<-releaseRun
				returnOnce.Do(func() {
					close(returned)
				})
				return nil
			},
			stopFn: func() {
				stopOnce.Do(func() {
					close(stopped)
				})
			},
			closeFn: func() error {
				select {
				case <-returned:
				default:
					closedWhileRunning.Store(true)
				}
				return nil
			},
		}
		return downloader, nil
	})
	api.schemeRouter = router

	_, response, err := api.downloadProtocolHandler(
		nil,
		pool,
		"ftp://example.invalid/archive.tar",
		"ftp",
		&common.DownloadParams{DownloadDirectory: warplib.ConfigDir},
	)
	if err != nil {
		t.Fatalf("downloadProtocolHandler: %v", err)
	}
	hash := response.(*common.DownloadResponse).DownloadId
	if optsCtx == nil {
		t.Fatal("DownloaderOpts.Context was not set")
	}
	if probeCtx := downloader.capturedProbeContext(); probeCtx != optsCtx {
		t.Fatal("Probe did not receive the Manager transfer context")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("protocol transfer did not start")
	}

	type reconstructionResult struct {
		lease *warplib.ReconstructionLease
		err   error
	}
	reconstructed := make(chan reconstructionResult, 1)
	go func() {
		lease, reconstructionErr := api.manager.BeginReconstruction(hash)
		reconstructed <- reconstructionResult{
			lease: lease,
			err:   reconstructionErr,
		}
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("reconstruction did not stop the active allocation")
	}
	select {
	case result := <-reconstructed:
		t.Fatalf("reconstruction returned before active run drained: %v", result.err)
	default:
	}
	if downloader.closed.Load() {
		t.Fatal("reconstruction closed the allocation while Download was active")
	}
	if closedWhileRunning.Load() {
		t.Fatal("allocation Close observed Download still running")
	}

	releaseOnce.Do(func() {
		close(releaseRun)
	})
	var result reconstructionResult
	select {
	case result = <-reconstructed:
	case <-time.After(time.Second):
		t.Fatal("reconstruction did not finish after active run returned")
	}
	if result.err != nil {
		t.Fatalf("BeginReconstruction: %v", result.err)
	}
	if result.lease == nil || !result.lease.IsCurrent() {
		t.Fatal("reconstruction did not retain its exact generation")
	}
	if !downloader.closed.Load() {
		t.Fatal("reconstruction did not close the drained allocation")
	}
	if closedWhileRunning.Load() {
		t.Fatal("allocation was closed before Download returned")
	}
}

func TestShutdownCancellationDoesNotReportCriticalOrPurge(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	started := make(chan struct{})
	router := warplib.NewSchemeRouter(&http.Client{})
	var downloader *lifecycleProtocolDownloader
	router.Register("ftp", func(_ string, _ *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		downloader = &lifecycleProtocolDownloader{
			hash:        "shutdown-cancellation",
			fileName:    "archive.tar",
			downloadDir: warplib.ConfigDir,
			probeResult: warplib.ProbeResult{
				FileName:      "archive.tar",
				ContentLength: 32,
				Resumable:     true,
			},
			downloadFn: func(ctx context.Context, handlers *warplib.Handlers) error {
				close(started)
				<-ctx.Done()
				if handlers != nil && handlers.ErrorHandler != nil {
					handlers.ErrorHandler("shutdown-cancellation", ctx.Err())
				}
				return ctx.Err()
			},
		}
		return downloader, nil
	})
	api.schemeRouter = router

	_, response, err := api.downloadProtocolHandler(
		nil,
		pool,
		"ftp://example.invalid/archive.tar",
		"ftp",
		&common.DownloadParams{DownloadDirectory: warplib.ConfigDir},
	)
	if err != nil {
		t.Fatalf("downloadProtocolHandler: %v", err)
	}
	hash := response.(*common.DownloadResponse).DownloadId
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tracked protocol transfer did not start")
	}

	api.manager.CancelTransfers()
	deadline := time.Now().Add(time.Second)
	for pool.HasDownload(hash) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if pool.HasDownload(hash) {
		t.Fatal("shutdown-cancelled generation was not finalized")
	}
	if critical := pool.GetError(hash); critical != nil {
		t.Fatalf("shutdown cancellation was reported as critical: %+v", critical)
	}
	if api.manager.GetItem(hash) == nil {
		t.Fatal("shutdown cancellation purged resumable Manager state")
	}
}

func TestResumePartialFailureClosesOnlyExactParentLease(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	parent := &warplib.Item{
		Hash:             "resume-parent",
		Name:             "parent.bin",
		Url:              "ftp://example.invalid/parent.bin",
		TotalSize:        5,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		ChildHash:        "resume-child",
		Resumable:        true,
		Protocol:         warplib.ProtoFTP,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	child := &warplib.Item{
		Hash:             "resume-child",
		Name:             "child.bin",
		Url:              "ftp://example.invalid/child.bin",
		TotalSize:        7,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Protocol:         warplib.ProtoFTP,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(parent)
	api.manager.UpdateItem(child)

	childSetupErr := errors.New("child setup failed")
	router := warplib.NewSchemeRouter(&http.Client{})
	var (
		parentAllocations []*lifecycleProtocolDownloader
		replacementLease  *warplib.ReconstructionLease
	)
	router.Register("ftp", func(rawURL string, _ *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		if strings.Contains(rawURL, "child.bin") {
			var replaceErr error
			_, replacementLease, replaceErr = api.manager.ResumeDownloadWithLease(
				api.client,
				parent.Hash,
				&warplib.ResumeDownloadOpts{},
			)
			if replaceErr != nil {
				return nil, replaceErr
			}
			return nil, childSetupErr
		}
		allocation := &lifecycleProtocolDownloader{
			hash:        parent.Hash,
			fileName:    parent.Name,
			downloadDir: warplib.ConfigDir,
			probeResult: warplib.ProbeResult{
				FileName:      parent.Name,
				ContentLength: 5,
				Resumable:     true,
			},
		}
		parentAllocations = append(parentAllocations, allocation)
		return allocation, nil
	})
	api.manager.SetSchemeRouter(router)

	body, err := json.Marshal(common.ResumeParams{DownloadId: parent.Hash})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, _, err = api.resumeHandler(nil, pool, body)
	if !errors.Is(err, childSetupErr) {
		t.Fatalf("resumeHandler error = %v, want child setup failure", err)
	}
	if len(parentAllocations) != 2 {
		t.Fatalf("parent allocations = %d, want original plus replacement", len(parentAllocations))
	}
	if !parentAllocations[0].closed.Load() {
		t.Fatal("replacement reconstruction did not close original parent allocation")
	}
	if parentAllocations[1].closed.Load() {
		t.Fatal("stale parent cleanup closed newer replacement allocation")
	}
	if replacementLease == nil || !replacementLease.IsCurrent() {
		t.Fatal("newer parent reconstruction lease was disturbed")
	}
	if pool.HasDownload(parent.Hash) || pool.HasDownload(child.Hash) {
		t.Fatal("failed resume left pool generations registered")
	}

	closed, closeErr := replacementLease.Close()
	if closeErr != nil {
		t.Fatalf("close replacement lease: %v", closeErr)
	}
	if !closed {
		t.Fatal("replacement lease no longer owned its allocation")
	}
}
