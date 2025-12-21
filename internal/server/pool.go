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
func (p *Pool) AddConnection(uid string, sconn *SyncConn) {
	p.mu.RLock()
	_conns := p.m[uid]
	p.mu.RUnlock()
	_conns = append(_conns, sconn)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[uid] = _conns
}

func (p *Pool) writeBroadcastedMessage(uid string, i int, sconn *SyncConn, head []byte, data []byte) {
	sconn.wmu.Lock()
	_, err := sconn.Conn.Write(head)
	if err != nil {
		p.removeConn(uid, i)
		return
	}
	_, err = sconn.Conn.Write(data)
	if err != nil {
		p.removeConn(uid, i)
		return
	}
	sconn.wmu.Unlock()
}

// Broadcast sends the given data to all connections watching the specified download.
// Connections that fail to receive the message are automatically removed from the pool.
func (p *Pool) Broadcast(uid string, data []byte) {
	head := intToBytes(uint32(len(data)))
	p.mu.RLock()
	sconns := p.m[uid]
	p.mu.RUnlock()
	for i, sconn := range sconns {
		p.writeBroadcastedMessage(uid, i, sconn, head, data)
	}
}

// WriteError stores an error for the specified download.
// If a critical error already exists and the new error is not critical,
// the existing critical error is preserved.
func (p *Pool) WriteError(uid string, errType ErrorType, errMessage string) {
	p.mu.RLock()
	err, ok := p.e[uid]
	if ok && err.Type == ErrorTypeCritical && errType != ErrorTypeCritical {
		p.mu.RUnlock()
		return
	}
	p.mu.RUnlock()
	p.mu.Lock()
	defer p.mu.Unlock()
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
