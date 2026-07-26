package warplib

import (
	"context"
	"reflect"
	"sync"
)

type allocationOwner struct {
	allocation ProtocolDownloader
	stopOnce   sync.Once
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeErr   error
}

func newAllocationOwner(allocation ProtocolDownloader) *allocationOwner {
	if allocation == nil {
		return nil
	}
	return &allocationOwner{
		allocation: allocation,
		closeDone:  make(chan struct{}),
	}
}

func (o *allocationOwner) stop() {
	if o != nil && o.allocation != nil {
		o.stopOnce.Do(o.allocation.Stop)
	}
}

func (o *allocationOwner) close() error {
	if o == nil || o.allocation == nil {
		return nil
	}
	o.closeOnce.Do(func() {
		o.stop()
		o.closeErr = o.allocation.Close()
		close(o.closeDone)
	})
	<-o.closeDone
	return o.closeErr
}

func sameProtocolDownloader(left, right ProtocolDownloader) bool {
	if left == nil || right == nil {
		return false
	}
	leftType := reflect.TypeOf(left)
	if leftType != reflect.TypeOf(right) || !leftType.Comparable() {
		return false
	}
	return reflect.ValueOf(left).Interface() == reflect.ValueOf(right).Interface()
}

func (i *Item) allocationOwnerLocked() *allocationOwner {
	if i.dAlloc == nil {
		return nil
	}
	if i.dAllocOwner == nil {
		i.dAllocOwner = newAllocationOwner(i.dAlloc)
	}
	return i.dAllocOwner
}

type runLeaseState uint8

const (
	runLeasePending runLeaseState = iota
	runLeaseRunning
	runLeaseFinished
	runLeaseClosed
	runLeaseAbandoned
)

