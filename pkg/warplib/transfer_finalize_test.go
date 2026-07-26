package warplib

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

type recordingSyncCloser struct {
	calls    *[]string
	syncErr  error
	closeErr error
}

func TestFinishProtocolOperationPreservesGenuineErrorDuringStop(t *testing.T) {
	genuineErr := errors.New("genuine protocol failure")
	var once sync.Once
	stoppedCalls := 0
	handlers := &Handlers{
		DownloadStoppedHandler: func() { stoppedCalls++ },
	}

	err := finishProtocolOperation(false, true, genuineErr, &once, handlers)
	if !errors.Is(err, genuineErr) {
		t.Fatalf("finishProtocolOperation error = %v, want genuine failure", err)
	}
	if stoppedCalls != 0 {
		t.Fatalf("DownloadStopped calls = %d, want 0 for genuine failure", stoppedCalls)
	}

	err = finishProtocolOperation(false, true, context.Canceled, &once, handlers)
	if err != nil {
		t.Fatalf("cancellation result = %v, want nil", err)
	}
	err = finishProtocolOperation(false, true, nil, &once, handlers)
	if err != nil {
		t.Fatalf("repeated stopped result = %v, want nil", err)
	}
	if stoppedCalls != 1 {
		t.Fatalf("DownloadStopped calls = %d, want exactly 1", stoppedCalls)
	}
}

func (c *recordingSyncCloser) Sync() error {
	*c.calls = append(*c.calls, "local sync")
	return c.syncErr
}

func (c *recordingSyncCloser) Close() error {
	*c.calls = append(*c.calls, "local close")
	return c.closeErr
}

type recordingCloser struct {
	calls *[]string
	err   error
}

func (c *recordingCloser) Close() error {
	*c.calls = append(*c.calls, "remote close")
	return c.err
}

func appendTailOnFinalProgress(path string, expected int64) (func(string, int), func() error) {
	var (
		total     atomic.Int64
		once      sync.Once
		appendErr error
	)
	handler := func(_ string, n int) {
		if total.Add(int64(n)) < expected {
			return
		}
		once.Do(func() {
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				appendErr = err
				return
			}
			if _, err = f.Write([]byte("tail")); err != nil {
				appendErr = err
			}
			if err = f.Close(); appendErr == nil {
				appendErr = err
			}
		})
	}
	return handler, func() error { return appendErr }
}

func TestFinalizeProtocolTransferAggregatesAllErrors(t *testing.T) {
	syncErr := errors.New("sync failed")
	localCloseErr := errors.New("local close failed")
	remoteCloseErr := errors.New("remote close failed")
	var calls []string

	err := finalizeProtocolTransfer(
		&recordingSyncCloser{calls: &calls, syncErr: syncErr, closeErr: localCloseErr},
		&recordingCloser{calls: &calls, err: remoteCloseErr},
	)
	for _, want := range []error{syncErr, localCloseErr, remoteCloseErr} {
		if !errors.Is(err, want) {
			t.Fatalf("finalize error %v does not contain %v", err, want)
		}
	}
	wantCalls := []string{"local sync", "local close", "remote close"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestValidatePhysicalFileSize(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "destination-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := validatePhysicalFileSize(f, 4); err != nil {
		t.Fatalf("exact size: %v", err)
	}
	if err := validatePhysicalFileSize(f, 3); !errors.Is(err, ErrDownloadSizeMismatch) {
		t.Fatalf("oversized destination error = %v, want %v", err, ErrDownloadSizeMismatch)
	}
	if err := validatePhysicalFileSize(nil, 4); !errors.Is(err, ErrDownloadDataMissing) {
		t.Fatalf("nil destination error = %v, want %v", err, ErrDownloadDataMissing)
	}
}
