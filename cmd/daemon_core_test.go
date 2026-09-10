package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/scheduler"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman/keyring"
	"github.com/warpdl/warpdl/pkg/logger"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestLoggerKeyringAdapterWarning(t *testing.T) {
	l := &loggerKeyringAdapter{log: logger.NewNopLogger()}
	l.Warning("test warning: %s %d", "arg", 42) // must not panic
}

func TestGetCookieManagerWithLogger_KeyringGetSuccess(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{getKey: bytes.Repeat([]byte{0x22}, 32)}
	oldKeyring := newKeyring
	newKeyring = func(configDir string, _ keyring.Logger) keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	cm, err := getCookieManagerWithLogger(logger.NewNopLogger())
	if err != nil {
		t.Fatalf("getCookieManagerWithLogger: %v", err)
	}
	defer cm.Close()
}

func TestGetCookieManagerWithLogger_KeyringSetKeySuccess(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{
		getErr: errors.New("no key"),
		setKey: bytes.Repeat([]byte{0x33}, 32),
	}
	oldKeyring := newKeyring
	newKeyring = func(configDir string, _ keyring.Logger) keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	cm, err := getCookieManagerWithLogger(logger.NewNopLogger())
	if err != nil {
		t.Fatalf("getCookieManagerWithLogger: %v", err)
	}
	defer cm.Close()
}

func TestGetCookieManagerWithLogger_KeyringSetKeyError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{
		getErr: errors.New("no key"),
		setErr: errors.New("set failed"),
	}
	oldKeyring := newKeyring
	newKeyring = func(configDir string, _ keyring.Logger) keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	if _, err := getCookieManagerWithLogger(logger.NewNopLogger()); err == nil {
		t.Fatal("expected error for keyring set failure")
	}
}

