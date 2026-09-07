package api

import (
	"bytes"
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
	"github.com/warpdl/warpdl/internal/scheduler"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type retainedResponseBody struct {
	*bytes.Reader
	closed atomic.Bool
}

func (b *retainedResponseBody) Close() error {
	b.closed.Store(true)
	return nil
}

type changingRepresentationTransport struct {
	mu       sync.Mutex
	payloads [][]byte
	bodies   []*retainedResponseBody
}

func (t *changingRepresentationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	index := len(t.bodies)
	if index >= len(t.payloads) {
		index = len(t.payloads) - 1
	}
	payload := append([]byte(nil), t.payloads[index]...)
	body := &retainedResponseBody{Reader: bytes.NewReader(payload)}
	t.bodies = append(t.bodies, body)
	t.mu.Unlock()
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        make(http.Header), // no ETag: metadata body is retained
		Body:          body,
		ContentLength: int64(len(payload)),
		Request:       req,
	}, nil
}

func (t *changingRepresentationTransport) snapshotBodies() []*retainedResponseBody {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*retainedResponseBody(nil), t.bodies...)
}

type scheduledProtocolDownloader struct {
	hash            string
	fileName        string
	downloadDir     string
	probed          bool
	closed          bool
	downloadCalls   int
	receivedHandler bool
}

func (d *scheduledProtocolDownloader) Probe(context.Context) (warplib.ProbeResult, error) {
	d.probed = true
	return warplib.ProbeResult{
		FileName:      d.fileName,
		ContentLength: 512,
		Resumable:     true,
	}, nil
}

func (d *scheduledProtocolDownloader) Download(_ context.Context, handlers *warplib.Handlers) error {
	if !d.probed {
		return warplib.ErrProbeRequired
	}
	d.downloadCalls++
	d.receivedHandler = handlers != nil
	if handlers != nil && handlers.DownloadProgressHandler != nil {
		handlers.DownloadProgressHandler(d.hash, 512)
	}
	if handlers != nil && handlers.DownloadCompleteHandler != nil {
		handlers.DownloadCompleteHandler(warplib.MAIN_HASH, 512)
	}
	return nil
}

func (d *scheduledProtocolDownloader) Resume(context.Context, map[int64]*warplib.ItemPart, *warplib.Handlers) error {
	return nil
}

func (d *scheduledProtocolDownloader) Capabilities() warplib.DownloadCapabilities {
	return warplib.DownloadCapabilities{SupportsResume: true}
}

func (d *scheduledProtocolDownloader) Close() error                 { d.closed = true; return nil }
func (d *scheduledProtocolDownloader) Stop()                        {}
func (d *scheduledProtocolDownloader) IsStopped() bool              { return false }
func (d *scheduledProtocolDownloader) GetMaxConnections() int32     { return 1 }
func (d *scheduledProtocolDownloader) GetMaxParts() int32           { return 1 }
func (d *scheduledProtocolDownloader) GetHash() string              { return d.hash }
func (d *scheduledProtocolDownloader) GetFileName() string          { return d.fileName }
func (d *scheduledProtocolDownloader) GetDownloadDirectory() string { return d.downloadDir }
func (d *scheduledProtocolDownloader) GetSavePath() string {
	return filepath.Join(d.downloadDir, d.fileName)
}
func (d *scheduledProtocolDownloader) GetContentLength() warplib.ContentLength { return 512 }