// RunLease owns one exact Item allocation and a run claim acquired before an
// asynchronous start is admitted. A lease is one-shot: Start/StartContext may
// be called once, and Close safely releases an unstarted claim or drains a
// running call before closing only the captured allocation.
type RunLease struct {
	item       *Item
	owner      *allocationOwner
	allocation ProtocolDownloader
	handlers   *Handlers
	generation uint64
	release    func()

	stateMu sync.Mutex
	state   runLeaseState
	runDone chan struct{}

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func (i *Item) acquireRunLease(
	match func(ProtocolDownloader) bool,
) (*RunLease, error) {
	if i == nil {
		return nil, ErrItemDownloaderNotFound
	}
	i.reconstructionMu.Lock()
	i.dAllocMu.Lock()
	allocation := i.dAlloc
	if allocation == nil {
		i.dAllocMu.Unlock()
		i.reconstructionMu.Unlock()
		return nil, ErrItemDownloaderNotFound
	}
	if match != nil && !match(allocation) {
		i.dAllocMu.Unlock()
		i.reconstructionMu.Unlock()
		return nil, ErrReconstructionSuperseded
	}
	owner := i.allocationOwnerLocked()
	handlers := i.resumeHandlers
	generation := i.dAllocGeneration
	i.dAllocMu.Unlock()
	release := i.claimRunLocked()
	i.reconstructionMu.Unlock()

	return &RunLease{
		item:       i,
		owner:      owner,
		allocation: allocation,
		handlers:   handlers,
		generation: generation,
		release:    release,
		state:      runLeasePending,
		runDone:    make(chan struct{}),
		closeDone:  make(chan struct{}),
	}, nil
}

// AcquireRunLease synchronously claims the Item's exact current allocation.
func (i *Item) AcquireRunLease() (*RunLease, error) {
	return i.acquireRunLease(nil)
}

func (m *Manager) acquireTypedRunLease(
	hash string,
	match func(ProtocolDownloader) bool,
) (*RunLease, error) {
	if m == nil {
		return nil, ErrManagerShuttingDown
	}
	m.transferMu.Lock()
	m.ensureTransferLifetimeLocked()
	if m.transferClosing || m.transferCtx.Err() != nil {
		m.transferMu.Unlock()
		return nil, ErrManagerShuttingDown
	}
	m.transferActive++
	managerDone := m.transferDone()
	item := m.GetItem(hash)
	if item == nil {
		m.finishTransferLocked()
		m.transferMu.Unlock()
		return nil, ErrDownloadNotFound
	}
	lease, err := item.acquireRunLease(match)
	if err != nil {
		m.finishTransferLocked()
		m.transferMu.Unlock()
		return nil, err
	}
	itemRelease := lease.release
	lease.release = func() {
		itemRelease()
		managerDone()
	}
	m.transferMu.Unlock()
	return lease, nil
}

// AcquireRunLease synchronously claims hash's exact current allocation and
// accounts the pending claim as Manager-owned transfer work.
func (m *Manager) AcquireRunLease(hash string) (*RunLease, error) {
	return m.acquireTypedRunLease(hash, nil)
}

// AcquireDownloadRunLease claims hash only when its current HTTP allocation
// wraps the exact Downloader supplied by the caller.
func (m *Manager) AcquireDownloadRunLease(
	hash string,
	downloader *Downloader,
) (*RunLease, error) {
	if downloader == nil {
		return nil, ErrItemDownloaderNotFound
	}
	return m.acquireTypedRunLease(hash, func(allocation ProtocolDownloader) bool {
		httpAllocation, ok := allocation.(*httpProtocolDownloader)
		return ok && httpAllocation.inner == downloader
	})
}

// AcquireProtocolRunLease claims hash only when its current protocol
// allocation is the exact object supplied by the caller.
func (m *Manager) AcquireProtocolRunLease(
	hash string,
	downloader ProtocolDownloader,
) (*RunLease, error) {
	if downloader == nil {
		return nil, ErrItemDownloaderNotFound
	}
	return m.acquireTypedRunLease(hash, func(allocation ProtocolDownloader) bool {
		return sameProtocolDownloader(allocation, downloader)
	})
}

// Start begins the captured allocation with a background caller context.
func (l *RunLease) Start() error {
	return l.StartContext(context.Background())
}

// StartContext begins the captured allocation exactly once while retaining the
// synchronously acquired run claim through the protocol call.
func (l *RunLease) StartContext(ctx context.Context) error {
	if l == nil || l.owner == nil || l.allocation == nil {
		return ErrRunLeaseUsed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	l.stateMu.Lock()
	if l.state != runLeasePending {
		l.stateMu.Unlock()
		return ErrRunLeaseUsed
	}
	l.state = runLeaseRunning
	l.stateMu.Unlock()

	defer func() {
		l.release()
		l.stateMu.Lock()
		l.state = runLeaseFinished
		close(l.runDone)
		l.stateMu.Unlock()
	}()
	if !l.allocation.IsStopped() {
		return l.allocation.Download(ctx, l.handlers)
	}
	return nil
}

func (l *RunLease) detachExact() {
	item := l.item
	if item == nil {
		return
	}
	item.reconstructionTransition.Lock()
	item.reconstructionMu.Lock()
	item.dAllocMu.Lock()
	if item.dAllocOwner == l.owner &&
		item.dAllocGeneration == l.generation {
		item.dAlloc = nil
		item.dAllocOwner = nil
		item.resumeHandlers = nil
		item.dAllocGeneration = 0
		item.reconstructionGeneration++
	}
	item.dAllocMu.Unlock()
	item.reconstructionMu.Unlock()
	item.reconstructionTransition.Unlock()
}

// Abandon relinquishes a pending claim without stopping, detaching, or
// closing its captured allocation. It is used when a caller discovers that
// its own queue/pool activation became stale after acquisition even though
// the allocation itself may belong to a newer activation. Abandon is
// idempotent; Start is rejected and a later Close remains a no-op.
func (l *RunLease) Abandon() error {
	if l == nil {
		return nil
	}
	l.stateMu.Lock()
	switch l.state {
	case runLeaseAbandoned:
		l.stateMu.Unlock()
		return nil
	case runLeasePending:
		l.state = runLeaseAbandoned
	default:
		l.stateMu.Unlock()
		return ErrRunLeaseUsed
	}
	l.stateMu.Unlock()
	if l.release != nil {
		l.release()
	}
	if l.runDone != nil {
		close(l.runDone)
	}
	return nil
}

// Close stops and closes only the captured allocation. Before Start it also
// releases the pending claim; during/after Start it waits for the exact call to
// return. Repeated calls are safe.
func (l *RunLease) Close() error {
	if l == nil {
		return nil
	}
	l.stateMu.Lock()
	if l.closeDone == nil {
		l.closeDone = make(chan struct{})
	}
	closeDone := l.closeDone
	l.stateMu.Unlock()
	l.closeOnce.Do(func() {
		l.stateMu.Lock()
		state := l.state
		if state == runLeasePending {
			l.state = runLeaseClosed
		}
		l.stateMu.Unlock()
		if state == runLeaseAbandoned {
			close(closeDone)
			return
		}

		l.owner.stop()
		switch state {
		case runLeasePending:
			if l.release != nil {
				l.release()
			}
			if l.runDone != nil {
				close(l.runDone)
			}
		case runLeaseRunning:
			if l.runDone != nil {
				<-l.runDone
			}
		}
		l.detachExact()
		l.closeErr = l.owner.close()
		l.stateMu.Lock()
		l.state = runLeaseClosed
		l.stateMu.Unlock()
		close(closeDone)
	})
	<-closeDone
	return l.closeErr
}