func TestGetCookieManagerWithLogger_InvalidHex(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "not-valid-hex")

	if _, err := getCookieManagerWithLogger(logger.NewNopLogger()); err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestGetCookieManagerWithLogger_CredmanError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write corrupt GOB data to cookie file so NewCookieManager fails to load
	cookiePath := base + "/cookies.warp"
	if err := os.WriteFile(cookiePath, []byte("not valid gob data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv(cookieKeyEnv, strings.Repeat("bb", 32))

	_, err := getCookieManagerWithLogger(logger.NewNopLogger())
	if err == nil {
		t.Fatal("expected error for corrupt cookie file")
	}
}

func TestInitDaemonComponents_WithCookieKey(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	t.Setenv(cookieKeyEnv, strings.Repeat("11", 32))

	oldBuildArgs := currentBuildArgs
	currentBuildArgs = BuildArgs{
		Version:   "1.0.0",
		Commit:    "test",
		BuildType: "test",
	}
	defer func() { currentBuildArgs = oldBuildArgs }()

	components, err := initDaemonComponents(logger.NewNopLogger(), 0, nil)
	if err != nil {
		t.Fatalf("initDaemonComponents: %v", err)
	}
	if components == nil || components.Server == nil || components.Manager == nil || components.Api == nil {
		t.Fatal("initDaemonComponents returned incomplete components")
	}

	components.Close()
}

func TestInitDaemonComponents_MissedScheduleHonorsPausedQueue(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	t.Setenv(cookieKeyEnv, strings.Repeat("22", 32))

	content := bytes.Repeat([]byte("scheduled-queue"), 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			if r.Method != http.MethodHead {
				_, _ = w.Write(content)
			}
			return
		}
		bounds := strings.SplitN(strings.TrimPrefix(rangeHeader, "bytes="), "-", 2)
		start, _ := strconv.Atoi(bounds[0])
		end := len(content) - 1
		if len(bounds) == 2 && bounds[1] != "" {
			end, _ = strconv.Atoi(bounds[1])
		}
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method != http.MethodHead {
			_, _ = w.Write(content[start : end+1])
		}
	}))
	defer server.Close()

	first, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	first.SetMaxConcurrentDownloads(1, nil)
	first.GetQueue().Pause()
	downloader, err := warplib.NewDownloader(server.Client(), server.URL+"/scheduled.bin", &warplib.DownloaderOpts{
		FileName:          "scheduled.bin",
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
		Overwrite:         true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	hash := downloader.GetHash()
	if err := first.AddDownload(downloader, &warplib.AddDownloadOpts{
		AbsoluteLocation: base,
		SkipQueue:        true,
	}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	if err := first.ConfigureSchedule(hash, time.Now().Add(-time.Minute), "", warplib.ScheduleStateScheduled); err != nil {
		t.Fatalf("ConfigureSchedule: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first manager: %v", err)
	}
	_ = downloader.Close()

	oldBuildArgs := currentBuildArgs
	currentBuildArgs = BuildArgs{Version: "1.0.0", Commit: "test", BuildType: "test"}
	defer func() { currentBuildArgs = oldBuildArgs }()

	components, err := initDaemonComponents(logger.NewNopLogger(), 1, nil)
	if err != nil {
		t.Fatalf("initDaemonComponents: %v", err)
	}
	defer components.Close()

	queue := components.Manager.GetQueue()
	if queue == nil || !queue.IsPaused() {
		t.Fatal("restored queue did not preserve paused state")
	}
	if !queue.IsWaiting(hash) {
		t.Fatal("missed scheduled item bypassed the paused queue")
	}
	if queue.ActiveCount() != 0 {
		t.Fatal("missed scheduled item occupied an active slot while paused")
	}
}

func newDaemonLifecycleServer(t *testing.T, content []byte) (
	*httptest.Server,
	*atomic.Bool,
	*atomic.Bool,
	<-chan struct{},
	chan<- struct{},
) {
	t.Helper()
	var blockStart atomic.Bool
	var truncateStart atomic.Bool
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"daemon-lifecycle"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			_, _ = w.Write(content)
			return
		}
		bounds := strings.SplitN(strings.TrimPrefix(rangeHeader, "bytes="), "-", 2)
		start, _ := strconv.Atoi(bounds[0])
		end := len(content) - 1
		if len(bounds) == 2 && bounds[1] != "" {
			end, _ = strconv.Atoi(bounds[1])
		}
		if start < 0 || start > end || end >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if start == 0 && blockStart.Load() {
			startedOnce.Do(func() { close(started) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		chunk := content[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.Header().Set("Content-Range",
			"bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusPartialContent)
		if start != 1 && truncateStart.Load() {
			startedOnce.Do(func() { close(started) })
			_, _ = w.Write(chunk[:len(chunk)/2])
			return
		}
		_, _ = w.Write(chunk)
	}))
	return server, &blockStart, &truncateStart, started, release
}

func TestInitDaemonComponents_RestoresTriggeredUnlimitedItemIntoPool(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	t.Setenv(cookieKeyEnv, strings.Repeat("23", 32))

	content := bytes.Repeat([]byte("triggered-pool-lifecycle-"), 4096)
	downloadServer, blockStart, _, started, release := newDaemonLifecycleServer(t, content)
	defer downloadServer.Close()
	first, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	downloader, err := warplib.NewDownloader(downloadServer.Client(), downloadServer.URL+"/triggered.bin", &warplib.DownloaderOpts{
		FileName:          "triggered.bin",
		LockFileName:      true,
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
		RetryConfig: &warplib.RetryConfig{
			MaxRetries:    1,
			BaseDelay:     time.Millisecond,
			MaxDelay:      time.Millisecond,
			BackoffFactor: 1,
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	hash := downloader.GetHash()
	if err := first.AddDownload(downloader, &warplib.AddDownloadOpts{
		AbsoluteLocation: base,
		SkipQueue:        true,
	}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	if err := first.ConfigureSchedule(hash, time.Now().Add(-time.Minute), "", warplib.ScheduleStateTriggered); err != nil {
		t.Fatalf("ConfigureSchedule: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	_ = downloader.Close()
	blockStart.Store(true)

	oldBuildArgs := currentBuildArgs
	currentBuildArgs = BuildArgs{Version: "1.0.0", Commit: "test", BuildType: "test"}
	defer func() { currentBuildArgs = oldBuildArgs }()
	components, err := initDaemonComponents(logger.NewNopLogger(), 0, nil)
	if err != nil {
		t.Fatalf("initDaemonComponents: %v", err)
	}
	defer func() { _ = components.Close() }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("triggered item was not restored with maxConcurrent=0")
	}
	if !components.Server.Pool().HasDownload(hash) {
		t.Fatal("reconstructed transfer was not registered in the server pool")
	}
	queue := components.Manager.GetQueue()
	if queue == nil || queue.MaxConcurrent() != 0 || !queue.IsActive(hash) {
		t.Fatalf("restored unlimited queue state: queue=%v", queue)
	}
	if err := components.Manager.GetItem(hash).StopDownload(); err != nil {
		t.Fatalf("StopDownload: %v", err)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for components.Server.Pool().HasDownload(hash) || queue.IsActive(hash) {
		if time.Now().After(deadline) {
			t.Fatalf("stopped transfer retained pool/queue state: pool=%v active=%v",
				components.Server.Pool().HasDownload(hash), queue.IsActive(hash))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestInitDaemonComponents_RestoredQueueFailureRecordsPoolError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	t.Setenv(cookieKeyEnv, strings.Repeat("24", 32))

	content := bytes.Repeat([]byte("restored-queue-error-"), 4096)
	downloadServer, _, truncateStart, started, _ := newDaemonLifecycleServer(t, content)
	defer downloadServer.Close()
	first, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	first.SetMaxConcurrentDownloads(1, nil)
	downloader, err := warplib.NewDownloader(downloadServer.Client(), downloadServer.URL+"/failure.bin", &warplib.DownloaderOpts{
		FileName:          "failure.bin",
		LockFileName:      true,
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
		RetryConfig: &warplib.RetryConfig{
			MaxRetries:    1,
			BaseDelay:     time.Millisecond,
			MaxDelay:      time.Millisecond,
			BackoffFactor: 1,
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	hash := downloader.GetHash()
	if err := first.AddDownload(downloader, &warplib.AddDownloadOpts{AbsoluteLocation: base}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	if !first.GetQueue().IsActive(hash) {
		t.Fatal("setup item was not persisted active")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	_ = downloader.Close()
	truncateStart.Store(true)

	oldBuildArgs := currentBuildArgs
	currentBuildArgs = BuildArgs{Version: "1.0.0", Commit: "test", BuildType: "test"}
	defer func() { currentBuildArgs = oldBuildArgs }()
	components, err := initDaemonComponents(logger.NewNopLogger(), 0, nil)
	if err != nil {
		t.Fatalf("initDaemonComponents: %v", err)
	}
	defer func() { _ = components.Close() }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("restored queue transfer did not start")
	}
	// Pool errors are recorded for intermediate retry failures too, so poll
	// until the terminal state holds conjunctively: critical error recorded,
	// transfer detached from the pool, queue slot released.
	deadline := time.Now().Add(10 * time.Second)
	for {
		recorded := components.Server.Pool().GetError(hash)
		terminal := recorded != nil && recorded.Type == server.ErrorTypeCritical && recorded.Message != ""
		detached := !components.Server.Pool().HasDownload(hash)
		queue := components.Manager.GetQueue()
		released := queue == nil || !queue.IsActive(hash)
		if terminal && detached && released {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restored queue failure did not reach terminal state (error=%+v detached=%v released=%v)", recorded, detached, released)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDaemonComponentsCloseQuiescesQueuePromotion(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	started := make(chan string, 2)
	manager.SetMaxConcurrentDownloads(1, func(hash string) { started <- hash })
	queue := manager.GetQueue()
	queue.Add("active", warplib.PriorityNormal)
	if got := <-started; got != "active" {
		t.Fatalf("initial start = %q", got)
	}
	queue.Add("waiting", warplib.PriorityNormal)

	components := &DaemonComponents{Manager: manager}
	if err := components.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Model an active downloader reporting completion during/after teardown.
	// Close must already have disabled promotion.
	queue.OnComplete("active")
	select {
	case hash := <-started:
		t.Fatalf("daemon cleanup started waiting item %q", hash)
	default:
	}
	if !queue.IsWaiting("waiting") {
		t.Fatal("daemon cleanup discarded waiting item")
	}
}

func TestDaemonComponentsCloseDrainsSchedulerBeforeItemShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	triggerEntered := make(chan struct{})
	releaseTrigger := make(chan struct{})
	sched := scheduler.New(ctx, func(string) {
		close(triggerEntered)
		<-releaseTrigger
	})
	sched.Add(scheduler.ScheduleEvent{
		ItemHash:  "in-flight",
		TriggerAt: time.Now(),
	})
	select {
	case <-triggerEntered:
	case <-time.After(time.Second):
		t.Fatal("scheduler trigger did not begin")
	}

	components := &DaemonComponents{
		Scheduler:       sched,
		schedulerCancel: cancel,
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- components.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before in-flight scheduler callback drained: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseTrigger)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return after scheduler callback drained")
	}
}

func TestDaemonComponentsCloseContextTimeoutCanBeRetried(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	finalized := make(chan struct{})
	if !manager.GoTransfer(func(context.Context) {
		close(started)
		<-release // Model a transport that does not promptly honor cancellation.
		close(finalized)
	}) {
		t.Fatal("blocking transfer was not admitted")
	}
	<-started

	components := &DaemonComponents{Manager: manager}
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err = components.CloseContext(shortCtx)
	shortCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first CloseContext error = %v, want deadline exceeded", err)
	}
	select {
	case <-finalized:
		t.Fatal("blocking transfer unexpectedly finalized before release")
	default:
	}

	close(release)
	retryCtx, retryCancel := context.WithTimeout(context.Background(), time.Second)
	defer retryCancel()
	if err := components.CloseContext(retryCtx); err != nil {
		t.Fatalf("retry CloseContext: %v", err)
	}
	select {
	case <-finalized:
	default:
		t.Fatal("successful retry returned before transfer finalizer")
	}
}

func TestScheduledTransferInFlightDetectsQueueAndLiveDownloader(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	manager.SetMaxConcurrentDownloads(1, nil)
	queue := manager.GetQueue()
	queue.Add("active-occurrence", warplib.PriorityNormal)
	queue.Add("waiting-occurrence", warplib.PriorityNormal)
	if !scheduledTransferInFlight(manager, nil, "active-occurrence") {
		t.Fatal("active queue occurrence was not detected")
	}
	if !scheduledTransferInFlight(manager, nil, "waiting-occurrence") {
		t.Fatal("waiting queue occurrence was not detected")
	}
	queue.OnComplete("active-occurrence")
	queue.OnComplete("waiting-occurrence")
	if scheduledTransferInFlight(manager, nil, "waiting-occurrence") {
		t.Fatal("completed queue occurrence remained in flight")
	}

	content := bytes.Repeat([]byte("recurring-overlap"), 64)
	server, _, _, _, _ := newDaemonLifecycleServer(t, content)
	defer server.Close()
	downloader, err := warplib.NewDownloader(server.Client(), server.URL+"/recurring.bin", &warplib.DownloaderOpts{
		FileName:          "recurring.bin",
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
		Overwrite:         true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := manager.AddDownload(downloader, &warplib.AddDownloadOpts{
		AbsoluteLocation: base,
		SkipQueue:        true,
	}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := manager.GetItem(downloader.GetHash())
	if !scheduledTransferInFlight(manager, item, item.Hash) {
		t.Fatal("allocated incomplete occurrence was not detected")
	}
	if err := item.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if scheduledTransferInFlight(manager, item, item.Hash) {
		t.Fatal("completed occurrence was mistaken for an overlap")
	}
}

func TestDaemonApplyTimestampSuffix_WithExtension(t *testing.T) {
	ts := time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	got := daemonApplyTimestampSuffix("backup.zip", ts)
	want := "backup-2026-03-01T020000.zip"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDaemonApplyTimestampSuffix_NoExtension(t *testing.T) {
	ts := time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	got := daemonApplyTimestampSuffix("backup", ts)
	want := "backup-2026-03-01T020000"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDaemonApplyTimestampSuffix_MultipleDots(t *testing.T) {
	ts := time.Date(2026, 6, 15, 10, 30, 0, 0, time.UTC)
	got := daemonApplyTimestampSuffix("my.file.tar.gz", ts)
	want := "my.file.tar-2026-06-15T103000.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDaemonApplyTimestampSuffix_ReplacesPriorOccurrence(t *testing.T) {
	ts := time.Date(2026, 6, 16, 10, 30, 0, 0, time.UTC)
	got := daemonApplyTimestampSuffix("backup-2026-06-15T103000.zip", ts)
	want := "backup-2026-06-16T103000.zip"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
