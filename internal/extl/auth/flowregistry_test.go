package auth

import (
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

func TestFlowRegistry_StartReturnsFlowID(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	f, _, err := fr.Start(types.TokenKey{PluginID: "p"}, FlowKindPKCE)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID == "" {
		t.Fatal("expected non-empty flow id")
	}
}

func TestFlowRegistry_DuplicateJoinsExisting(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f1, _, _ := fr.Start(k, FlowKindPKCE)
	f2, joined, _ := fr.Start(k, FlowKindPKCE)
	if !joined {
		t.Fatal("second Start should report joined=true")
	}
	if f1.ID != f2.ID {
		t.Fatalf("expected same flow id, got %s vs %s", f1.ID, f2.ID)
	}
}

func TestFlowRegistry_ResolveUnblocksAwaiters(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)

	// Three awaiters — Start increments awaiters to 1 already; we need
	// two more joins so total awaiters == 3.
	fr.Start(k, FlowKindPKCE) // join #2
	fr.Start(k, FlowKindPKCE) // join #3

	var gotToks [3]atomic.Pointer[types.OAuth2Token]
	ready := make(chan struct{}, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	for i := 0; i < 3; i++ {
		i := i
		go func() {
			defer wg.Done()
			ready <- struct{}{}
			tok, err := fr.Await(f.ID)
			if err != nil {
				t.Errorf("awaiter %d: %v", i, err)
				return
			}
			gotToks[i].Store(tok)
		}()
	}
	// Wait for all 3 awaiters to start before resolving.
	for i := 0; i < 3; i++ {
		<-ready
	}
	// Give them a tiny moment to actually block in <-f.done.
	time.Sleep(20 * time.Millisecond)
	expected := &types.OAuth2Token{AccessToken: "A"}
	fr.Resolve(f.ID, expected, nil)
	wg.Wait()

	for i := 0; i < 3; i++ {
		got := gotToks[i].Load()
		if got == nil || got.AccessToken != "A" {
			t.Fatalf("awaiter %d: got %v", i, got)
		}
	}
}

func TestFlowRegistry_JoinedCallersBothSucceedSequentially(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f1, _, _ := fr.Start(k, FlowKindPKCE)
	f2, joined, _ := fr.Start(k, FlowKindPKCE)
	if !joined || f1.ID != f2.ID {
		t.Fatalf("join failed: joined=%v ids=%s/%s", joined, f1.ID, f2.ID)
	}

	fr.Resolve(f1.ID, &types.OAuth2Token{AccessToken: "A"}, nil)

	tok1, err := fr.Await(f1.ID)
	if err != nil {
		t.Fatalf("first Await: %v", err)
	}
	if tok1.AccessToken != "A" {
		t.Fatalf("first tok: %q", tok1.AccessToken)
	}
	// Critical: second Await must also succeed, even though first already cleaned up.
	tok2, err := fr.Await(f2.ID)
	if err != nil {
		t.Fatalf("second Await: %v", err)
	}
	if tok2.AccessToken != "A" {
		t.Fatalf("second tok: %q", tok2.AccessToken)
	}
}

func TestFlowRegistry_ResolvedFlowNotReusedByLaterStart(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f1, _, _ := fr.Start(k, FlowKindPKCE)
	fr.Resolve(f1.ID, &types.OAuth2Token{AccessToken: "OLD"}, nil)
	_, _ = fr.Await(f1.ID) // drain

	// Now a new caller asks for the same key — MUST get a fresh flow,
	// not the resolved stale one.
	f2, joined, err := fr.Start(k, FlowKindPKCE)
	if err != nil {
		t.Fatal(err)
	}
	if joined {
		t.Fatalf("new Start after Resolve should not join stale flow; joined=%v", joined)
	}
	if f2.ID == f1.ID {
		t.Fatalf("new flow got the same ID as the resolved one")
	}
}

func TestFlowRegistry_CancelReturnsError(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)

	done := make(chan error, 1)
	go func() { _, err := fr.Await(f.ID); done <- err }()
	time.Sleep(10 * time.Millisecond)
	fr.Cancel(f.ID, errors.New("user_cancel"))
	err := <-done
	if err == nil || err.Error() != "user_cancel" {
		t.Fatalf("expected user_cancel error, got %v", err)
	}
}

func TestFlowRegistry_TimeoutExpiresFlow(t *testing.T) {
	fr := NewFlowRegistry(20 * time.Millisecond)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)

	_, err := fr.Await(f.ID)
	if !errors.Is(err, ErrFlowTimeout) {
		t.Fatalf("expected ErrFlowTimeout, got %v", err)
	}
}

func TestFlowRegistry_AwaitUnknownErrors(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	_, err := fr.Await("does-not-exist")
	if !errors.Is(err, ErrFlowUnknown) {
		t.Fatalf("expected ErrFlowUnknown, got %v", err)
	}
}

func TestFlowRegistry_ShutdownIsIdempotent(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	fr.Shutdown()
	// Second call must not panic.
	fr.Shutdown()
	fr.Shutdown()
}

func TestFlowRegistry_ShutdownDoesNotDeadlockWithConcurrentStarts(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			k := types.TokenKey{PluginID: "p", Account: "a" + strconv.Itoa(i)}
			f, _, err := fr.Start(k, FlowKindPKCE)
			if err != nil {
				return // shutdown raced us; acceptable
			}
			// Don't actually await forever; we expect Cancel to fire.
			done := make(chan struct{})
			go func() {
				_, _ = fr.Await(f.ID)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Errorf("Await(%s) deadlocked", f.ID)
			}
		}()
	}
	close(start)
	time.Sleep(5 * time.Millisecond)
	fr.Shutdown()
	wg.Wait()
}
