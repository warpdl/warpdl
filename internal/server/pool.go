package server

import (
	"log"
	"sync"
)

// Pool manages active download connections and their associated errors.
// It provides thread-safe operations for tracking which downloads are active,
// broadcasting messages to all connections watching a download, and storing
// errors that occur during downloads.
type Pool struct {
	l  *log.Logger
	mu *sync.RWMutex
	m  map[string][]*SyncConn
	e  map[string]*Error
}

// NewPool creates a new Pool instance with the given logger.
// The pool is initialized with empty connection and error maps.
func NewPool(l *log.Logger) *Pool {
	return &Pool{
		l:  l,
		mu: &sync.RWMutex{},
		m:  make(map[string][]*SyncConn),
		e:  make(map[string]*Error),
	}
}

// HasDownload reports whether a download with the given unique identifier exists in the pool.
func (p *Pool) HasDownload(uid string) bool {
	p.mu.RLock()
	_, ok := p.m[uid]
	p.mu.RUnlock()
	return ok
}

// AddDownload registers a new download in the pool with the given unique identifier.
// If sconn is nil, an empty connection slice is created for later connections to join.
// If sconn is provided, it becomes the first connection watching this download.
func (p *Pool) AddDownload(uid string, sconn *SyncConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sconn == nil {
		p.m[uid] = []*SyncConn{}
		return
	}
	p.m[uid] = []*SyncConn{sconn}
}

// StopDownload removes a download from the pool by its unique identifier.
// This should be called when a download completes or is cancelled.
func (p *Pool) StopDownload(uid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, uid)
}

// AddConnection adds a new connection to an existing download's connection list.
// The connection will receive broadcast messages for the specified download.
// Fixed Race 5: Single write lock instead of RLock-unlock-Lock to prevent TOCTOU.
func (p *Pool) AddConnection(uid string, sconn *SyncConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[uid] = append(p.m[uid], sconn)
}

// writeBroadcastedMessage sends a message to a single connection.
// Fixed Race 6: Always unlock wmu using defer, return success/failure status.
func (p *Pool) writeBroadcastedMessage(sconn *SyncConn, head, data []byte) bool {
	sconn.wmu.Lock()
	defer sconn.wmu.Unlock()

	if _, err := sconn.Conn.Write(head); err != nil {
		return false
	}
	if _, err := sconn.Conn.Write(data); err != nil {
		return false
	}
	return true
}

// Broadcast sends the given data to all connections watching the specified download.
// Connections that fail to receive the message are automatically removed from the pool.
// Fixed Race 6: Copy slice before iteration, batch removals to prevent corruption.
func (p *Pool) Broadcast(uid string, data []byte) {
	head := intToBytes(uint32(len(data)))

	p.mu.RLock()
	sconns := p.m[uid]
	if len(sconns) == 0 {
		p.mu.RUnlock()
		return
	}
	snapshot := make([]*SyncConn, len(sconns))
	copy(snapshot, sconns)
	p.mu.RUnlock()

	var failed []*SyncConn
	for _, sconn := range snapshot {
		if !p.writeBroadcastedMessage(sconn, head, data) {
			failed = append(failed, sconn)
		}
	}

	if len(failed) > 0 {
		p.removeConns(uid, failed)
	}
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
	_ = conns[connIndex].Conn.Close()
	// shift last connection to the current connIndex
	conns[connIndex] = conns[len(conns)-1]
	// slice the last connection
	p.m[uid] = conns[:len(conns)-1]
}

// removeConns batch removes multiple connections from a download.
// This is more efficient and safer than removing connections one by one.
func (p *Pool) removeConns(uid string, toRemove []*SyncConn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	removeSet := make(map[*SyncConn]struct{}, len(toRemove))
	for _, sc := range toRemove {
		removeSet[sc] = struct{}{}
	}

	conns := p.m[uid]
	kept := conns[:0]
	for _, sc := range conns {
		if _, remove := removeSet[sc]; remove {
			_ = sc.Conn.Close()
		} else {
			kept = append(kept, sc)
		}
	}
	p.m[uid] = kept
}
