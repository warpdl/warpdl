package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
)

func receiveRPCNotificationMethod(t *testing.T, messages <-chan []byte) string {
	t.Helper()
	select {
	case data := <-messages:
		var notification struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(data, &notification); err != nil {
			t.Fatalf("decode notification: %v", err)
		}
		return notification.Method
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RPC notification")
		return ""
	}
}

func assertNoRPCNotification(t *testing.T, messages <-chan []byte) {
	t.Helper()
	select {
	case data := <-messages:
		t.Fatalf("unexpected duplicate RPC notification: %s", data)
	case <-time.After(50 * time.Millisecond):
	}
}

func newTransferNotificationTestServer(t *testing.T) (*Server, chan []byte) {
	t.Helper()
	rpc := &RPCServer{notifier: NewRPCNotifier(nil)}
	server := &Server{ws: &WebServer{rpc: rpc}}
	client, rpcClient, cleanup := newTestServer(t)
	rpc.notifier.Register(rpcClient)

	messages := make(chan []byte, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			data, err := client.Recv()
			if err != nil {
				return
			}
			messages <- data
		}
	}()
	t.Cleanup(func() {
		rpc.notifier.Unregister(rpcClient)
		cleanup()
		<-done
	})
	return server, messages
}

func TestDecorateTransferHandlersChainsAndReportsReturnedErrorOnce(t *testing.T) {
	server, messages := newTransferNotificationTestServer(t)
	var progressCalls, errorCalls atomic.Int32
	handlers, reportReturnedError := server.DecorateTransferHandlers("reconstructed", &warplib.Handlers{
		DownloadProgressHandler: func(string, int) { progressCalls.Add(1) },
		ErrorHandler:            func(string, error) { errorCalls.Add(1) },
	})

	handlers.DownloadProgressHandler("part", 32)
	if got := receiveRPCNotificationMethod(t, messages); got != "download.progress" {
		t.Fatalf("progress notification method = %q", got)
	}
	runErr := errors.New("reconstructed transfer failed")
	handlers.ErrorHandler("part", runErr)
	reportReturnedError(runErr)
	if got := receiveRPCNotificationMethod(t, messages); got != "download.error" {
		t.Fatalf("error notification method = %q", got)
	}
	assertNoRPCNotification(t, messages)
	if progressCalls.Load() != 1 || errorCalls.Load() != 1 {
		t.Fatalf("chained handler calls: progress=%d error=%d",
			progressCalls.Load(), errorCalls.Load())
	}
}

func TestDecorateTransferHandlersReportsCompletionOnce(t *testing.T) {
	server, messages := newTransferNotificationTestServer(t)
	var completeCalls atomic.Int32
	handlers, reportReturnedError := server.DecorateTransferHandlers("completed", &warplib.Handlers{
		DownloadCompleteHandler: func(string, int64) { completeCalls.Add(1) },
	})

	handlers.DownloadCompleteHandler(warplib.MAIN_HASH, 128)
	reportReturnedError(errors.New("late returned error"))
	if got := receiveRPCNotificationMethod(t, messages); got != "download.complete" {
		t.Fatalf("completion notification method = %q", got)
	}
	assertNoRPCNotification(t, messages)
	if completeCalls.Load() != 1 {
		t.Fatalf("chained completion calls = %d", completeCalls.Load())
	}
}

func TestDecorateTransferHandlersSuppressesShutdownCancellationCallback(t *testing.T) {
	server, messages := newTransferNotificationTestServer(t)
	manager, _ := newRPCQueueTestManager(t)
	server.ws.m = manager
	manager.CancelTransfers()

	var errorCalls atomic.Int32
	handlers, reportReturnedError := server.DecorateTransferHandlers("shutdown", &warplib.Handlers{
		ErrorHandler: func(string, error) { errorCalls.Add(1) },
	})
	handlers.ErrorHandler("part", context.Canceled)
	reportReturnedError(context.Canceled)

	assertNoRPCNotification(t, messages)
	if errorCalls.Load() != 1 {
		t.Fatalf("chained error callback calls = %d, want 1", errorCalls.Load())
	}
}

