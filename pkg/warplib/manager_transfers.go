package warplib

import (
	"context"
	"sync"
)

func (m *Manager) initTransferLifetime() {
	m.transferCtx, m.transferCancel = context.WithCancel(context.Background())
	m.transferCond = sync.NewCond(&m.transferMu)
}

func (m *Manager) ensureTransferLifetimeLocked() {
	if m.transferCtx == nil || m.transferCond == nil {
		m.initTransferLifetime()
	}
}

func (m *Manager) finishTransferLocked() {
	m.transferActive--
	if m.transferActive == 0 {
		m.transferCond.Broadcast()
	}
}

func (m *Manager) transferDone() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			m.transferMu.Lock()
			m.finishTransferLocked()
			m.transferMu.Unlock()
		})
	}
}

// TransferContext is cancelled when the Manager begins shutting down.
func (m *Manager) TransferContext() context.Context {
	if m == nil {
		return context.Background()
	}
	m.transferMu.Lock()
	m.ensureTransferLifetimeLocked()
	ctx := m.transferCtx
	m.transferMu.Unlock()
	return ctx
}

func (m *Manager) admitTransfer() (context.Context, func(), bool) {
	if m == nil {
		return nil, nil, false
	}
	m.transferMu.Lock()
	m.ensureTransferLifetimeLocked()
	if m.transferClosing || m.transferCtx.Err() != nil {
		m.transferMu.Unlock()
		return m.transferCtx, nil, false
	}
	m.transferActive++
	ctx := m.transferCtx
	done := m.transferDone()
	m.transferMu.Unlock()
	return ctx, done, true
}

// GoTransfer atomically admits fn to the Manager lifetime and runs it in a new
// goroutine. It returns false after transfer shutdown has begun; in that case
// fn is never called and the caller retains ownership of any pending cleanup.
func (m *Manager) GoTransfer(fn func(context.Context)) bool {
	if fn == nil {
		return false
	}
	ctx, done, admitted := m.admitTransfer()
	if !admitted {
		return false
	}
	go func() {
		defer done()
		fn(ctx)
	}()
	return true
}

// CancelTransfers closes transfer admission and cancels the shared Manager
// lifetime. It does not wait for admitted functions to return.
func (m *Manager) CancelTransfers() {
	if m == nil {
		return
	}
	m.transferMu.Lock()
	m.ensureTransferLifetimeLocked()
	m.transferClosing = true
	cancel := m.transferCancel
	m.transferCond.Broadcast()
	m.transferMu.Unlock()
	cancel()
}

// WaitTransfers closes transfer admission and waits for every already-admitted
// synchronous or goroutine transfer to return. The wait is bounded by ctx.
func (m *Manager) WaitTransfers(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.transferMu.Lock()
	m.ensureTransferLifetimeLocked()
	m.transferClosing = true
	stopWakeup := context.AfterFunc(ctx, func() {
		m.transferMu.Lock()
		m.transferCond.Broadcast()
		m.transferMu.Unlock()
	})
	for m.transferActive > 0 && ctx.Err() == nil {
		m.transferCond.Wait()
	}
	var err error
	if m.transferActive > 0 {
		err = ctx.Err()
	}
	m.transferMu.Unlock()
	stopWakeup()
	return err
}
