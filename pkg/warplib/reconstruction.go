package warplib

import (
	"context"
	"fmt"
	"net/http"
)

// ReconstructionLease identifies one exact attempt to rebuild an Item's live
// downloader. Network probing does not hold Item locks; a newer lease or an
// explicit stop/close invalidates the older generation before it can commit.
type ReconstructionLease struct {
	item       *Item
	generation uint64
	allocation ProtocolDownloader
	owner      *allocationOwner
	handlers   *Handlers
	committed  bool
	// beforeInvokeForTest pauses after the run claim is acquired and before
	// calling the downloader. It is set only by deterministic package tests.
	beforeInvokeForTest func()
	// beforePersistForTest pauses after publication locks are released and
	// before the current manager map is persisted.
	beforePersistForTest func()
}

// BeginReconstruction invalidates older attempts and drains the allocation
// they could have already claimed before returning a lease for a replacement.
// Network probing happens after this short lifecycle barrier and holds no Item
// or Manager lock.
func (m *Manager) BeginReconstruction(hash string) (*ReconstructionLease, error) {
	return m.beginReconstruction(hash, nil)
}

func (m *Manager) beginReconstruction(
	hash string,
	guard func() bool,
) (*ReconstructionLease, error) {
	// A caller guard may acquire queue, pool, or extension locks. Stage it
	// before Item lifecycle locks so queue cleanup can safely take the inverse
	// path through Item.CloseDownloader. Exact generations below reject
	// intervening stop, close, or replacement transitions.
	if guard != nil && !guard() {
		return nil, ErrReconstructionSuperseded
	}
	item := m.GetItem(hash)
	if item == nil {
		return nil, ErrDownloadNotFound
	}
	item.reconstructionTransition.Lock()
	defer item.reconstructionTransition.Unlock()

	item.reconstructionMu.Lock()
	item.reconstructionGeneration++
	lease := &ReconstructionLease{
		item:       item,
		generation: item.reconstructionGeneration,
	}
	item.dAllocMu.Lock()
	previous := item.dAlloc
	previousOwner := item.allocationOwnerLocked()
	item.dAlloc = nil
	item.dAllocOwner = nil
	item.resumeHandlers = nil
	item.dAllocGeneration = 0
	item.dAllocMu.Unlock()
	drained := item.runsDrainedLocked()
	item.reconstructionMu.Unlock()

	previousOwner.stop()
	<-drained
	if previous != nil {
		if err := previousOwner.close(); err != nil {
			return nil, fmt.Errorf("close previous downloader: %w", err)
		}
	}

	item.reconstructionMu.Lock()
	current := item.reconstructionGeneration == lease.generation
	item.reconstructionMu.Unlock()
	if !current {
		return nil, ErrReconstructionSuperseded
	}
	return lease, nil
}

func (l *ReconstructionLease) belongsTo(item *Item) bool {
	return l != nil && item != nil && l.item == item
}

// Hash returns the Item hash bound to this lease.
func (l *ReconstructionLease) Hash() string {
	if l == nil || l.item == nil {
		return ""
	}
	return l.item.Hash
}

// IsCurrent reports whether this exact reconstruction attempt still owns the
// Item generation. Once committed, it also verifies that its allocation has
// not been replaced or detached.
func (l *ReconstructionLease) IsCurrent() bool {
	if l == nil || l.item == nil {
		return false
	}
	item := l.item
	item.reconstructionMu.Lock()
	defer item.reconstructionMu.Unlock()
	if item.reconstructionGeneration != l.generation {
		return false
	}
	if !l.committed {
		return true
	}
	item.dAllocMu.RLock()
	defer item.dAllocMu.RUnlock()
	return item.dAllocGeneration == l.generation &&
		item.dAllocOwner == l.owner &&
		item.dAlloc != nil
}

func (l *ReconstructionLease) commit(
	m *Manager,
	allocation ProtocolDownloader,
	handlers *Handlers,
	guard func() bool,
	mutate func(*Item),
) error {
	if l == nil || l.item == nil || allocation == nil {
		return ErrReconstructionSuperseded
	}
	// Never invoke an arbitrary caller guard under Item lifecycle locks.
	if guard != nil && !guard() {
		return ErrReconstructionSuperseded
	}
	item := l.item
	owner := newAllocationOwner(allocation)
	item.reconstructionTransition.Lock()
	item.reconstructionMu.Lock()
	if item.reconstructionGeneration != l.generation {
		item.reconstructionMu.Unlock()
		item.reconstructionTransition.Unlock()
		return ErrReconstructionSuperseded
	}

	m.mu.Lock()
	if m.items[item.Hash] != item {
		m.mu.Unlock()
		item.reconstructionMu.Unlock()
		item.reconstructionTransition.Unlock()
		return ErrDownloadNotFound
	}
	item.dAllocMu.Lock()
	if mutate != nil {
		mutate(item)
	}
	item.dAlloc = allocation
	item.dAllocOwner = owner
	item.resumeHandlers = handlers
	item.dAllocGeneration = l.generation
	l.allocation = allocation
	l.owner = owner
	l.handlers = handlers
	l.committed = true
	item.dAllocMu.Unlock()
	m.mu.Unlock()
	item.reconstructionMu.Unlock()
	item.reconstructionTransition.Unlock()

	// Persistence snapshots the current Item after all Item lifecycle locks
	// are released. If a newer transition wins here, the snapshot is newer and
	// this exact lease reports non-current; critically, the persister may take
	// queue.mu without inverting Queue.CloseWaitingDownloader's lock order.
	if l.beforePersistForTest != nil {
		l.beforePersistForTest()
	}
	m.persistCurrentItems()
	return nil
}