func TestDecorateTransferHandlersLateGenerationCannotNotifyReplacement(t *testing.T) {
	server, messages := newTransferNotificationTestServer(t)
	const uid = "reused-decorated-gid"
	pool := NewPool(nil)
	pool.AddDownload(uid, nil)
	oldGeneration, ok := pool.CurrentGeneration(uid)
	if !ok {
		t.Fatal("old generation was not registered")
	}

	var chained atomic.Int32
	handlers, reportReturnedError := server.DecorateTransferHandlersForGeneration(
		uid,
		oldGeneration,
		&warplib.Handlers{
			ErrorHandler:            func(string, error) { chained.Add(1) },
			DownloadProgressHandler: func(string, int) { chained.Add(1) },
			ResumeProgressHandler:   func(string, int) { chained.Add(1) },
			DownloadCompleteHandler: func(string, int64) { chained.Add(1) },
		},
	)
	if !oldGeneration.Abort() {
		t.Fatal("failed to retire old generation")
	}
	pool.AddDownload(uid, nil)

	replacementTerminal := &atomic.Bool{}
	server.ws.rpc.registerTransferTerminal(uid, replacementTerminal)
	handlers.DownloadProgressHandler("part", 1)
	handlers.ResumeProgressHandler("part", 2)
	handlers.ErrorHandler("part", errors.New("late callback failure"))
	handlers.DownloadCompleteHandler(warplib.MAIN_HASH, 3)
	reportReturnedError(errors.New("late returned failure"))

	assertNoRPCNotification(t, messages)
	if got := chained.Load(); got != 4 {
		t.Fatalf("chained handler calls = %d, want 4", got)
	}
	server.ws.rpc.transferMu.Lock()
	current := server.ws.rpc.transferTerminals[uid]
	server.ws.rpc.transferMu.Unlock()
	if current != replacementTerminal {
		t.Fatal("late decorated generation unregistered replacement terminal")
	}
}

func TestTransferResultReporterSuppressesDuplicateLiveRPCError(t *testing.T) {
	server, messages := newTransferNotificationTestServer(t)
	rpc := server.ws.rpc
	const uid = "live-rpc"
	var terminalReported atomic.Bool
	rpc.registerTransferTerminal(uid, &terminalReported)
	handlers := rpc.rpcTransferHandlers(pointerTo(uid), &terminalReported, nil)
	reportReturnedError := server.TransferResultReporter(uid)

	runErr := errors.New("live transfer failed")
	handlers.ErrorHandler("part", runErr)
	handlers.DownloadProgressHandler("late-part", 64)
	handlers.ResumeProgressHandler("late-part", 64)
	reportReturnedError(runErr)
	if got := receiveRPCNotificationMethod(t, messages); got != "download.error" {
		t.Fatalf("error notification method = %q", got)
	}
	assertNoRPCNotification(t, messages)

	rpc.transferMu.Lock()
	_, retained := rpc.transferTerminals[uid]
	rpc.transferMu.Unlock()
	if retained {
		t.Fatal("completed live transfer retained terminal generation")
	}
}

func TestTransferResultReporterSuppressesShutdownCancellation(t *testing.T) {
	server, messages := newTransferNotificationTestServer(t)
	manager, pool := newRPCQueueTestManager(t)
	server.ws.m = manager
	server.ws.rpc.manager = manager
	server.ws.rpc.pool = pool
	const uid = "shutdown-result"

	pool.AddDownload(uid, nil)
	generation, ok := pool.CurrentGeneration(uid)
	if !ok {
		t.Fatal("generation was not registered")
	}
	terminal := &atomic.Bool{}
	server.ws.rpc.registerTransferTerminal(uid, terminal)
	reportReturnedError := server.TransferResultReporterForGeneration(uid, generation)

	manager.CancelTransfers()
	reportReturnedError(context.Canceled)

	assertNoRPCNotification(t, messages)
	server.ws.rpc.transferMu.Lock()
	_, retained := server.ws.rpc.transferTerminals[uid]
	server.ws.rpc.transferMu.Unlock()
	if retained {
		t.Fatal("shutdown result retained terminal registration")
	}
	generation.Abort()
}

