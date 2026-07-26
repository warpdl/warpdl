package server

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestPoolGenerationAccessorsDistinguishManagedRuns(t *testing.T) {
	pool := NewPool(nil)
	if got := (&Server{pool: pool}).Pool(); got != pool {
		t.Fatal("Server.Pool returned a different pool")
	}
	if got := (*TransferGeneration)(nil).UID(); got != "" {
		t.Fatalf("nil generation UID = %q", got)
	}

	managed, ok := pool.BeginDownload("managed", nil)
	if !ok {
		t.Fatal("reserve managed generation")
	}
	if managed.UID() != "managed" {
		t.Fatalf("managed UID = %q", managed.UID())
	}
	if current, found := pool.ManagedGeneration("managed"); !found || current != managed {
		t.Fatal("ManagedGeneration did not return the exact managed token")
	}
	managed.Abort()

	legacy, ok := pool.beginLegacyDownload("legacy", nil)
	if !ok {
		t.Fatal("reserve legacy generation")
	}
	if _, found := pool.ManagedGeneration("legacy"); found {
		t.Fatal("legacy generation was exposed as binary-API managed")
	}
	legacy.Abort()
}

func TestWrapServerRuntimeError(t *testing.T) {
	if err := wrapServerRuntimeError("IPC server", nil); err != nil {
		t.Fatalf("nil runtime error wrapped as %v", err)
	}
	cause := errors.New("serve failed")
	err := wrapServerRuntimeError("IPC server", cause)
	if !errors.Is(err, cause) || err.Error() != "IPC server failed: serve failed" {
		t.Fatalf("wrapped runtime error = %v", err)
	}
}

func TestTransferGenerationFinishIsDeliveryBarrier(t *testing.T) {
	pool := NewPool(nil)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	generation, ok := pool.BeginDownload("same-id", NewSyncConn(serverConn))
	if !ok {
		t.Fatal("BeginDownload failed")
	}
	if !generation.RecordTerminal([]byte("old terminal")) {
		t.Fatal("RecordTerminal failed")
	}
	if !generation.IsCurrent() {
		t.Fatal("terminal generation unexpectedly stopped being current")
	}
	if generation.IsRunnable() {
		t.Fatal("terminal generation remained runnable")
	}
	if generation.Broadcast([]byte("late progress")) {
		t.Fatal("progress was accepted after terminal record")
	}

	finished := make(chan bool, 1)
	go func() {
		finished <- generation.Finish(nil)
	}()

	select {
	case <-finished:
		t.Fatal("Finish returned before the terminal writer was read")
	case <-time.After(25 * time.Millisecond):
	}
	if !pool.HasDownload("same-id") {
		t.Fatal("UID became resumable before terminal delivery")
	}
	if _, reserved := pool.BeginDownload("same-id", nil); reserved {
		t.Fatal("replacement generation overlapped terminal delivery")
	}

	payload, err := NewSyncConn(clientConn).Read()
	if err != nil {
		t.Fatalf("read terminal: %v", err)
	}
	if string(payload) != "old terminal" {
		t.Fatalf("terminal payload = %q", payload)
	}
	select {
	case ok := <-finished:
		if !ok {
			t.Fatal("Finish rejected the current generation")
		}
	case <-time.After(time.Second):
		t.Fatal("Finish did not pass its delivery barrier")
	}

	replacement, reserved := pool.BeginDownload("same-id", nil)
	if !reserved {
		t.Fatal("replacement was not resumable after terminal delivery")
	}
	if generation.RecordTerminal([]byte("stale")) ||
		generation.Broadcast([]byte("stale")) ||
		generation.Abort() {
		t.Fatal("stale generation mutated its replacement")
	}
	replacement.Abort()
}

func TestBeginDownloadRejectsConcurrentGeneration(t *testing.T) {
	pool := NewPool(nil)
	first, ok := pool.BeginDownload("id", nil)
	if !ok {
		t.Fatal("first BeginDownload failed")
	}
	if _, ok := pool.BeginDownload("id", nil); ok {
		t.Fatal("concurrent BeginDownload succeeded")
	}
	if !first.Abort() {
		t.Fatal("Abort failed")
	}
	if _, ok := pool.BeginDownload("id", nil); !ok {
		t.Fatal("BeginDownload failed after abort")
	}
}
