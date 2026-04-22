package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

// FlowKind is PKCE (browser loopback) or Device (user-code polling).
type FlowKind string

const (
	FlowKindPKCE   FlowKind = "pkce"
	FlowKindDevice FlowKind = "device"
)

var (
	// ErrFlowUnknown is returned by Await when the flow id is not registered.
	ErrFlowUnknown = errors.New("flow: unknown flow id")
	// ErrFlowTimeout is returned when a flow exceeds the registry's timeout.
	ErrFlowTimeout = errors.New("flow: timed out")
	// ErrRegistryShutDown is returned by Start when the registry has been shut down.
	ErrRegistryShutDown = errors.New("flow: registry shut down")
)

// Flow is a running auth attempt. The registry owns its lifecycle.
type Flow struct {
	ID      string
	Kind    FlowKind
	Key     types.TokenKey
	Started time.Time

	// in-memory only; never persisted
	CodeVerifier string // PKCE
	State        string // CSRF guard
	DeviceCode   string // Device flow

	// done is closed exactly once when the flow resolves (success or error).
	// Closing it broadcasts completion to all awaiters.
	done chan struct{}
	// result holds the terminal outcome. Set before done is closed, read
	// after done is observed. The CAS on result serves as the "resolve
	// happens once" guard so timeout and Resolve races don't overwrite.
	result atomic.Pointer[flowResult]
	// awaiters counts the number of in-flight Await callers that still
	// expect to observe this flow's terminal result. Each Start (fresh or
	// join) increments by 1; each Await decrements by 1 on exit. Map
	// entries are only cleaned up when the count reaches zero so that
	// sequential Await callers joined via Start all observe the token
	// instead of racing each other to ErrFlowUnknown.
	awaiters atomic.Int64
}

type flowResult struct {
	tok *types.OAuth2Token
	err error
}

// FlowRegistry is an in-memory broker between plugin Token() calls and
// the RPC layer that completes flows. It is safe for concurrent use.
type FlowRegistry struct {
	mu      sync.Mutex
	byID    map[string]*Flow
	byKey   map[types.TokenKey]*Flow
	timeout time.Duration
	done    chan struct{} // closed by Shutdown
}

// NewFlowRegistry creates a registry. `timeout` is the per-flow deadline;
// a flow still running when the deadline expires is cancelled with
// ErrFlowTimeout.
func NewFlowRegistry(timeout time.Duration) *FlowRegistry {
	return &FlowRegistry{
		byID:    map[string]*Flow{},
		byKey:   map[types.TokenKey]*Flow{},
		timeout: timeout,
		done:    make(chan struct{}),
	}
}

// Start creates a new flow for key, or joins an existing one keyed by the
// same (plugin, account) pair. The second return is true iff we joined an
// already-running flow instead of creating a new one.
func (r *FlowRegistry) Start(key types.TokenKey, kind FlowKind) (*Flow, bool, error) {
	key = key.WithDefaultAccount()
	r.mu.Lock()
	select {
	case <-r.done:
		r.mu.Unlock()
		return nil, false, ErrRegistryShutDown
	default:
	}
	if existing, ok := r.byKey[key]; ok {
		// Only join if the existing flow isn't already resolved. A stale
		// resolved entry can linger while the last awaiter is still on
		// its way out (ref-counted cleanup); a new caller must get a
		// fresh flow rather than immediately observing the previous
		// caller's token.
		if existing.result.Load() == nil {
			existing.awaiters.Add(1)
			r.mu.Unlock()
			return existing, true, nil
		}
		// Stale resolved entry; drop it so we can create a fresh one.
		delete(r.byKey, key)
		delete(r.byID, existing.ID)
	}
	id, err := randomID()
	if err != nil {
		r.mu.Unlock()
		return nil, false, err
	}
	f := &Flow{
		ID:      id,
		Kind:    kind,
		Key:     key,
		Started: time.Now(),
		done:    make(chan struct{}),
	}
	f.awaiters.Store(1)
	r.byID[id] = f
	r.byKey[key] = f
	r.mu.Unlock()

	// Timeout watcher. If the registry is shut down or the flow completes
	// before the timeout, this goroutine exits without doing anything.
	go func() {
		select {
		case <-time.After(r.timeout):
			r.Cancel(id, ErrFlowTimeout)
		case <-f.done:
		case <-r.done:
		}
	}()

	return f, false, nil
}

// Await blocks until the flow is resolved, cancelled, or times out.
// Multiple goroutines may Await the same flow id concurrently (or
// sequentially, when joined via Start); every caller receives the same
// terminal result. The last awaiter to leave performs map cleanup so
// that a slow-to-call Await from a joined caller does not miss the
// result and see ErrFlowUnknown.
func (r *FlowRegistry) Await(id string) (*types.OAuth2Token, error) {
	r.mu.Lock()
	f, ok := r.byID[id]
	r.mu.Unlock()
	if !ok {
		return nil, ErrFlowUnknown
	}
	<-f.done
	res := f.result.Load()
	// Last awaiter out cleans up. The identity check on byKey guards
	// against a rare case where a new flow for the same key has taken
	// the slot (should be prevented elsewhere, but kept as a safety net).
	if f.awaiters.Add(-1) == 0 {
		r.mu.Lock()
		delete(r.byID, f.ID)
		if cur, ok := r.byKey[f.Key]; ok && cur.ID == f.ID {
			delete(r.byKey, f.Key)
		}
		r.mu.Unlock()
	}
	return res.tok, res.err
}

// Resolve delivers a terminal result to awaiters. Only the first call per
// flow wins; subsequent calls (including concurrent timeout firings) are
// no-ops. Safe from any goroutine.
func (r *FlowRegistry) Resolve(id string, tok *types.OAuth2Token, err error) {
	r.mu.Lock()
	f, ok := r.byID[id]
	r.mu.Unlock()
	if !ok {
		return
	}
	res := &flowResult{tok: tok, err: err}
	if f.result.CompareAndSwap(nil, res) {
		close(f.done)
	}
}

// Cancel resolves a flow with an error. If err is nil, a generic
// "cancelled" error is used.
func (r *FlowRegistry) Cancel(id string, err error) {
	if err == nil {
		err = errors.New("cancelled")
	}
	r.Resolve(id, nil, err)
}

// Get returns the live flow for id, or nil if none is registered.
func (r *FlowRegistry) Get(id string) *Flow {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id]
}

// Shutdown cancels every in-flight flow and signals all timeout
// goroutines to exit. Safe to call more than once; subsequent calls
// are no-ops.
func (r *FlowRegistry) Shutdown() {
	r.mu.Lock()
	select {
	case <-r.done:
		r.mu.Unlock()
		return
	default:
	}
	close(r.done)
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		r.Cancel(id, errors.New("registry shutdown"))
	}
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
