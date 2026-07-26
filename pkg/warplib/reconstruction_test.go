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

type blockingReconstructionDownloader struct {
	hash          string
	name          string
	dir           string
	size          int64
	probeStarted  chan struct{}
	releaseProbe  chan struct{}
	closeCalls    atomic.Int32
	downloadCalls atomic.Int32
	stopCalls     atomic.Int32
	stopCalled    chan struct{}
	stopOnce      sync.Once
}

func (d *blockingReconstructionDownloader) Probe(context.Context) (ProbeResult, error) {
	close(d.probeStarted)
	<-d.releaseProbe
	return ProbeResult{
		FileName:      d.name,
		ContentLength: d.size,
		Resumable:     true,
	}, nil
}

func (d *blockingReconstructionDownloader) Download(context.Context, *Handlers) error {
	d.downloadCalls.Add(1)
	return nil
}

func (d *blockingReconstructionDownloader) Resume(context.Context, map[int64]*ItemPart, *Handlers) error {
	d.downloadCalls.Add(1)
	return nil
}

func (d *blockingReconstructionDownloader) Capabilities() DownloadCapabilities {
	return DownloadCapabilities{SupportsResume: true}
}

func (d *blockingReconstructionDownloader) Close() error {
	d.closeCalls.Add(1)
	return nil
}

func (d *blockingReconstructionDownloader) Stop() {
	d.stopCalls.Add(1)
	if d.stopCalled != nil {
		d.stopOnce.Do(func() { close(d.stopCalled) })
	}
}
func (d *blockingReconstructionDownloader) IsStopped() bool          { return false }
func (d *blockingReconstructionDownloader) GetMaxConnections() int32 { return 1 }
func (d *blockingReconstructionDownloader) GetMaxParts() int32       { return 1 }
func (d *blockingReconstructionDownloader) GetHash() string          { return d.hash }
func (d *blockingReconstructionDownloader) GetFileName() string      { return d.name }
func (d *blockingReconstructionDownloader) GetDownloadDirectory() string {
	return d.dir
}
func (d *blockingReconstructionDownloader) GetSavePath() string {
	return filepath.Join(d.dir, d.name)
}
func (d *blockingReconstructionDownloader) GetContentLength() ContentLength {
	return ContentLength(d.size)
}

type reconstructionResult struct {
	item  *Item
	lease *ReconstructionLease
	err   error
}

func TestResumeDownloadWithLeaseRejectsCancelReaddABA(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "reconstruction-aba"
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

	first := &blockingReconstructionDownloader{
		hash:         hash,
		name:         "first.bin",
		dir:          base,
		size:         111,
		probeStarted: make(chan struct{}),
		releaseProbe: make(chan struct{}),
	}
	replacement := &blockingReconstructionDownloader{
		hash:         hash,
		name:         "replacement.bin",
		dir:          base,
		size:         222,
		probeStarted: make(chan struct{}),
		releaseProbe: make(chan struct{}),
	}
	var factoryCalls atomic.Int32
	router := NewSchemeRouter(nil)
	router.Register("ftp", func(string, *DownloaderOpts) (ProtocolDownloader, error) {
		if factoryCalls.Add(1) == 1 {
			return first, nil
		}
		return replacement, nil
	})
	manager.SetSchemeRouter(router)

	firstResult := make(chan reconstructionResult, 1)
	go func() {
		item, lease, resumeErr := manager.ResumeDownloadWithLease(nil, hash, &ResumeDownloadOpts{
			Fresh:       true,
			CommitGuard: func() bool { return true },
		})
		firstResult <- reconstructionResult{item: item, lease: lease, err: resumeErr}
	}()
	<-first.probeStarted

	// This models cancel/re-add: the replacement begins while the old probe is
	// still blocked, invalidating the first Item reconstruction generation.
	replacementResult := make(chan reconstructionResult, 1)
	go func() {
		item, lease, resumeErr := manager.ResumeDownloadWithLease(nil, hash, &ResumeDownloadOpts{
			Fresh:       true,
			CommitGuard: func() bool { return true },
		})
		replacementResult <- reconstructionResult{item: item, lease: lease, err: resumeErr}
	}()
	<-replacement.probeStarted
	close(replacement.releaseProbe)
	second := <-replacementResult
	if second.err != nil {
		t.Fatalf("replacement reconstruction: %v", second.err)
	}
	if second.lease.Hash() != hash || !second.lease.IsCurrent() {
		t.Fatal("replacement lease did not retain exact current identity")
	}

	close(first.releaseProbe)
	stale := <-firstResult
	if !errors.Is(stale.err, ErrReconstructionSuperseded) {
		t.Fatalf("stale reconstruction error = %v, want %v", stale.err, ErrReconstructionSuperseded)
	}
	if stale.lease.IsCurrent() {
		t.Fatal("superseded reconstruction lease remained current")
	}
	if first.closeCalls.Load() != 1 {
		t.Fatalf("stale local downloader close calls = %d, want 1", first.closeCalls.Load())
	}
	if replacement.closeCalls.Load() != 0 {
		t.Fatal("replacement was closed by stale reconstruction failure")
	}

	// This is the old post-probe cleanup point. The stale lease must be unable
	// to close the allocation committed by the replacement lease.
	if closed, closeErr := stale.lease.Close(); closeErr != nil || closed {
		t.Fatalf("stale lease Close = (%v, %v), want (false, nil)", closed, closeErr)
	}
	if replacement.closeCalls.Load() != 0 {
		t.Fatal("stale lease closed replacement allocation")
	}
	if got := manager.GetItem(hash).Snapshot(); got.Name != replacement.name ||
		got.TotalSize != ContentLength(replacement.size) {
		t.Fatalf("committed metadata = (%q, %d), want (%q, %d)",
			got.Name, got.TotalSize, replacement.name, replacement.size)
	}
	if err := second.lease.Start(); err != nil {
		t.Fatalf("start replacement lease: %v", err)
	}
	if replacement.downloadCalls.Load() != 1 || first.downloadCalls.Load() != 0 {
		t.Fatalf("download calls first=%d replacement=%d",
			first.downloadCalls.Load(), replacement.downloadCalls.Load())
	}
	if closed, closeErr := second.lease.Close(); closeErr != nil || !closed {
		t.Fatalf("replacement Close = (%v, %v), want (true, nil)", closed, closeErr)
	}
	if second.lease.IsCurrent() {
		t.Fatal("closed reconstruction lease remained current")
	}
}

