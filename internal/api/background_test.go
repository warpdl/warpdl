package api

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"
)

func TestApiWaitBackgroundBoundsAndRetries(t *testing.T) {
	api, err := NewApi(log.New(io.Discard, "", 0), nil, nil, nil, nil, nil, "", "", "")
	if err != nil {
		t.Fatalf("NewApi: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	if !api.goBackground(func(context.Context) {
		close(started)
		<-release // Model an upstream call that is slow to honor cancellation.
		close(finished)
	}) {
		t.Fatal("background task was not admitted")
	}
	<-started
	api.BeginShutdown()

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err = api.WaitBackground(shortCtx)
	shortCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitBackground error = %v, want deadline exceeded", err)
	}
	if api.goBackground(func(context.Context) {}) {
		t.Fatal("background task admitted after shutdown")
	}

	close(release)
	retryCtx, retryCancel := context.WithTimeout(context.Background(), time.Second)
	defer retryCancel()
	if err := api.WaitBackground(retryCtx); err != nil {
		t.Fatalf("WaitBackground retry: %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("successful wait returned before background finalizer")
	}
}