func TestTransferResultReporterLateGenerationCannotDeleteReplacement(t *testing.T) {
	server, messages := newTransferNotificationTestServer(t)
	rpc := server.ws.rpc
	const uid = "reused-rpc-gid"
	pool := NewPool(nil)

	pool.AddDownload(uid, nil)
	oldGeneration, ok := pool.CurrentGeneration(uid)
	if !ok {
		t.Fatal("old generation was not registered")
	}
	oldTerminal := &atomic.Bool{}
	rpc.registerTransferTerminal(uid, oldTerminal)
	oldReporter := server.TransferResultReporterForGeneration(uid, oldGeneration)

	if !oldGeneration.Abort() {
		t.Fatal("failed to retire old generation")
	}
	pool.AddDownload(uid, nil)
	newGeneration, ok := pool.CurrentGeneration(uid)
	if !ok {
		t.Fatal("replacement generation was not registered")
	}
	newTerminal := &atomic.Bool{}
	rpc.registerTransferTerminal(uid, newTerminal)
	oldReporter(errors.New("late old-generation failure"))
	assertNoRPCNotification(t, messages)

	rpc.transferMu.Lock()
	current := rpc.transferTerminals[uid]
	rpc.transferMu.Unlock()
	if current != newTerminal {
		t.Fatal("late old-generation cleanup deleted the replacement generation")
	}

	newReporter := server.TransferResultReporterForGeneration(uid, newGeneration)
	newReporter(errors.New("new generation failed"))
	if got := receiveRPCNotificationMethod(t, messages); got != "download.error" {
		t.Fatalf("replacement error notification method = %q", got)
	}
	assertNoRPCNotification(t, messages)
}

func TestRPCTransferCallbacksAndFinalizerCannotMutateReplacementGeneration(t *testing.T) {
	server, messages := newTransferNotificationTestServer(t)
	rpc := server.ws.rpc
	pool := NewPool(nil)
	rpc.pool = pool
	const uid = "reused-transfer-gid"

	pool.AddDownload(uid, nil)
	oldGeneration, ok := pool.CurrentGeneration(uid)
	if !ok {
		t.Fatal("old generation was not registered")
	}
	var generation atomic.Pointer[TransferGeneration]
	generation.Store(oldGeneration)
	var terminal atomic.Bool
	handlers := rpc.rpcTransferHandlers(pointerTo(uid), &terminal, &generation)

	if !oldGeneration.Abort() {
		t.Fatal("failed to retire old generation")
	}
	pool.AddDownload(uid, nil)
	replacement, ok := pool.CurrentGeneration(uid)
	if !ok {
		t.Fatal("replacement generation was not registered")
	}

	handlers.DownloadProgressHandler("late-part", 1)
	handlers.ResumeProgressHandler("late-part", 2)
	handlers.ErrorHandler("late-part", errors.New("late worker failure"))
	var exactClose atomic.Int32
	rpc.finishAsyncTransferWithCleanup(
		uid,
		oldGeneration,
		errors.New("late returned failure"),
		&terminal,
		func() error {
			exactClose.Add(1)
			return nil
		},
		true,
	)

	assertNoRPCNotification(t, messages)
	if got := exactClose.Load(); got != 1 {
		t.Fatalf("exact stale cleanup calls = %d, want 1", got)
	}
	current, ok := pool.CurrentGeneration(uid)
	if !ok || current != replacement || !replacement.IsRunnable() {
		t.Fatal("late old generation changed replacement lifecycle")
	}
	if got := pool.GetError(uid); got != nil {
		t.Fatalf("late old generation overwrote replacement error state: %+v", got)
	}
	replacement.Abort()
}

func TestNormalizeServerTransferErrorSuppressesOnlyShutdownFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	substantive := errors.New("disk write failed")

	for _, err := range []error{
		context.Canceled,
		warplib.ErrItemDownloaderNotFound,
		warplib.ErrReconstructionSuperseded,
		warplib.ErrManagerShuttingDown,
		errors.Join(context.Canceled, warplib.ErrReconstructionSuperseded),
	} {
		if got := normalizeServerTransferError(ctx, err); got != nil {
			t.Errorf("normalizeServerTransferError(%v) = %v, want nil", err, got)
		}
	}

	joined := errors.Join(warplib.ErrReconstructionSuperseded, substantive)
	if got := normalizeServerTransferError(ctx, joined); !errors.Is(got, substantive) {
		t.Fatalf("substantive sibling was suppressed: %v", got)
	}
	if got := normalizeServerTransferError(context.Background(), warplib.ErrReconstructionSuperseded); got == nil {
		t.Fatal("live-context reconstruction failure was suppressed")
	}
}

func pointerTo(value string) *string {
	return &value
}