func (l *ReconstructionLease) claimAllocation() (ProtocolDownloader, *Handlers, func(), error) {
	if l == nil || l.item == nil {
		return nil, nil, nil, ErrReconstructionSuperseded
	}
	item := l.item
	item.reconstructionMu.Lock()
	defer item.reconstructionMu.Unlock()
	if item.reconstructionGeneration != l.generation || !l.committed {
		return nil, nil, nil, ErrReconstructionSuperseded
	}
	return item.claimDAllocLocked(l.generation, true)
}

// Start begins the fresh allocation committed by this exact lease.
func (l *ReconstructionLease) Start() error {
	return l.StartContext(context.Background())
}

// StartContext begins the fresh allocation committed by this exact lease
// within ctx.
func (l *ReconstructionLease) StartContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	allocation, handlers, release, err := l.claimAllocation()
	if err != nil {
		return err
	}
	defer release()
	if l.beforeInvokeForTest != nil {
		l.beforeInvokeForTest()
	}
	if allocation.IsStopped() {
		return nil
	}
	return allocation.Download(ctx, handlers)
}

// Resume resumes the allocation committed by this exact lease.
func (l *ReconstructionLease) Resume() error {
	return l.ResumeContext(context.Background())
}

// ResumeContext resumes the allocation committed by this exact lease within
// ctx.
func (l *ReconstructionLease) ResumeContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if l == nil || l.item == nil {
		return ErrReconstructionSuperseded
	}
	item := l.item
	allocation, handlers, release, err := l.claimAllocation()
	if err != nil {
		return err
	}
	defer release()
	if l.beforeInvokeForTest != nil {
		l.beforeInvokeForTest()
	}
	if allocation.IsStopped() {
		return nil
	}
	item.mu.RLock()
	partsCopy := make(map[int64]*ItemPart, len(item.Parts))
	for offset, part := range item.Parts {
		if part == nil {
			partsCopy[offset] = nil
			continue
		}
		partCopy := *part
		partsCopy[offset] = &partCopy
	}
	item.mu.RUnlock()
	return allocation.Resume(ctx, partsCopy, handlers)
}

// Close closes only the allocation committed by this lease. If a newer lease
// has replaced it, Close is a no-op and cannot disturb the replacement.
func (l *ReconstructionLease) Close() (bool, error) {
	if l == nil || l.item == nil {
		return false, nil
	}
	item := l.item
	item.reconstructionTransition.Lock()
	defer item.reconstructionTransition.Unlock()
	item.reconstructionMu.Lock()
	if item.reconstructionGeneration != l.generation || !l.committed {
		item.reconstructionMu.Unlock()
		return false, nil
	}
	item.dAllocMu.Lock()
	if item.dAllocGeneration != l.generation || item.dAlloc == nil {
		item.dAllocMu.Unlock()
		item.reconstructionMu.Unlock()
		return false, nil
	}
	owner := item.allocationOwnerLocked()
	if owner != l.owner {
		item.dAllocMu.Unlock()
		item.reconstructionMu.Unlock()
		return false, nil
	}
	item.dAlloc = nil
	item.dAllocOwner = nil
	item.resumeHandlers = nil
	item.dAllocGeneration = 0
	item.reconstructionGeneration++
	item.dAllocMu.Unlock()
	drained := item.runsDrainedLocked()
	item.reconstructionMu.Unlock()
	owner.stop()
	<-drained
	return true, owner.close()
}

// ResumeDownloadWithLease is a convenience for callers that need to retain
// the exact reconstruction token through Start/Resume and cleanup.
func cloneResumeDownloadOpts(opts *ResumeDownloadOpts) *ResumeDownloadOpts {
	if opts == nil {
		return &ResumeDownloadOpts{}
	}
	copyOpts := *opts
	return &copyOpts
}

func transferCommitGuard(ctx context.Context, guard func() bool) func() bool {
	return func() bool {
		return ctx != nil && ctx.Err() == nil && (guard == nil || guard())
	}
}

func (m *Manager) ResumeDownloadWithLease(
	client *http.Client,
	hash string,
	opts *ResumeDownloadOpts,
) (*Item, *ReconstructionLease, error) {
	transferCtx, done, admitted := m.admitTransfer()
	if !admitted {
		return nil, nil, ErrManagerShuttingDown
	}
	defer done()
	opts = cloneResumeDownloadOpts(opts)
	opts.CommitGuard = transferCommitGuard(transferCtx, opts.CommitGuard)
	lease, err := m.beginReconstruction(hash, opts.CommitGuard)
	if err != nil {
		return nil, nil, err
	}
	opts.ReconstructionLease = lease
	item, err := m.resumeDownload(transferCtx, client, hash, opts)
	return item, lease, err
}
