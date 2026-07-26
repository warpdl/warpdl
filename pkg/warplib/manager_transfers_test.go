package warplib

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func waitForTransferClosing(t *testing.T, manager *Manager) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		manager.transferMu.Lock()
		closing := manager.transferClosing
		manager.transferMu.Unlock()
		if closing {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("transfer admission did not close")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestManagerWaitTransfersClosesAdmissionAndDrains(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	if !manager.GoTransfer(func(context.Context) {
		defer close(finished)
		close(started)
		<-release
	}) {
		t.Fatal("initial transfer was not admitted")
	}
	<-started

	waitResult := make(chan error, 1)
	go func() {
		waitResult <- manager.WaitTransfers(context.Background())
	}()
	waitForTransferClosing(t, manager)
	var rejectedRan atomic.Bool
	if manager.GoTransfer(func(context.Context) { rejectedRan.Store(true) }) {
		t.Fatal("transfer was admitted after WaitTransfers closed admission")
	}
	close(release)
	if err := <-waitResult; err != nil {
		t.Fatalf("WaitTransfers: %v", err)
	}
	<-finished
	if rejectedRan.Load() {
		t.Fatal("rejected transfer callback ran")
	}
	manager.transferMu.Lock()
	active := manager.transferActive
	manager.transferMu.Unlock()
	if active != 0 {
		t.Fatalf("active transfers = %d, want 0", active)
	}
}

func TestManagerCloseCancelsAndWaitsForTransferFinalizer(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	started := make(chan struct{})
	finalized := make(chan struct{})
	if !manager.GoTransfer(func(ctx context.Context) {
		defer close(finalized)
		close(started)
		<-ctx.Done()
	}) {
		t.Fatal("transfer was not admitted")
	}
	<-started
	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-finalized
	if !errors.Is(manager.TransferContext().Err(), context.Canceled) {
		t.Fatalf("transfer context error = %v, want context canceled",
			manager.TransferContext().Err())
	}
	if manager.GoTransfer(func(context.Context) {}) {
		t.Fatal("transfer admitted after Manager.Close")
	}
	if _, err := manager.ResumeDownload(nil, "missing", nil); !errors.Is(err, ErrManagerShuttingDown) {
		t.Fatalf("ResumeDownload after Close = %v, want %v", err, ErrManagerShuttingDown)
	}
}

func TestNormalizeTransferError(t *testing.T) {
	liveCtx := context.Background()
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	substantive := errors.New("disk write failed")
	joinedSubstantive := errors.Join(context.Canceled, substantive)
	tests := []struct {
		name string
		ctx  context.Context
		err  error
		nil  bool
	}{
		{name: "live cancellation preserved", ctx: liveCtx, err: context.Canceled},
		{name: "cancelled context cancellation", ctx: cancelledCtx, err: context.Canceled, nil: true},
		{name: "cancelled context closed transport", ctx: cancelledCtx, err: net.ErrClosed, nil: true},
		{name: "cancelled context missing allocation", ctx: cancelledCtx, err: ErrItemDownloaderNotFound, nil: true},
		{name: "cancelled context superseded reconstruction", ctx: cancelledCtx, err: ErrReconstructionSuperseded, nil: true},
		{name: "cancelled context closed admission", ctx: cancelledCtx, err: ErrManagerShuttingDown, nil: true},
		{
			name: "all cancellation leaves",
			ctx:  cancelledCtx,
			err:  errors.Join(context.Canceled, net.ErrClosed),
			nil:  true,
		},
		{
			name: "all shutdown leaves",
			ctx:  cancelledCtx,
			err:  errors.Join(ErrReconstructionSuperseded, net.ErrClosed),
			nil:  true,
		},
		{name: "substantive joined sibling", ctx: cancelledCtx, err: joinedSubstantive},
		{name: "lifecycle sentinel with substantive sibling", ctx: cancelledCtx, err: errors.Join(ErrManagerShuttingDown, substantive)},
		{name: "deadline is substantive", ctx: cancelledCtx, err: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeTransferError(test.ctx, test.err)
			if test.nil {
				if got != nil {
					t.Fatalf("NormalizeTransferError = %v, want nil", got)
				}
				return
			}
			if got != test.err {
				t.Fatalf("NormalizeTransferError = %v, want original %v", got, test.err)
			}
		})
	}
}

type shutdownProbeDownloader struct {
	*blockingReconstructionDownloader
	probeContext chan context.Context
}

func (d *shutdownProbeDownloader) Probe(ctx context.Context) (ProbeResult, error) {
	d.probeContext <- ctx
	return d.blockingReconstructionDownloader.Probe(ctx)
}