func TestResumeDownloadCommitGuardRejectsCancelledActivation(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "reconstruction-guard"
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
	candidate := &blockingReconstructionDownloader{
		hash:         hash,
		name:         "candidate.bin",
		dir:          base,
		size:         333,
		probeStarted: make(chan struct{}),
		releaseProbe: make(chan struct{}),
	}
	router := NewSchemeRouter(nil)
	router.Register("ftp", func(string, *DownloaderOpts) (ProtocolDownloader, error) {
		return candidate, nil
	})
	manager.SetSchemeRouter(router)

	var current atomic.Bool
	current.Store(true)
	result := make(chan reconstructionResult, 1)
	go func() {
		item, lease, resumeErr := manager.ResumeDownloadWithLease(nil, hash, &ResumeDownloadOpts{
			Fresh:       true,
			CommitGuard: current.Load,
		})
		result <- reconstructionResult{item: item, lease: lease, err: resumeErr}
	}()
	<-candidate.probeStarted
	current.Store(false)
	close(candidate.releaseProbe)
	got := <-result
	if !errors.Is(got.err, ErrReconstructionSuperseded) {
		t.Fatalf("guarded reconstruction error = %v, want %v", got.err, ErrReconstructionSuperseded)
	}
	if candidate.closeCalls.Load() != 1 {
		t.Fatalf("cancelled local downloader close calls = %d, want 1", candidate.closeCalls.Load())
	}
	item := manager.GetItem(hash)
	if item.IsDownloading() || item.Snapshot().Name != "old.bin" {
		t.Fatal("cancelled reconstruction published allocation or metadata")
	}
}

func TestResumeDownloadFalseCommitGuardPreservesLiveAllocation(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "reconstruction-false-guard"
	manager.UpdateItem(&Item{
		Hash:             hash,
		Name:             "live.bin",
		Url:              "ftp://example.invalid/live.bin",
		TotalSize:        10,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Protocol:         ProtoFTP,
		Parts:            make(map[int64]*ItemPart),
	})
	live := &blockingReconstructionDownloader{
		hash:         hash,
		name:         "live.bin",
		dir:          base,
		size:         10,
		probeStarted: make(chan struct{}),
		releaseProbe: make(chan struct{}),
	}
	item := manager.GetItem(hash)
	item.setDAlloc(live)

	_, lease, err := manager.ResumeDownloadWithLease(nil, hash, &ResumeDownloadOpts{
		Fresh:       true,
		CommitGuard: func() bool { return false },
	})
	if !errors.Is(err, ErrReconstructionSuperseded) {
		t.Fatalf("guarded reconstruction error = %v, want %v", err, ErrReconstructionSuperseded)
	}
	if lease != nil {
		t.Fatal("false guard returned a reconstruction lease")
	}
	if item.getDAlloc() != live || !item.IsDownloading() {
		t.Fatal("false guard detached the live allocation")
	}
	if live.stopCalls.Load() != 0 || live.closeCalls.Load() != 0 {
		t.Fatalf("false guard touched live allocation: stop=%d close=%d",
			live.stopCalls.Load(), live.closeCalls.Load())
	}
	if err := item.CloseDownloader(); err != nil {
		t.Fatalf("cleanup live allocation: %v", err)
	}
}

