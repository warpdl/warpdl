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

	var gotTok atomic.Pointer[types.OAuth2Token]
	var wg sync.WaitGroup
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func() {
			defer wg.Done()
			tok, err := fr.Await(f.ID)
			if err != nil {
				t.Errorf("Await: %v", err)
				return
			}
			gotTok.Store(tok)
		}()
	}
	time.Sleep(10 * time.Millisecond)
	expected := &types.OAuth2Token{AccessToken: "A"}
	fr.Resolve(f.ID, expected, nil)
	wg.Wait()
	if gotTok.Load().AccessToken != "A" {
		t.Fatal("awaiters did not receive resolved token")
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