func TestManagerCloseCancelsBlockedProbeAndRejectsCommit(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	const hash = "manager-shutdown-probe"
	manager.UpdateItem(&Item{
		Hash:             hash,
		Name:             "old.bin",
		Url:              "ftp://example.invalid/old.bin",
		TotalSize:        10,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Protocol:         ProtoFTP,
		Parts:            make(map[int64]*ItemPart),
	})
	candidate := &shutdownProbeDownloader{
		blockingReconstructionDownloader: &blockingReconstructionDownloader{
			hash:         hash,
			name:         "candidate.bin",
			dir:          base,
			size:         333,
			probeStarted: make(chan struct{}),
			releaseProbe: make(chan struct{}),
		},
		probeContext: make(chan context.Context, 1),
	}
	constructorContext := make(chan context.Context, 1)
	router := NewSchemeRouter(nil)
	router.Register("ftp", func(_ string, opts *DownloaderOpts) (ProtocolDownloader, error) {
		constructorContext <- opts.Context
		return candidate, nil
	})
	manager.SetSchemeRouter(router)

	resumeResult := make(chan reconstructionResult, 1)
	go func() {
		item, lease, resumeErr := manager.ResumeDownloadWithLease(nil, hash, &ResumeDownloadOpts{
			Fresh: true,
		})
		resumeResult <- reconstructionResult{item: item, lease: lease, err: resumeErr}
	}()
	probeCtx := <-candidate.probeContext
	if got := <-constructorContext; got != manager.TransferContext() {
		t.Fatal("protocol constructor did not receive Manager transfer context")
	}
	if probeCtx != manager.TransferContext() {
		t.Fatal("Probe did not receive Manager transfer context")
	}

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- manager.Close()
	}()
	select {
	case <-manager.TransferContext().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Manager.Close did not cancel transfer context")
	}
	select {
	case err := <-closeResult:
		t.Fatalf("Manager.Close returned before blocked probe drained: %v", err)
	default:
	}
	if candidate.closeCalls.Load() != 0 {
		t.Fatal("candidate closed before ignored Probe returned")
	}

	close(candidate.releaseProbe)
	resumed := <-resumeResult
	if !errors.Is(resumed.err, ErrReconstructionSuperseded) {
		t.Fatalf("ResumeDownloadWithLease error = %v, want %v",
			resumed.err, ErrReconstructionSuperseded)
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("Manager.Close: %v", err)
	}
	if candidate.closeCalls.Load() != 1 {
		t.Fatalf("candidate close calls = %d, want 1", candidate.closeCalls.Load())
	}
	item := manager.GetItem(hash)
	if item.IsDownloading() {
		t.Fatal("cancelled probe published its downloader")
	}
	snapshot := item.Snapshot()
	if snapshot.Name != "old.bin" || snapshot.TotalSize != 10 {
		t.Fatalf("cancelled probe committed metadata: name=%q size=%d",
			snapshot.Name, snapshot.TotalSize)
	}
}

type blockedRegistrationDownloader struct {
	*runLeaseDownloader
	fileNameEntered chan struct{}
	releaseFileName chan struct{}
	enterOnce       atomic.Bool
}

func (d *blockedRegistrationDownloader) GetFileName() string {
	if d.enterOnce.CompareAndSwap(false, true) {
		close(d.fileNameEntered)
		<-d.releaseFileName
	}
	return d.runLeaseDownloader.GetFileName()
}

func TestManagerCloseDrainsAndClosesConcurrentRegistration(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	const hash = "shutdown-registration"
	allocation := &blockedRegistrationDownloader{
		runLeaseDownloader: &runLeaseDownloader{
			hash: hash,
			name: hash + ".bin",
			dir:  base,
		},
		fileNameEntered: make(chan struct{}),
		releaseFileName: make(chan struct{}),
	}
	addResult := make(chan error, 1)
	go func() {
		addResult <- manager.AddProtocolDownload(
			allocation,
			ProbeResult{
				FileName:      allocation.name,
				ContentLength: 1,
				Resumable:     true,
			},
			"ftp://example.invalid/"+hash,
			ProtoFTP,
			&Handlers{},
			&AddDownloadOpts{AbsoluteLocation: base},
		)
	}()
	<-allocation.fileNameEntered

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- manager.Close()
	}()
	select {
	case <-manager.TransferContext().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Manager.Close did not cancel registration admission")
	}
	select {
	case err := <-closeResult:
		t.Fatalf("Manager.Close crossed admitted registration early: %v", err)
	default:
	}

	close(allocation.releaseFileName)
	if err := <-addResult; err != nil {
		t.Fatalf("AddProtocolDownload admitted before shutdown: %v", err)
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("Manager.Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Manager.Close did not finish after registration returned")
	}
	if got := allocation.closeCalls.Load(); got != 1 {
		t.Fatalf("registered allocation close calls = %d, want 1", got)
	}
	if item := manager.GetItem(hash); item == nil || item.getDAlloc() != nil {
		t.Fatal("concurrently registered allocation remained attached after Close")
	}
	if err := manager.AddProtocolDownload(
		allocation,
		ProbeResult{},
		"ftp://example.invalid/"+hash,
		ProtoFTP,
		&Handlers{},
		nil,
	); !errors.Is(err, ErrManagerShuttingDown) {
		t.Fatalf("AddProtocolDownload after Close = %v, want %v",
			err, ErrManagerShuttingDown)
	}
}
