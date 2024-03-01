package server

import (
	"fmt"
	"log"
	"sync"
)

type Pool struct {
	mu *sync.RWMutex
	m  map[string][]*SyncConn
	e  map[string]*Error
}

func NewPool(l *log.Logger) *Pool {
	return &Pool{
		mu: &sync.RWMutex{},
		m:  make(map[string][]*SyncConn),
		e:  make(map[string]*Error),
	}
}

func (p *Pool) HasDownload(uid string) bool {
	p.mu.RLock()
	_, ok := p.m[uid]
	p.mu.RUnlock()
	return ok
}

func (p *Pool) AddDownload(uid string, sconn *SyncConn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[uid] = []*SyncConn{sconn}
}

func (p *Pool) StopDownload(uid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, uid)
}

func (p *Pool) AddConnections(uid string, conns []*SyncConn) {
	p.mu.RLock()
	_conns := p.m[uid]
	p.mu.RUnlock()
	if _conns == nil {
		_conns = []*SyncConn{}
	}
	_conns = append(_conns, conns...)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[uid] = _conns
}

func (p *Pool) Broadcast(uid string, data []byte) error {
	head := intToBytes(uint32(len(data)))
	p.mu.RLock()
	defer p.mu.RUnlock()
	for i, sconn := range p.m[uid] {
		sconn.wmu.Lock()
		_, err := sconn.Conn.Write(head)
		if err != nil {
			p.removeConn(uid, i)
			return fmt.Errorf("error writing: %s", err.Error())
		}
		_, err = sconn.Conn.Write(data)
		if err != nil {
			p.removeConn(uid, i)
			return fmt.Errorf("error writing: %s", err.Error())
		}
		sconn.wmu.Unlock()
	}
	return nil
}

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

func (p *Pool) ForceWriteError(uid string, errType ErrorType, errMessage string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.e[uid] = &Error{errType, errMessage}
}

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