func TestReconstructionRunClaimBlocksPreInvokeReplacement(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "reconstruction-run-claim"
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
	first := &blockingReconstructionDownloader{
		hash:         hash,
		name:         "first.bin",
		dir:          base,
		size:         111,
		probeStarted: make(chan struct{}),
		releaseProbe: make(chan struct{}),
		stopCalled:   make(chan struct{}),
	}
	replacement := &blockingReconstructionDownloader{
		hash:         hash,
		name:         "replacement.bin",
		dir:          base,
		size:         222,
		probeStarted: make(chan struct{}),
		releaseProbe: make(chan struct{}),
	}
	close(first.releaseProbe)
	var factoryCalls atomic.Int32
	router := NewSchemeRouter(nil)
	router.Register("ftp", func(string, *DownloaderOpts) (ProtocolDownloader, error) {
		if factoryCalls.Add(1) == 1 {
			return first, nil
		}
		return replacement, nil
	})
	manager.SetSchemeRouter(router)

	_, firstLease, err := manager.ResumeDownloadWithLease(nil, hash, &ResumeDownloadOpts{
		Fresh:       true,
		CommitGuard: func() bool { return true },
	})
	if err != nil {
		t.Fatalf("first reconstruction: %v", err)
	}
	invokeClaimed := make(chan struct{})
	releaseInvoke := make(chan struct{})
	firstLease.beforeInvokeForTest = func() {
		close(invokeClaimed)
		<-releaseInvoke
	}
	firstRun := make(chan error, 1)
	go func() {
		firstRun <- firstLease.Start()
	}()
	<-invokeClaimed

	replacementResult := make(chan reconstructionResult, 1)
	go func() {
		item, lease, resumeErr := manager.ResumeDownloadWithLease(nil, hash, &ResumeDownloadOpts{
			Fresh:       true,
			CommitGuard: func() bool { return true },
		})
		replacementResult <- reconstructionResult{item: item, lease: lease, err: resumeErr}
	}()
	select {
	case <-first.stopCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement did not invalidate the claimed allocation")
	}
	if first.closeCalls.Load() != 0 {
		t.Fatal("claimed allocation was closed before its invocation returned")
	}
	select {
	case <-replacement.probeStarted:
		t.Fatal("replacement probing began before the prior run claim drained")
	case result := <-replacementResult:
		t.Fatalf("replacement returned before the prior run claim drained: %v", result.err)
	default:
	}

	close(releaseInvoke)
	if runErr := <-firstRun; runErr != nil {
		t.Fatalf("first Start: %v", runErr)
	}
	select {
	case <-replacement.probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement did not proceed after prior invocation returned")
	}
	close(replacement.releaseProbe)
	second := <-replacementResult
	if second.err != nil {
		t.Fatalf("replacement reconstruction: %v", second.err)
	}
	if first.downloadCalls.Load() != 1 {
		t.Fatalf("first download calls = %d, want 1", first.downloadCalls.Load())
	}
	if first.closeCalls.Load() != 1 {
		t.Fatalf("first close calls = %d, want 1 after run drain", first.closeCalls.Load())
	}
	if !second.lease.IsCurrent() {
		t.Fatal("replacement lease is not current")
	}
	if closed, closeErr := second.lease.Close(); closeErr != nil || !closed {
		t.Fatalf("replacement Close = (%v, %v), want (true, nil)", closed, closeErr)
	}
}

