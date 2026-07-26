package api

import (
	"errors"
	"testing"

	"github.com/warpdl/warpdl/internal/server"
)

func TestFinishManagedTransferUsesExactGeneration(t *testing.T) {
	pool := server.NewPool(nil)
	first, ok := pool.BeginDownload("same-id", nil)
	if !ok {
		t.Fatal("reserve first generation")
	}
	if !first.Abort() {
		t.Fatal("abort first generation")
	}
	replacement, ok := pool.BeginDownload("same-id", nil)
	if !ok {
		t.Fatal("reserve replacement generation")
	}

	if !FinishManagedTransfer(first, errors.New("stale failure")) {
		t.Fatal("managed stale token was not recognized")
	}
	if !replacement.IsCurrent() || !pool.HasDownload("same-id") {
		t.Fatal("stale finalizer removed its replacement")
	}
	if got := pool.GetError("same-id"); got != nil {
		t.Fatalf("stale finalizer wrote replacement error: %+v", got)
	}

	replacement.RecordTerminal([]byte("replacement terminal"))
	if !replacement.Finish(nil) {
		t.Fatal("finish replacement generation")
	}
}
