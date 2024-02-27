package server

import (
	"fmt"
	"log"
	"net"
	"sync"
)

type Pool struct {
	mu *sync.RWMutex
	m  map[string][]net.Conn
	e  map[string]*Error
}

func NewPool(l *log.Logger) *Pool {
	return &Pool{
		mu: &sync.RWMutex{},
		m:  make(map[string][]net.Conn),
		e:  make(map[string]*Error),
	}
}

func (p *Pool) AddDownload(uid string, conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[uid] = []net.Conn{conn}
}

func (p *Pool) AddConnections(uid string, conns []net.Conn) {
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

func (p *Pool) Broadcast(uid string, data []byte) error {
	head := intToBytes(uint32(len(data)))
	p.mu.RLock()
	defer p.mu.RUnlock()
	for i, conn := range p.m[uid] {
		_, err := conn.Write(head)
		if err != nil {
			p.removeConn(uid, i)
			return fmt.Errorf("error writing: %s", err.Error())
		}
		_, err = conn.Write(data)
		if err != nil {
			p.removeConn(uid, i)
			return fmt.Errorf("error writing: %s", err.Error())
		}
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
	_ = conns[connIndex].Close()
	// shift last connection to the current connIndex
	conns[connIndex] = conns[len(conns)-1]
	// slice the last connection
	p.m[uid] = conns[:len(conns)-1]
}
