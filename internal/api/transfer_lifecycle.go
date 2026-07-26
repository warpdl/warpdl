package api

import (
	"context"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// managedTransferHandlers records terminal callbacks without removing the
// generation from the pool. Start/Resume's outer goroutine owns that final
// transition after every worker has drained.
func managedTransferHandlers(
	currentGeneration func() *server.TransferGeneration,
	isStopped func() bool,
	transferContexts ...context.Context,
) *warplib.Handlers {
	current := func() (*server.TransferGeneration, string, bool) {
		if currentGeneration == nil {
			return nil, "", false
		}
		generation := currentGeneration()
		if generation == nil {
			return nil, "", false
		}
		return generation, generation.UID(), true
	}
	stopped := func() bool {
		return isStopped != nil && isStopped()
	}
	broadcast := func(data []byte) {
		if generation, _, ok := current(); ok {
			generation.Broadcast(data)
		}
	}
	return &warplib.Handlers{
		ErrorHandler: func(_ string, err error) {
			if err == nil || (errors.Is(err, context.Canceled) && stopped()) {
				return
			}
			if len(transferContexts) > 0 &&
				warplib.NormalizeTransferError(transferContexts[0], err) == nil {
				return
			}
			generation, uid, ok := current()
			if !ok {
				return
			}
			generation.RecordTerminal(server.MakeDownloadError(uid, err))
			generation.WriteError(server.ErrorTypeCritical, err.Error())
		},
		ResumeProgressHandler: func(hash string, nread int) {
			_, uid, ok := current()
			if !ok {
				return
			}
			broadcast(server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.ResumeProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		DownloadProgressHandler: func(hash string, nread int) {
			_, uid, ok := current()
			if !ok {
				return
			}
			broadcast(server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		DownloadCompleteHandler: func(hash string, tread int64) {
			generation, uid, ok := current()
			if !ok {
				return
			}
			generation.RecordTerminal(server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadComplete,
				Value:      tread,
				Hash:       hash,
			}))
		},
		DownloadStoppedHandler: func() {
			generation, uid, ok := current()
			if !ok {
				return
			}
			generation.RecordTerminal(server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadStopped,
			}))
		},
		CompileStartHandler: func(hash string) {
			_, uid, ok := current()
			if !ok {
				return
			}
			broadcast(server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileStart,
				Hash:       hash,
			}))
		},
		CompileProgressHandler: func(hash string, nread int) {
			_, uid, ok := current()
			if !ok {
				return
			}
			broadcast(server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		CompileCompleteHandler: func(hash string, tread int64) {
			_, uid, ok := current()
			if !ok {
				return
			}
			broadcast(server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileComplete,
				Value:      tread,
				Hash:       hash,
			}))
		},
	}
}

func finishManagedTransfer(
	generation *server.TransferGeneration,
	item *warplib.Item,
	runErr error,
) bool {
	if generation == nil || !generation.IsCurrent() {
		return false
	}
	uid := generation.UID()
	var fallback []byte
	if runErr != nil {
		fallback = server.MakeDownloadError(uid, runErr)
		generation.RecordTerminal(fallback)
		generation.WriteError(server.ErrorTypeCritical, runErr.Error())
		if item != nil {
			_ = item.CloseDownloader()
		}
	} else {
		fallback = server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
			DownloadId: uid,
			Action:     common.DownloadStopped,
		})
	}
	return generation.Finish(fallback)
}

// ManagedTransferHandlers returns binary-API handlers for a reconstructed
// queued/scheduled transfer. Nil means no binary-API generation was captured.
func ManagedTransferHandlers(
	generation *server.TransferGeneration,
	manager *warplib.Manager,
) *warplib.Handlers {
	if generation == nil {
		return nil
	}
	uid := generation.UID()
	return managedTransferHandlers(func() *server.TransferGeneration {
		return generation
	}, func() bool {
		item := manager.GetItem(uid)
		return item == nil || item.IsStopped()
	}, manager.TransferContext())
}

// FinishManagedTransfer finalizes the captured binary-API generation after its
// Start/Resume call returns. It returns false when no managed generation was
// captured, so another frontend's finalizer remains responsible.
func FinishManagedTransfer(
	generation *server.TransferGeneration,
	runErr error,
) bool {
	if generation == nil {
		return false
	}
	// Returning true means this call carried a managed token, even when that
	// token has already gone stale. Callers must not fall back to a UID-based
	// terminal path in that case because it could target a replacement.
	finishManagedTransfer(generation, nil, runErr)
	return true
}

// finishLeaseManagedTransfer normalizes Manager shutdown cancellation before
// touching pool state. A substantive error closes only the allocation owned by
// the exact reconstruction lease, even if the pool generation is already
// stale; it can never detach a newer replacement.
func finishLeaseManagedTransfer(
	ctx context.Context,
	generation *server.TransferGeneration,
	lease *warplib.ReconstructionLease,
	runErr error,
) bool {
	runErr = warplib.NormalizeTransferError(ctx, runErr)
	if runErr != nil && lease != nil {
		_, closeErr := lease.Close()
		runErr = errors.Join(runErr, closeErr)
	}
	return finishManagedTransfer(generation, nil, runErr)
}

// finishRunLeaseManagedTransfer mirrors reconstruction finalization for a
// freshly added allocation. A substantive failure closes only the captured
// RunLease before the exact pool generation is finalized.
func finishRunLeaseManagedTransfer(
	ctx context.Context,
	generation *server.TransferGeneration,
	lease *warplib.RunLease,
	runErr error,
) bool {
	runErr = warplib.NormalizeTransferError(ctx, runErr)
	if runErr != nil && lease != nil {
		runErr = errors.Join(runErr, lease.Close())
	}
	return finishManagedTransfer(generation, nil, runErr)
}

// launchInitialRunLease transfers ownership of an already-acquired exact run
// claim to the Manager tracker. On rejection the callback cannot run, so this
// function synchronously closes the exact lease before removing registration
// state owned by the rejected request.
func (s *Api) launchInitialRunLease(
	generation *server.TransferGeneration,
	hash string,
	lease *warplib.RunLease,
) error {
	if s.manager.GoTransfer(func(ctx context.Context) {
		finishRunLeaseManagedTransfer(
			ctx,
			generation,
			lease,
			lease.StartContext(ctx),
		)
	}) {
		return nil
	}
	closeErr := lease.Close()
	cleanupErr := cleanupDownloadRegistrationState(s.manager, hash)
	// Remove the pool reservation last so no same-hash replacement can be
	// published while the rejected request still performs hash-keyed cleanup.
	generation.Abort()
	return errors.Join(
		warplib.ErrManagerShuttingDown,
		closeErr,
		cleanupErr,
	)
}
