package server

import (
	"log"
	"net"
	"sync"
)

type Pool struct {
	log *log.Logger
	mu  *sync.RWMutex
	m   map[string][]net.Conn
	e   map[string]*Error
}

func NewPool(l *log.Logger) *Pool {
	return &Pool{
		log: l,
		mu:  &sync.RWMutex{},
		m:   make(map[string][]net.Conn),
		e:   make(map[string]*Error),
	}
}

func (p *Pool) AddDownload(uid string, conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[uid] = []net.Conn{conn}
}

func (p *Pool) AddConnection(uid string, conns []net.Conn) {
	p.mu.RLock()
	_conns := p.m[uid]
	p.mu.RUnlock()
	if _conns == nil {
		_conns = []net.Conn{}
	}
	_conns = append(_conns, conns...)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[uid] = _conns
}

func (p *Pool) Broadcast(uid string, data []byte) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for i, conn := range p.m[uid] {
		_, err := conn.Write(data)
		if err == nil {
			continue
		}
		p.log.Printf("[%s]Error writing: %s\n", uid, err.Error())
		p.removeConn(uid, i)
	}
}

func (p *Pool) WriteError(uid string, errType ErrorType, errMessage string) {
	p.mu.RLock()
	err, ok := p.e[uid]
	if ok && err.Type == ErrorTypeCritical && errType != ErrorTypeCritical {
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
	for i := range p.m[uid] {
		if i == connIndex {
			p.m[uid] = append(p.m[uid][:i], p.m[uid][i+1:]...)
			return
		}
	}
}