func TestDownloadProtocolHandlerRegistersScheduleAndDefersStart(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()
	router := warplib.NewSchemeRouter(&http.Client{})
	var protocolDownloaders []*scheduledProtocolDownloader
	router.Register("ftp", func(string, *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		protocolDownloader := &scheduledProtocolDownloader{
			hash:        "scheduled-ftp",
			fileName:    "archive.tar",
			downloadDir: warplib.ConfigDir,
		}
		protocolDownloaders = append(protocolDownloaders, protocolDownloader)
		return protocolDownloader, nil
	})
	api.schemeRouter = router
	api.manager.SetSchemeRouter(router)

	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	defer cancelScheduler()
	triggered := make(chan string, 1)
	api.scheduler = scheduler.New(schedulerCtx, func(hash string) {
		triggered <- hash
	})

	params := common.DownloadParams{
		Url:               "ftp://example.invalid/archive.tar",
		DownloadDirectory: warplib.ConfigDir,
		StartIn:           "50ms",
	}
	body, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, response, err := api.downloadHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("downloadHandler: %v", err)
	}
	if len(protocolDownloaders) != 1 || protocolDownloaders[0].downloadCalls != 0 {
		t.Fatal("scheduled FTP download started immediately")
	}
	downloadResponse := response.(*common.DownloadResponse)
	item := api.manager.GetItem(downloadResponse.DownloadId)
	if item == nil {
		t.Fatal("scheduled FTP item was not persisted")
	}
	snapshot := item.Snapshot()
	if snapshot.Protocol != warplib.ProtoFTP ||
		snapshot.ScheduleState != warplib.ScheduleStateScheduled ||
		snapshot.ScheduledAt.IsZero() {
		t.Fatalf("persisted FTP schedule = %+v", snapshot)
	}
	if !protocolDownloaders[0].closed || item.IsDownloading() {
		t.Fatal("scheduled FTP retained its probed live downloader")
	}

	select {
	case hash := <-triggered:
		if hash != downloadResponse.DownloadId {
			t.Fatalf("triggered hash = %q, want %q", hash, downloadResponse.DownloadId)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FTP schedule was not registered with scheduler")
	}

	// The daemon trigger Fresh-reconstructs even without a process restart,
	// so metadata and remote size are probed at the actual trigger time.
	item, err = api.manager.ResumeDownload(api.client, item.Hash, &warplib.ResumeDownloadOpts{Fresh: true})
	if err != nil {
		t.Fatalf("reconstruct scheduled protocol item: %v", err)
	}
	if err := item.Start(); err != nil {
		t.Fatalf("start reconstructed protocol item: %v", err)
	}
	if len(protocolDownloaders) != 2 ||
		protocolDownloaders[1].downloadCalls != 1 ||
		!protocolDownloaders[1].receivedHandler {
		t.Fatalf("fresh protocol instances=%d calls=%d handlers=%v",
			len(protocolDownloaders),
			protocolDownloaders[len(protocolDownloaders)-1].downloadCalls,
			protocolDownloaders[len(protocolDownloaders)-1].receivedHandler)
	}
	if item.GetDownloaded() != 512 {
		t.Fatalf("manager progress after scheduled start = %d, want 512", item.GetDownloaded())
	}
}

func TestScheduledAuthenticatedProxyIsRejectedWithoutPersistingSecret(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()
	params := &common.DownloadParams{
		Proxy:             "http://proxy-user:proxy-secret@proxy.invalid:8080",
		DownloadDirectory: warplib.ConfigDir,
		StartIn:           "1h",
	}
	_, _, err := api.downloadHTTPHandler(
		nil,
		pool,
		"http://origin.invalid/file.bin",
		params,
		nil,
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "proxy secrets are not persisted") {
		t.Fatalf("scheduled authenticated proxy error = %v", err)
	}
	if len(api.manager.GetItems()) != 0 {
		t.Fatal("rejected authenticated-proxy schedule was persisted")
	}

	api.manager.SetMaxConcurrentDownloads(1, nil)
	params.StartIn = ""
	_, _, err = api.downloadHTTPHandler(
		nil,
		pool,
		"http://origin.invalid/file.bin",
		params,
		nil,
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "proxy secrets are not persisted") {
		t.Fatalf("queued authenticated proxy error = %v", err)
	}
	if len(api.manager.GetItems()) != 0 {
		t.Fatal("rejected authenticated-proxy queue item was persisted")
	}
}

