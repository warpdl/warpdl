package server

import (
	"io"
	"log"
	"sync"
	"time"
)

const (
	poolBroadcastQueueSize = 64
	poolWriteTimeout       = 2 * time.Second
)

type poolSubscriptionKey struct {
	uid  string
	conn *SyncConn
}

type poolSubscriber struct {
	queue     chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

// TransferGeneration identifies one active incarnation of a download UID.
// The value is intentionally opaque: callers can only operate on the exact
// generation returned by BeginDownload, so a late callback from an older run
// cannot broadcast into or remove a replacement run with the same UID.
//
// All mutable fields are protected by Pool.mu.
type TransferGeneration struct {
	pool          *Pool
	uid           string
	serial        uint64
	managed       bool
	terminal      bool
	terminalData  []byte
	terminalizing bool
	finishDone    chan struct{}
	finishClosed  bool
}

// Pool manages active download connections and their associated errors.
// It provides thread-safe operations for tracking which downloads are active,
// broadcasting messages to all connections watching a download, and storing
// errors that occur during downloads.
type Pool struct {
	l  *log.Logger
	mu *sync.RWMutex
	m  map[string][]*SyncConn
	e  map[string]*Error
	// generations tracks the exact live incarnation behind each m entry.
	// Legacy AddDownload registrations receive an unmanaged generation;
	// BeginDownload creates a managed generation used by the binary API.
	generations map[string]*TransferGeneration
	nextSerial  uint64
	// subscribers are created lazily on the first broadcast so merely
	// attaching a connection does not allocate a goroutine.
	subs map[poolSubscriptionKey]*poolSubscriber
}

// NewPool creates a new Pool instance with the given logger.
// The pool is initialized with empty connection and error maps.
func NewPool(l *log.Logger) *Pool {
	return &Pool{
		l:           l,
		mu:          &sync.RWMutex{},
		m:           make(map[string][]*SyncConn),
		e:           make(map[string]*Error),
		generations: make(map[string]*TransferGeneration),
		subs:        make(map[poolSubscriptionKey]*poolSubscriber),
	}
}

func newPoolSubscriber() *poolSubscriber {
	return &poolSubscriber{
		queue: make(chan []byte, poolBroadcastQueueSize),
		done:  make(chan struct{}),
	}
}

func (p *Pool) newGenerationLocked(uid string, managed bool) *TransferGeneration {
	p.nextSerial++
	generation := &TransferGeneration{
		pool:       p,
		uid:        uid,
		serial:     p.nextSerial,
		managed:    managed,
		finishDone: make(chan struct{}),
	}
	p.generations[uid] = generation
	return generation
}

func (p *Pool) closeGenerationLocked(generation *TransferGeneration) {
	if generation == nil || generation.finishClosed {
		return
	}
	generation.finishClosed = true
	close(generation.finishDone)
}

// HasDownload reports whether a download with the given unique identifier exists in the pool.
func (p *Pool) HasDownload(uid string) bool {
	p.mu.RLock()
	_, ok := p.m[uid]
	p.mu.RUnlock()
	return ok
}

// BeginDownload atomically reserves a new managed generation for uid.
// It fails while any prior generation is active or still delivering its
// terminal frame. This closes both concurrent-resume and stop/resume races.
func (p *Pool) BeginDownload(uid string, sconn *SyncConn) (*TransferGeneration, bool) {
	return p.beginDownload(uid, sconn, true)
}

// beginLegacyDownload atomically reserves an unmanaged server/RPC generation.
// Unlike AddDownload followed by CurrentGeneration, it cannot accidentally
// capture an older run that won the same UID concurrently.
func (p *Pool) beginLegacyDownload(uid string, sconn *SyncConn) (*TransferGeneration, bool) {
	return p.beginDownload(uid, sconn, false)
}

func (p *Pool) beginDownload(
	uid string,
	sconn *SyncConn,
	managed bool,
) (*TransferGeneration, bool) {
	if uid == "" {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.m[uid]; exists {
		return nil, false
	}
	p.m[uid] = nil
	if sconn != nil {
		p.m[uid] = append(p.m[uid], sconn)
	}
	delete(p.e, uid)
	return p.newGenerationLocked(uid, managed), true
}

// ManagedGeneration returns the currently active binary-API generation.
// Legacy server/RPC registrations deliberately do not satisfy this lookup.
func (p *Pool) ManagedGeneration(uid string) (*TransferGeneration, bool) {
	p.mu.RLock()
	generation := p.generations[uid]
	ok := generation != nil && generation.managed
	p.mu.RUnlock()
	return generation, ok
}

// CurrentGeneration returns the exact active generation for uid, including
// legacy RPC/web registrations. Callers retain this token through callbacks
// and finalization so a late result cannot affect a replacement generation.
func (p *Pool) CurrentGeneration(uid string) (*TransferGeneration, bool) {
	p.mu.RLock()
	generation := p.generations[uid]
	ok := generation != nil
	p.mu.RUnlock()
	return generation, ok
}

// AddDownload registers a new download in the pool with the given unique identifier.
// If sconn is nil, an empty connection slice is created for later connections to join.
// If sconn is provided, it becomes the first connection watching this download.
func (p *Pool) AddDownload(uid string, sconn *SyncConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.m[uid]; !ok {
		p.m[uid] = []*SyncConn{}
		p.newGenerationLocked(uid, false)
	}
	if generation := p.generations[uid]; generation != nil &&
		(generation.terminal || generation.terminalizing) {
		return
	}
	if sconn == nil || containsConn(p.m[uid], sconn) {
		return
	}
	p.m[uid] = append(p.m[uid], sconn)
}

// StopDownload removes a download from the pool by its unique identifier.
// This should be called when a download completes or is cancelled.
func (p *Pool) StopDownload(uid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, uid)
	if generation := p.generations[uid]; generation != nil {
		delete(p.generations, uid)
		p.closeGenerationLocked(generation)
	}
	for key, sub := range p.subs {
		if key.uid == uid {
			delete(p.subs, key)
			sub.closeOnce.Do(func() { close(sub.queue) })
		}
	}
}

// AddConnection adds a new connection to an existing download's connection list.
// The connection will receive broadcast messages for the specified download.
// It returns false if uid is no longer registered. Checking existence and
// attaching happen under the same lock so a terminal broadcast cannot be
// followed by an attach that accidentally recreates the terminated entry.
func (p *Pool) AddConnection(uid string, sconn *SyncConn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	conns, exists := p.m[uid]
	if !exists || sconn == nil {
		return false
	}
	if generation := p.generations[uid]; generation != nil &&
		(generation.terminal || generation.terminalizing) {
		return false
	}
	if !containsConn(conns, sconn) {
		p.m[uid] = append(conns, sconn)
	}
	return true
}

// writeBroadcastedMessage sends a message to a single connection.
// Fixed Race 6: Always unlock wmu using defer, return success/failure status.
func (p *Pool) writeBroadcastedMessage(sconn *SyncConn, head, data []byte) bool {
	if sconn == nil || sconn.Conn == nil {
		return false
	}
	// Set the deadline before waiting for wmu. A synchronous response writer
	// may already hold that mutex while blocked on the same non-reading client;
	// updating the connection deadline interrupts that in-flight write so
	// terminal delivery and Manager shutdown cannot wait forever for the lock.
	if err := sconn.Conn.SetWriteDeadline(time.Now().Add(poolWriteTimeout)); err != nil {
		return false
	}
	defer func() { _ = sconn.Conn.SetWriteDeadline(time.Time{}) }()

	sconn.wmu.Lock()
	defer sconn.wmu.Unlock()

	if err := writeAll(sconn.Conn, head); err != nil {
		return false
	}
	if err := writeAll(sconn.Conn, data); err != nil {
		return false
	}
	return true
}

func writeAll(dst io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := dst.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

// Broadcast sends the given data to all connections watching the specified download.
// Connections that fail to receive the message are automatically removed from the pool.
// Fixed Race 6: Copy slice before iteration, batch removals to prevent corruption.
func (p *Pool) Broadcast(uid string, data []byte) {
	p.mu.Lock()
	if generation := p.generations[uid]; generation != nil &&
		(generation.terminal || generation.terminalizing) {
		p.mu.Unlock()
		return
	}
	sconns := p.m[uid]
	if len(sconns) == 0 {
		p.mu.Unlock()
		return
	}
	for _, sconn := range sconns {
		key := poolSubscriptionKey{uid: uid, conn: sconn}
		sub := p.subs[key]
		if sub == nil {
			sub = newPoolSubscriber()
			p.subs[key] = sub
			go p.runSubscriber(key, sub)
		}
		msg := append([]byte(nil), data...)
		select {
		case sub.queue <- msg:
		default:
			// Progress delivery is best-effort. Keep the bounded subscriber
			// alive when it falls behind so a later terminal update can
			// replace the stale backlog. The writer's deadline still evicts
			// clients that stop reading altogether.
		}
	}
	p.mu.Unlock()
}

// BroadcastTerminal replaces any queued progress with one terminal update,
// removes the download from future broadcasts, and lets existing writers drain.
//
// All queue sends and closes happen while p.mu is held. This makes terminal
// delivery atomic with Broadcast and StopDownload and prevents send-on-closed
// races without making transfer callbacks wait for network I/O.
func (p *Pool) BroadcastTerminal(uid string, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if generation := p.generations[uid]; generation != nil {
		if generation.terminalizing {
			return
		}
		delete(p.generations, uid)
		p.closeGenerationLocked(generation)
	}
	sconns := p.m[uid]
	delete(p.m, uid)
	for _, sconn := range sconns {
		key := poolSubscriptionKey{uid: uid, conn: sconn}
		sub := p.subs[key]
		if sub == nil {
			sub = newPoolSubscriber()
			go p.runSubscriber(key, sub)
		} else {
			delete(p.subs, key)
		}

		// A writer may already own one progress frame. Everything still
		// queued is stale once the transfer terminates, so discard it.
	drain:
		for {
			select {
			case <-sub.queue:
			default:
				break drain
			}
		}

		sub.queue <- append([]byte(nil), data...)
		sub.closeOnce.Do(func() { close(sub.queue) })
	}

	// Defensive cleanup for subscribers left behind by an inconsistent
	// connection slice. They cannot receive the terminal frame, but they
	// must not remain eligible for future broadcasts.
	for key, sub := range p.subs {
		if key.uid != uid {
			continue
		}
		delete(p.subs, key)
		sub.closeOnce.Do(func() { close(sub.queue) })
	}
}

func (p *Pool) runSubscriber(key poolSubscriptionKey, sub *poolSubscriber) {
	defer close(sub.done)
	for data := range sub.queue {
		head := intToBytes(uint32(len(data)))
		if !p.writeBroadcastedMessage(key.conn, head, data) {
			p.mu.Lock()
			p.removeSubscriberLocked(key, sub, true)
			p.mu.Unlock()
			return
		}
	}
}

// UID returns the download identifier associated with this generation.
func (g *TransferGeneration) UID() string {
	if g == nil {
		return ""
	}
	return g.uid
}

// IsCurrent reports whether this exact generation is still registered.
func (g *TransferGeneration) IsCurrent() bool {
	if g == nil || g.pool == nil {
		return false
	}
	g.pool.mu.RLock()
	current := g.pool.generations[g.uid] == g
	g.pool.mu.RUnlock()
	return current
}

// IsRunnable reports whether this exact generation is still current and has
// not entered terminal recording or delivery. Queue and resume launchers use
// this to avoid starting replacement work while a prior terminal frame drains.
func (g *TransferGeneration) IsRunnable() bool {
	if g == nil || g.pool == nil {
		return false
	}
	g.pool.mu.RLock()
	runnable := g.pool.generations[g.uid] == g &&
		!g.terminal &&
		!g.terminalizing
	g.pool.mu.RUnlock()
	return runnable
}

// Broadcast publishes progress only while this exact generation remains live
// and has not recorded a terminal result.
func (g *TransferGeneration) Broadcast(data []byte) bool {
	if g == nil || g.pool == nil {
		return false
	}
	p := g.pool
	p.mu.Lock()
	if p.generations[g.uid] != g || g.terminal || g.terminalizing {
		p.mu.Unlock()
		return false
	}
	sconns := p.m[g.uid]
	for _, sconn := range sconns {
		key := poolSubscriptionKey{uid: g.uid, conn: sconn}
		sub := p.subs[key]
		if sub == nil {
			sub = newPoolSubscriber()
			p.subs[key] = sub
			go p.runSubscriber(key, sub)
		}
		msg := append([]byte(nil), data...)
		select {
		case sub.queue <- msg:
		default:
		}
	}
	p.mu.Unlock()
	return true
}

// RecordTerminal closes the progress gate and stores the first terminal frame
// for this generation without removing it from the pool. The outer goroutine
// that called Start or Resume must call Finish after all workers have returned.
func (g *TransferGeneration) RecordTerminal(data []byte) bool {
	if g == nil || g.pool == nil {
		return false
	}
	p := g.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.generations[g.uid] != g || g.terminalizing || g.terminal {
		return false
	}
	g.terminal = true
	g.terminalData = append([]byte(nil), data...)
	return true
}

// WriteError records an error only if this generation is still current.
// A stale worker therefore cannot overwrite the replacement run's state.
func (g *TransferGeneration) WriteError(errType ErrorType, errMessage string) bool {
	if g == nil || g.pool == nil {
		return false
	}
	p := g.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.generations[g.uid] != g {
		return false
	}
	if err, ok := p.e[g.uid]; ok && err.Type == ErrorTypeCritical && errType != ErrorTypeCritical {
		return true
	}
	p.e[g.uid] = &Error{errType, errMessage}
	return true
}

// Finish delivers the recorded terminal frame, waits for each connection
// writer to finish it (or hit its write deadline), and only then removes the
// UID from the active pool. Consequently HasDownload becoming false is a
// delivery barrier: a replacement generation cannot emit progress before the
// prior terminal frame has left the writer.
//
// fallback is recorded only when no callback supplied a terminal frame.
func (g *TransferGeneration) Finish(fallback []byte) bool {
	if g == nil || g.pool == nil {
		return false
	}
	p := g.pool
	p.mu.Lock()
	if p.generations[g.uid] != g {
		p.mu.Unlock()
		return false
	}
	if g.terminalizing {
		done := g.finishDone
		p.mu.Unlock()
		<-done
		return true
	}
	if !g.terminal {
		g.terminal = true
		g.terminalData = append([]byte(nil), fallback...)
	}
	g.terminalizing = true
	data := append([]byte(nil), g.terminalData...)
	sconns := append([]*SyncConn(nil), p.m[g.uid]...)
	// Keep the map key present so HasDownload remains true, but detach every
	// watcher from ordinary progress and from stale-writer cleanup.
	p.m[g.uid] = nil

	done := make([]<-chan struct{}, 0, len(sconns))
	for _, sconn := range sconns {
		key := poolSubscriptionKey{uid: g.uid, conn: sconn}
		sub := p.subs[key]
		if sub == nil {
			sub = newPoolSubscriber()
			go p.runSubscriber(key, sub)
		} else {
			delete(p.subs, key)
		}
	drain:
		for {
			select {
			case <-sub.queue:
			default:
				break drain
			}
		}
		if len(data) > 0 {
			sub.queue <- append([]byte(nil), data...)
		}
		sub.closeOnce.Do(func() { close(sub.queue) })
		done = append(done, sub.done)
	}
	for key, sub := range p.subs {
		if key.uid != g.uid {
			continue
		}
		delete(p.subs, key)
		sub.closeOnce.Do(func() { close(sub.queue) })
		done = append(done, sub.done)
	}
	p.mu.Unlock()

	for _, writerDone := range done {
		<-writerDone
	}

	p.mu.Lock()
	if p.generations[g.uid] == g {
		delete(p.generations, g.uid)
		delete(p.m, g.uid)
	}
	p.closeGenerationLocked(g)
	p.mu.Unlock()
	return true
}

// Abort removes a generation that failed before its worker goroutine started.
// It is generation-bound, so cleanup from one failed resume cannot remove a
// concurrently established replacement.
func (g *TransferGeneration) Abort() bool {
	if g == nil || g.pool == nil {
		return false
	}
	p := g.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.generations[g.uid] != g {
		return false
	}
	delete(p.generations, g.uid)
	delete(p.m, g.uid)
	for key, sub := range p.subs {
		if key.uid == g.uid {
			delete(p.subs, key)
			sub.closeOnce.Do(func() { close(sub.queue) })
		}
	}
	p.closeGenerationLocked(g)
	return true
}

func (p *Pool) removeSubscriberLocked(key poolSubscriptionKey, sub *poolSubscriber, closeConn bool) {
	if sub == nil {
		return
	}
	if p.subs[key] != sub {
		// Terminal delivery and StopDownload detach a subscriber before its
		// writer has necessarily finished. If that old writer then fails,
		// close the dead transport unless the same connection has already
		// been registered for a new run of this UID.
		if closeConn && !containsConn(p.m[key.uid], key.conn) &&
			key.conn != nil && key.conn.Conn != nil {
			_ = key.conn.Conn.Close()
		}
		return
	}
	delete(p.subs, key)
	conns := p.m[key.uid]
	kept := conns[:0]
	for _, conn := range conns {
		if conn != key.conn {
			kept = append(kept, conn)
		}
	}
	p.m[key.uid] = kept
	sub.closeOnce.Do(func() { close(sub.queue) })
	if closeConn && key.conn != nil && key.conn.Conn != nil {
		_ = key.conn.Conn.Close()
	}
}

func containsConn(conns []*SyncConn, target *SyncConn) bool {
	for _, conn := range conns {
		if conn == target {
			return true
		}
	}
	return false
}

// WriteError stores an error for the specified download.
// If a critical error already exists and the new error is not critical,
// the existing critical error is preserved.
// Fixed Race bonus: Single write lock to prevent TOCTOU.
func (p *Pool) WriteError(uid string, errType ErrorType, errMessage string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err, ok := p.e[uid]; ok && err.Type == ErrorTypeCritical && errType != ErrorTypeCritical {
		return
	}
	p.e[uid] = &Error{errType, errMessage}
}

// ForceWriteError stores an error for the specified download, overwriting any existing error.
// Unlike WriteError, this always overwrites regardless of error severity.
func (p *Pool) ForceWriteError(uid string, errType ErrorType, errMessage string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.e[uid] = &Error{errType, errMessage}
}

// GetError retrieves the stored error for the specified download.
// Returns nil if no error has been recorded for the download.
func (p *Pool) GetError(uid string) *Error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.e[uid]
}

// removeConn removes a single connection by index.
// Note: This method is kept for backward compatibility but should be used with caution
// as it can lead to slice corruption if the slice is modified during iteration.
func (p *Pool) removeConn(uid string, connIndex int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	conns := p.m[uid]
	if connIndex < 0 || connIndex >= len(conns) {
		return
	}
	conn := conns[connIndex]
	key := poolSubscriptionKey{uid: uid, conn: conn}
	if sub := p.subs[key]; sub != nil {
		p.removeSubscriberLocked(key, sub, true)
		return
	}
	if conn != nil && conn.Conn != nil {
		_ = conn.Conn.Close()
	}
	p.m[uid] = append(conns[:connIndex], conns[connIndex+1:]...)
}
