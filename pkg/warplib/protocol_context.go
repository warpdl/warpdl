package warplib

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
)

// protocolOperationContext is cancelled when either the caller abandons the
// operation or the downloader is stopped.
func protocolOperationContext(caller, lifecycle context.Context) (context.Context, func()) {
	if caller == nil {
		caller = context.Background()
	}
	ctx, cancel := context.WithCancel(caller)
	var stopLifecycle func() bool
	if lifecycle != nil {
		if lifecycle.Err() != nil {
			// AfterFunc schedules callbacks asynchronously. Propagate an
			// already-cancelled lifecycle synchronously so an operation cannot
			// race ahead and begin network I/O first.
			cancel()
		} else {
			stopLifecycle = context.AfterFunc(lifecycle, cancel)
		}
	}
	return ctx, func() {
		if stopLifecycle != nil {
			stopLifecycle()
		}
		cancel()
	}
}

func notifyProtocolStopped(once *sync.Once, handlers *Handlers) {
	once.Do(func() {
		if handlers != nil && handlers.DownloadStoppedHandler != nil {
			handlers.DownloadStoppedHandler()
		}
	})
}

// isStopTransportError reports true only when every leaf in an error tree is
// a local cancellation/close error. errors.Is on an errors.Join value is not
// sufficient because one cancelled child must not mask a genuine sibling.
func isStopTransportError(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isStopTransportError(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			return isStopTransportError(child)
		}
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed)
}

// isTransferShutdownError additionally accepts lifecycle sentinels produced
// when shutdown invalidates an allocation between admission and its run claim.
// As with transport errors, every joined leaf must be shutdown-related.
func isTransferShutdownError(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isTransferShutdownError(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			return isTransferShutdownError(child)
		}
	}
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, ErrItemDownloaderNotFound) ||
		errors.Is(err, ErrReconstructionSuperseded) ||
		errors.Is(err, ErrManagerShuttingDown)
}

// NormalizeTransferError suppresses an expected cancellation/closed-transport
// result only after the tracked transfer context has been cancelled. Every
// leaf in a joined error tree must be local cancellation, net.ErrClosed, or a
// lifecycle invalidation sentinel; a substantive sibling is returned unchanged.
func NormalizeTransferError(ctx context.Context, err error) error {
	if err == nil || ctx == nil || ctx.Err() == nil {
		return err
	}
	if isTransferShutdownError(err) {
		return nil
	}
	return err
}

func finishProtocolOperation(
	completed, stopped bool,
	err error,
	once *sync.Once,
	handlers *Handlers,
) error {
	if completed || !stopped {
		return err
	}
	if err != nil && !isStopTransportError(err) {
		return err
	}
	notifyProtocolStopped(once, handlers)
	return nil
}

// contextReadCloser converts errors caused by closing an active protocol
// transport after cancellation into context.Canceled. Genuine I/O failures
// that occur while the operation context is live are preserved.
type contextReadCloser struct {
	context.Context
	io.ReadCloser
}

func (r *contextReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil && r.Context.Err() != nil {
		return n, r.Context.Err()
	}
	return n, err
}