func TestQueuedProtocolPasswordIsRejectedButUsernameOnlyIsSafe(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()
	api.manager.SetMaxConcurrentDownloads(1, nil)
	api.manager.GetQueue().Pause()

	router := warplib.NewSchemeRouter(&http.Client{})
	factoryCalls := 0
	router.Register("sftp", func(string, *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
		factoryCalls++
		return &scheduledProtocolDownloader{
			hash:        "queued-sftp",
			fileName:    "archive.tar",
			downloadDir: warplib.ConfigDir,
		}, nil
	})
	api.schemeRouter = router

	passwordParams := &common.DownloadParams{DownloadDirectory: warplib.ConfigDir}
	_, _, err := api.downloadProtocolHandler(
		nil,
		pool,
		"sftp://alice:secret@example.invalid/archive.tar",
		"sftp",
		passwordParams,
	)
	if err == nil || !strings.Contains(err.Error(), "protocol secrets are not persisted") {
		t.Fatalf("queued password URL error = %v", err)
	}
	if factoryCalls != 0 {
		t.Fatal("password-bearing queued URL reached the protocol factory")
	}

	usernameParams := &common.DownloadParams{DownloadDirectory: warplib.ConfigDir}
	_, response, err := api.downloadProtocolHandler(
		nil,
		pool,
		"sftp://alice@example.invalid/archive.tar",
		"sftp",
		usernameParams,
	)
	if err != nil {
		t.Fatalf("username-only queued URL: %v", err)
	}
	item := api.manager.GetItem(response.(*common.DownloadResponse).DownloadId)
	if item == nil {
		t.Fatal("username-only queued item was not persisted")
	}
	snapshot := item.Snapshot()
	if strings.Contains(snapshot.URL, "alice@") {
		t.Fatalf("public/persisted URL retained userinfo: %q", snapshot.URL)
	}
	if snapshot.TransferConfig.ProtocolUsername != "alice" ||
		snapshot.TransferConfig.ProtocolCredentialsRequired {
		t.Fatalf("username reconstruction config = %+v", snapshot.TransferConfig)
	}
	if item.IsDownloading() || !api.manager.GetQueue().IsWaiting(item.Hash) {
		t.Fatal("waiting username-only protocol retained its probed downloader")
	}
}

func TestResolveDownloadScheduleRejectsPastStart(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.Local)
	_, _, _, err := resolveDownloadSchedule(&common.DownloadParams{
		StartAt: now.Add(-time.Minute).Format("2006-01-02 15:04"),
	}, now)
	if err == nil || !strings.Contains(err.Error(), "must be in the future") {
		t.Fatalf("resolveDownloadSchedule error = %v", err)
	}
}

func TestResolveDownloadScheduleSupportsStartIn(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.Local)
	start, cron, scheduled, err := resolveDownloadSchedule(&common.DownloadParams{
		StartIn: "90m",
	}, now)
	if err != nil {
		t.Fatalf("resolveDownloadSchedule: %v", err)
	}
	if !scheduled || cron != "" || !start.Equal(now.Add(90*time.Minute)) {
		t.Fatalf("resolved schedule = (%v, %q, %v)", start, cron, scheduled)
	}
}

func TestRecurringAsyncFailureKeepsFutureSchedule(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()
	api.manager.UpdateItem(&warplib.Item{
		Hash:  "recurring-failure",
		Name:  "backup.bin",
		Url:   "https://example.invalid/backup.bin",
		Parts: make(map[int64]*warplib.ItemPart),
	})
	if err := api.manager.ConfigureSchedule(
		"recurring-failure",
		time.Now().Add(time.Hour),
		"0 2 * * *",
		warplib.ScheduleStateScheduled,
	); err != nil {
		t.Fatalf("ConfigureSchedule: %v", err)
	}

	ReportAsyncDownloadError(pool, "recurring-failure", errors.New("temporary failure"), api.manager)
	if api.manager.GetItem("recurring-failure") == nil {
		t.Fatal("zero-byte recurring failure purged the future schedule")
	}
}