func TestBeginReconstructionGuardDoesNotHoldItemLocks(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "begin-guard-lock-order"
	live := &runLeaseDownloader{hash: hash, name: "live.bin", dir: base}
	item := newRunLeaseItem(hash, base, live)
	item.mu = manager.mu
	manager.UpdateItem(item)

	guardEntered := make(chan struct{})
	releaseGuard := make(chan struct{})
	beginResult := make(chan error, 1)
	go func() {
		_, beginErr := manager.beginReconstruction(hash, func() bool {
			close(guardEntered)
			<-releaseGuard
			return true
		})
		beginResult <- beginErr
	}()
	<-guardEntered

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- item.CloseDownloader()
	}()
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("CloseDownloader: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guard held an Item lifecycle lock needed by CloseDownloader")
	}
	close(releaseGuard)
	select {
	case err := <-beginResult:
		if err != nil {
			t.Fatalf("BeginReconstruction: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("BeginReconstruction did not return after guard release")
	}
}

func TestReconstructionCommitPersistenceDoesNotInvertQueueItemLocks(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "commit-persist-lock-order"
	item := &Item{
		Hash:             hash,
		Name:             "old.bin",
		Url:              "ftp://example.invalid/old.bin",
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Protocol:         ProtoFTP,
		Parts:            make(map[int64]*ItemPart),
		memPart:          make(map[string]int64),
		mu:               manager.mu,
	}
	manager.UpdateItem(item)
	lease, err := manager.BeginReconstruction(hash)
	if err != nil {
		t.Fatalf("BeginReconstruction: %v", err)
	}

	queue := NewQueueManager(1, nil)
	queue.Add("slot-holder", PriorityNormal)
	queue.Add(hash, PriorityNormal)
	manager.queue.Store(queue)

	candidate := &runLeaseDownloader{
		hash: hash,
		name: "candidate.bin",
		dir:  base,
	}
	commitLocked := make(chan struct{})
	releaseCommit := make(chan struct{})
	commitResult := make(chan error, 1)
	go func() {
		commitResult <- lease.commit(manager, candidate, nil, nil, func(*Item) {
			close(commitLocked)
			<-releaseCommit
		})
	}()
	<-commitLocked

	queueCallbackEntered := make(chan struct{})
	queueResult := make(chan error, 1)
	go func() {
		_, closeErr := queue.runIfWaiting(hash, func() error {
			close(queueCallbackEntered)
			return item.CloseDownloader()
		})
		queueResult <- closeErr
	}()
	<-queueCallbackEntered
	close(releaseCommit)

	select {
	case err := <-queueResult:
		if err != nil {
			t.Fatalf("queue close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queue close deadlocked with reconstruction persistence")
	}
	select {
	case err := <-commitResult:
		if err != nil {
			t.Fatalf("reconstruction commit: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconstruction persistence deadlocked with queue close")
	}
	if lease.IsCurrent() {
		t.Fatal("queue close did not invalidate the committed exact lease")
	}
	if got := candidate.closeCalls.Load(); got != 1 {
		t.Fatalf("candidate close calls = %d, want 1", got)
	}
}

func TestReconstructionCommitPersistenceCannotRemapReplacementItem(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	const hash = "commit-persist-item-aba"
	original := &Item{
		Hash:             hash,
		Name:             "original.bin",
		Url:              "ftp://example.invalid/original.bin",
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Protocol:         ProtoFTP,
		Parts:            make(map[int64]*ItemPart),
		memPart:          make(map[string]int64),
		mu:               manager.mu,
	}
	manager.UpdateItem(original)
	lease, err := manager.BeginReconstruction(hash)
	if err != nil {
		t.Fatalf("BeginReconstruction: %v", err)
	}

	candidate := &runLeaseDownloader{
		hash: hash,
		name: "candidate.bin",
		dir:  base,
	}
	published := make(chan struct{})
	releasePersist := make(chan struct{})
	lease.beforePersistForTest = func() {
		close(published)
		<-releasePersist
	}
	commitResult := make(chan error, 1)
	go func() {
		commitResult <- lease.commit(manager, candidate, nil, nil, nil)
	}()
	<-published

	replacementAllocation := &runLeaseDownloader{
		hash: hash,
		name: "replacement.bin",
		dir:  base,
	}
	replacement := newRunLeaseItem(hash, base, replacementAllocation)
	replacement.mu = manager.mu
	manager.UpdateItem(replacement)
	close(releasePersist)
	if err := <-commitResult; err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got := manager.GetItem(hash); got != replacement {
		t.Fatal("stale commit persistence remapped the replaced Item")
	}
	if closed, closeErr := lease.Close(); closeErr != nil || !closed {
		t.Fatalf("stale Item lease Close = (%v, %v), want (true, nil)", closed, closeErr)
	}
	if replacement.getDAlloc() != replacementAllocation {
		t.Fatal("stale Item lease cleanup detached replacement allocation")
	}
}
