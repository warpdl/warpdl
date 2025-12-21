package warplib

import (
	"errors"
	"io"
	"log"
	"testing"
)

func TestHandlersSetDefault(t *testing.T) {
	called := false
	h := &Handlers{
		ErrorHandler: func(hash string, err error) {
			called = true
		},
	}
	h.setDefault(log.New(io.Discard, "", 0))
	if h.DownloadProgressHandler == nil || h.SpawnPartHandler == nil {
		t.Fatalf("expected default handlers to be set")
	}
	h.ErrorHandler("hash", errors.New("boom"))
	if !called {
		t.Fatalf("expected custom error handler to be called")
	}
}
