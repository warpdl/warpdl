package warplib

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type runLeaseDownloader struct {
	hash          string
	name          string
	dir           string
	stopped       atomic.Bool
	stopCalls     atomic.Int32
	closeCalls    atomic.Int32
	downloadCalls atomic.Int32
	stopCalled    chan struct{}
	stopOnce      sync.Once
}

func (d *runLeaseDownloader) Probe(context.Context) (ProbeResult, error) {
	return ProbeResult{
		FileName:      d.name,
		ContentLength: 1,
		Resumable:     true,
	}, nil
}

func (d *runLeaseDownloader) Download(context.Context, *Handlers) error {
	d.downloadCalls.Add(1)
	return nil
}

func (d *runLeaseDownloader) Resume(
	context.Context,
	map[int64]*ItemPart,
	*Handlers,
) error {
	d.downloadCalls.Add(1)
	return nil
}

func (d *runLeaseDownloader) Capabilities() DownloadCapabilities {
	return DownloadCapabilities{SupportsResume: true}
}

func (d *runLeaseDownloader) Close() error {
	d.closeCalls.Add(1)
	return nil
}

func (d *runLeaseDownloader) Stop() {
	d.stopped.Store(true)
	d.stopCalls.Add(1)
	if d.stopCalled != nil {
		d.stopOnce.Do(func() { close(d.stopCalled) })
	}
}

func (d *runLeaseDownloader) IsStopped() bool          { return d.stopped.Load() }
func (d *runLeaseDownloader) GetMaxConnections() int32 { return 1 }
func (d *runLeaseDownloader) GetMaxParts() int32       { return 1 }
func (d *runLeaseDownloader) GetHash() string          { return d.hash }
func (d *runLeaseDownloader) GetFileName() string      { return d.name }
func (d *runLeaseDownloader) GetDownloadDirectory() string {
	return d.dir
}
func (d *runLeaseDownloader) GetSavePath() string {
	return filepath.Join(d.dir, d.name)
}
func (d *runLeaseDownloader) GetContentLength() ContentLength { return 1 }

func newRunLeaseItem(hash, dir string, allocation ProtocolDownloader) *Item {
	item := &Item{
		Hash:             hash,
		Name:             hash + ".bin",
		Url:              "ftp://example.invalid/" + hash,
		DownloadLocation: dir,
		AbsoluteLocation: dir,
		Resumable:        true,
		Protocol:         ProtoFTP,
		Parts:            make(map[int64]*ItemPart),
		memPart:          make(map[string]int64),
		mu:               &sync.RWMutex{},
	}
	item.setDAlloc(allocation)
	return item
}

func TestRunLeaseCloseReleasesRawCloseBarrierExactlyOnce(t *testing.T) {
	allocation := &runLeaseDownloader{
		hash:       "raw-close-barrier",
		name:       "raw-close-barrier.bin",
		dir:        t.TempDir(),
		stopCalled: make(chan struct{}),
	}
	item := newRunLeaseItem(allocation.hash, allocation.dir, allocation)
	lease, err := item.AcquireRunLease()
	if err != nil {
		t.Fatalf("AcquireRunLease: %v", err)
	}

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- item.CloseDownloader()
	}()
	select {
	case <-allocation.stopCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("CloseDownloader did not stop the captured allocation")
	}
	if got := allocation.closeCalls.Load(); got != 0 {
		t.Fatalf("allocation closed with pending run claim: calls=%d", got)
	}
	select {
	case err := <-closeResult:
		t.Fatalf("CloseDownloader crossed pending claim early: %v", err)
	default:
	}

	if err := lease.Close(); err != nil {
		t.Fatalf("RunLease.Close: %v", err)
	}
	if err := <-closeResult; err != nil {
		t.Fatalf("CloseDownloader: %v", err)
	}
	if got := allocation.closeCalls.Load(); got != 1 {
		t.Fatalf("allocation close calls = %d, want 1", got)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("second RunLease.Close: %v", err)
	}
	if err := lease.Start(); !errors.Is(err, ErrRunLeaseUsed) {
		t.Fatalf("Start after Close = %v, want %v", err, ErrRunLeaseUsed)
	}
}