func TestScheduledHTTPClosesValidatorlessProbeAndFreshlyReconstructs(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	initial := []byte("metadata-time-body")
	atTrigger := []byte("larger trigger-time representation")
	transport := &changingRepresentationTransport{payloads: [][]byte{initial, atTrigger}}
	api.client = &http.Client{Transport: transport}

	params := &common.DownloadParams{
		DownloadDirectory: warplib.ConfigDir,
		StartIn:           "1h",
	}
	_, response, err := api.downloadHTTPHandler(
		nil,
		pool,
		"http://representation.test/scheduled.bin",
		params,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("downloadHTTPHandler: %v", err)
	}
	downloadResponse := response.(*common.DownloadResponse)
	item := api.manager.GetItem(downloadResponse.DownloadId)
	if item == nil {
		t.Fatal("scheduled item was not persisted")
	}
	bodies := transport.snapshotBodies()
	if len(bodies) != 1 || !bodies[0].closed.Load() {
		t.Fatalf("metadata bodies = %d, first closed = %v", len(bodies), len(bodies) == 1 && bodies[0].closed.Load())
	}
	if item.IsDownloading() {
		t.Fatal("scheduled item retained a live downloader")
	}
	if !pool.HasDownload(downloadResponse.DownloadId) {
		t.Fatal("scheduled item was removed from the attach pool")
	}

	reconstructed, err := api.manager.ResumeDownload(api.client, downloadResponse.DownloadId, &warplib.ResumeDownloadOpts{
		Fresh: true,
	})
	if err != nil {
		t.Fatalf("Fresh reconstruction: %v", err)
	}
	if err := reconstructed.Start(); err != nil {
		t.Fatalf("Start reconstructed transfer: %v", err)
	}
	bodies = transport.snapshotBodies()
	if len(bodies) != 2 || !bodies[1].closed.Load() {
		t.Fatalf("trigger bodies = %d, second closed = %v", len(bodies), len(bodies) == 2 && bodies[1].closed.Load())
	}
	got, err := os.ReadFile(downloadResponse.SavePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, atTrigger) {
		t.Fatalf("downloaded body = %q, want trigger-time representation %q", got, atTrigger)
	}
}

func TestWaitingHTTPClosesValidatorlessProbeAndFreshlyReconstructsOnPromotion(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	initial := []byte("queue-time-body")
	atPromotion := []byte("fresh body when queue slot opens")
	transport := &changingRepresentationTransport{payloads: [][]byte{initial, atPromotion}}
	api.client = &http.Client{Transport: transport}

	startResult := make(chan error, 1)
	api.manager.SetMaxConcurrentDownloads(1, func(hash string) {
		item, err := api.manager.ResumeDownload(api.client, hash, &warplib.ResumeDownloadOpts{Fresh: true})
		if err == nil {
			err = item.Start()
		}
		startResult <- err
	})
	queue := api.manager.GetQueue()
	queue.Pause()

	params := &common.DownloadParams{DownloadDirectory: warplib.ConfigDir}
	_, response, err := api.downloadHTTPHandler(
		nil,
		pool,
		"http://representation.test/queued.bin",
		params,
		nil,
		false,
	)
	if err != nil {
		t.Fatalf("downloadHTTPHandler: %v", err)
	}
	downloadResponse := response.(*common.DownloadResponse)
	item := api.manager.GetItem(downloadResponse.DownloadId)
	if item == nil {
		t.Fatal("queued item was not persisted")
	}
	bodies := transport.snapshotBodies()
	if len(bodies) != 1 || !bodies[0].closed.Load() {
		t.Fatalf("queued metadata bodies = %d, first closed = %v", len(bodies), len(bodies) == 1 && bodies[0].closed.Load())
	}
	if !queue.IsWaiting(downloadResponse.DownloadId) || item.IsDownloading() {
		t.Fatalf("waiting state = %v, live downloader = %v",
			queue.IsWaiting(downloadResponse.DownloadId), item.IsDownloading())
	}
	if !pool.HasDownload(downloadResponse.DownloadId) {
		t.Fatal("waiting item was removed from the attach pool")
	}

	queue.Resume()
	select {
	case startErr := <-startResult:
		if startErr != nil {
			t.Fatalf("promoted Fresh transfer: %v", startErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queue promotion did not reconstruct and start the transfer")
	}
	bodies = transport.snapshotBodies()
	if len(bodies) != 2 || !bodies[1].closed.Load() {
		t.Fatalf("promotion bodies = %d, second closed = %v", len(bodies), len(bodies) == 2 && bodies[1].closed.Load())
	}
	got, err := os.ReadFile(downloadResponse.SavePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, atPromotion) {
		t.Fatalf("downloaded body = %q, want promotion representation %q", got, atPromotion)
	}
}
