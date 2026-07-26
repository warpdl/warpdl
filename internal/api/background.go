package api

import (
	"context"
	"fmt"
	"sync"
)

// goBackground atomically admits one API-owned background task. Admission is
// closed by BeginShutdown before cancellation, preventing Add-vs-Wait races.
func (s *Api) goBackground(fn func(context.Context)) bool {
	if s == nil || fn == nil {
		return false
	}
	s.backgroundMu.Lock()
	if s.backgroundClosing || s.shutdownCtx == nil || s.shutdownCtx.Err() != nil {
		s.backgroundMu.Unlock()
		return false
	}
	if s.backgroundCond == nil {
		s.backgroundCond = sync.NewCond(&s.backgroundMu)
	}
	s.backgroundActive++
	ctx := s.shutdownCtx
	s.backgroundMu.Unlock()

	go func() {
		defer func() {
			s.backgroundMu.Lock()
			s.backgroundActive--
			if s.backgroundActive == 0 {
				s.backgroundCond.Broadcast()
			}
			s.backgroundMu.Unlock()
		}()
		fn(ctx)
	}()
	return true
}

// WaitBackground closes background admission and waits for every admitted
// task. BeginShutdown should normally be called first to cancel their context.
func (s *Api) WaitBackground(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.backgroundMu.Lock()
	if s.backgroundCond == nil {
		s.backgroundCond = sync.NewCond(&s.backgroundMu)
	}
	s.backgroundClosing = true
	stopWakeup := context.AfterFunc(ctx, func() {
		s.backgroundMu.Lock()
		s.backgroundCond.Broadcast()
		s.backgroundMu.Unlock()
	})
	for s.backgroundActive > 0 && ctx.Err() == nil {
		s.backgroundCond.Wait()
	}
	var err error
	if s.backgroundActive > 0 {
		err = ctx.Err()
	}
	s.backgroundMu.Unlock()
	stopWakeup()
	return err
}

// CloseContext drains API background work and Manager-owned transfers before
// final persistence. On timeout it leaves shared dependencies open so callers
// can retry after the blocked task unwinds.
func (s *Api) CloseContext(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.BeginShutdown()
	if err := s.WaitBackground(ctx); err != nil {
		return fmt.Errorf("drain API background work: %w", err)
	}
	if s.manager == nil {
		return nil
	}
	s.manager.CancelTransfers()
	if err := s.manager.WaitTransfers(ctx); err != nil {
		return fmt.Errorf("drain manager transfers: %w", err)
	}
	return s.manager.Close()
}