func TestManagerRunLeaseAdmissionTracksPendingClaimAndRejectsMismatch(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	const hash = "manager-pending-run"
	allocation := &runLeaseDownloader{
		hash: hash,
		name: hash + ".bin",
		dir:  base,
	}
	item := newRunLeaseItem(hash, base, allocation)
	item.mu = manager.mu
	manager.UpdateItem(item)

	other := &runLeaseDownloader{hash: hash, name: "other.bin", dir: base}
	if _, err := manager.AcquireProtocolRunLease(hash, other); !errors.Is(err, ErrReconstructionSuperseded) {
		t.Fatalf("mismatched AcquireProtocolRunLease = %v, want %v",
			err, ErrReconstructionSuperseded)
	}
	manager.transferMu.Lock()
	activeAfterReject := manager.transferActive
	manager.transferMu.Unlock()
	if activeAfterReject != 0 {
		t.Fatalf("active transfers after rejected lease = %d, want 0", activeAfterReject)
	}

	lease, err := manager.AcquireProtocolRunLease(hash, allocation)
	if err != nil {
		t.Fatalf("AcquireProtocolRunLease: %v", err)
	}
	manager.CancelTransfers()
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- manager.WaitTransfers(context.Background())
	}()
	select {
	case err := <-waitResult:
		t.Fatalf("WaitTransfers crossed pending lease early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := manager.AcquireRunLease(hash); !errors.Is(err, ErrManagerShuttingDown) {
		t.Fatalf("AcquireRunLease after CancelTransfers = %v, want %v",
			err, ErrManagerShuttingDown)
	}

	if err := lease.Close(); err != nil {
		t.Fatalf("RunLease.Close: %v", err)
	}
	select {
	case err := <-waitResult:
		if err != nil {
			t.Fatalf("WaitTransfers: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitTransfers did not drain after pending lease Close")
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Manager.Close: %v", err)
	}
}

func TestRunLeaseAbandonRelinquishesStaleActivationWithoutClosingCurrent(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "run-lease-abandon"
	replacement := &runLeaseDownloader{
		hash: hash,
		name: "replacement.bin",
		dir:  base,
	}
	item := newRunLeaseItem(hash, base, replacement)
	item.mu = manager.mu
	manager.UpdateItem(item)

	// The caller's external activation A can become stale after this exact
	// acquisition has already observed the replacement allocation B.
	lease, err := manager.AcquireRunLease(hash)
	if err != nil {
		t.Fatalf("AcquireRunLease: %v", err)
	}
	if err := lease.Abandon(); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	if err := lease.Abandon(); err != nil {
		t.Fatalf("second Abandon: %v", err)
	}
	if err := lease.Start(); !errors.Is(err, ErrRunLeaseUsed) {
		t.Fatalf("Start after Abandon = %v, want %v", err, ErrRunLeaseUsed)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("Close after Abandon: %v", err)
	}
	if manager.GetItem(hash).getDAlloc() != replacement {
		t.Fatal("abandoned stale activation detached the replacement")
	}
	if replacement.stopCalls.Load() != 0 || replacement.closeCalls.Load() != 0 {
		t.Fatalf("abandoned stale activation touched replacement: stop=%d close=%d",
			replacement.stopCalls.Load(), replacement.closeCalls.Load())
	}
	manager.transferMu.Lock()
	active := manager.transferActive
	manager.transferMu.Unlock()
	if active != 0 {
		t.Fatalf("active transfers after Abandon = %d, want 0", active)
	}
}

func TestRunLeaseStopAndReconstructionCannotStartCapturedAllocation(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "run-lease-preinvoke-aba"
	first := &runLeaseDownloader{hash: hash, name: "first.bin", dir: base}
	item := newRunLeaseItem(hash, base, first)
	item.mu = manager.mu
	manager.UpdateItem(item)
	lease, err := manager.AcquireProtocolRunLease(hash, first)
	if err != nil {
		t.Fatalf("AcquireProtocolRunLease: %v", err)
	}
	if err := item.StopDownload(); err != nil {
		t.Fatalf("StopDownload: %v", err)
	}
	if item.IsDownloading() {
		t.Fatal("stopped allocation still reported as downloading")
	}

	replacement := &blockingReconstructionDownloader{
		hash:         hash,
		name:         "replacement.bin",
		dir:          base,
		size:         2,
		probeStarted: make(chan struct{}),
		releaseProbe: make(chan struct{}),
	}
	router := NewSchemeRouter(nil)
	router.Register("ftp", func(string, *DownloaderOpts) (ProtocolDownloader, error) {
		return replacement, nil
	})
	manager.SetSchemeRouter(router)
	replacementResult := make(chan reconstructionResult, 1)
	go func() {
		reconstructed, replacementLease, resumeErr :=
			manager.ResumeDownloadWithLease(nil, hash, &ResumeDownloadOpts{Fresh: true})
		replacementResult <- reconstructionResult{
			item:  reconstructed,
			lease: replacementLease,
			err:   resumeErr,
		}
	}()
	select {
	case <-replacement.probeStarted:
		t.Fatal("replacement probe crossed the pending exact run claim")
	case <-time.After(50 * time.Millisecond):
	}

	if err := lease.Start(); err != nil {
		t.Fatalf("stopped RunLease.Start: %v", err)
	}
	if got := first.downloadCalls.Load(); got != 0 {
		t.Fatalf("captured stopped allocation download calls = %d, want 0", got)
	}
	if err := lease.Start(); !errors.Is(err, ErrRunLeaseUsed) {
		t.Fatalf("second RunLease.Start = %v, want %v", err, ErrRunLeaseUsed)
	}
	select {
	case <-replacement.probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement probe did not proceed after run claim released")
	}
	close(replacement.releaseProbe)
	reconstructed := <-replacementResult
	if reconstructed.err != nil {
		t.Fatalf("replacement reconstruction: %v", reconstructed.err)
	}

	if err := lease.Close(); err != nil {
		t.Fatalf("stale RunLease.Close: %v", err)
	}
	if got := first.closeCalls.Load(); got != 1 {
		t.Fatalf("first allocation close calls = %d, want 1", got)
	}
	if manager.GetItem(hash).getDAlloc() != replacement {
		t.Fatal("stale RunLease.Close detached replacement allocation")
	}
	if got := replacement.closeCalls.Load(); got != 0 {
		t.Fatalf("replacement close calls = %d, want 0", got)
	}
	if closed, closeErr := reconstructed.lease.Close(); closeErr != nil || !closed {
		t.Fatalf("replacement lease Close = (%v, %v), want (true, nil)", closed, closeErr)
	}
}
