package server

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/creachadair/jrpc2"
)

const (
	rpcNotificationQueueSize = 64
	rpcNotificationTimeout   = 2 * time.Second
)

type rpcNotification struct {
	method    string
	params    json.RawMessage
	hasParams bool
}

type rpcSubscriber struct {
	queue    chan rpcNotification
	done     chan struct{}
	stopOnce sync.Once
}

// RPCNotifier maintains a set of connected jrpc2 WebSocket servers
// and broadcasts push notifications to all of them.
type RPCNotifier struct {
	mu      sync.RWMutex
	servers map[*jrpc2.Server]*rpcSubscriber
	log     *log.Logger
}

// NewRPCNotifier creates a new notifier.
func NewRPCNotifier(l *log.Logger) *RPCNotifier {
	return &RPCNotifier{
		servers: make(map[*jrpc2.Server]*rpcSubscriber),
		log:     l,
	}
}

// Register adds a server to the broadcast set.
func (n *RPCNotifier) Register(srv *jrpc2.Server) {
	n.mu.Lock()
	if _, ok := n.servers[srv]; ok {
		n.mu.Unlock()
		return
	}
	sub := &rpcSubscriber{
		queue: make(chan rpcNotification, rpcNotificationQueueSize),
		done:  make(chan struct{}),
	}
	n.servers[srv] = sub
	n.mu.Unlock()
	go n.runSubscriber(srv, sub)
}

// Unregister removes a server from the broadcast set.
func (n *RPCNotifier) Unregister(srv *jrpc2.Server) {
	n.mu.Lock()
	sub := n.servers[srv]
	if sub != nil {
		delete(n.servers, srv)
		sub.stopOnce.Do(func() { close(sub.done) })
	}
	n.mu.Unlock()
}

// Broadcast sends a push notification to all registered servers.
// Servers that fail to receive (e.g., disconnected) are unregistered.
func (n *RPCNotifier) Broadcast(method string, params any) {
	var encoded json.RawMessage
	if params != nil {
		var err error
		encoded, err = json.Marshal(params)
		if err != nil {
			if n.log != nil {
				n.log.Printf("RPC push encoding failed: %v", err)
			}
			return
		}
	}
	notification := rpcNotification{
		method:    method,
		params:    encoded,
		hasParams: params != nil,
	}

	n.mu.Lock()
	for srv, sub := range n.servers {
		select {
		case sub.queue <- notification:
		default:
			delete(n.servers, srv)
			sub.stopOnce.Do(func() { close(sub.done) })
			go srv.Stop()
			if n.log != nil {
				n.log.Printf("RPC push queue full; evicting slow client")
			}
		}
	}
	n.mu.Unlock()
}

func (n *RPCNotifier) runSubscriber(srv *jrpc2.Server, sub *rpcSubscriber) {
	for {
		select {
		case <-sub.done:
			return
		default:
		}
		select {
		case <-sub.done:
			return
		case notification := <-sub.queue:
			ctx, cancel := context.WithTimeout(context.Background(), rpcNotificationTimeout)
			var params any
			if notification.hasParams {
				params = notification.params
			}
			err := srv.Notify(ctx, notification.method, params)
			cancel()
			if err == nil {
				continue
			}
			if n.log != nil {
				n.log.Printf("RPC push failed: %v", err)
			}
			srv.Stop()
			n.mu.Lock()
			if n.servers[srv] == sub {
				delete(n.servers, srv)
				sub.stopOnce.Do(func() { close(sub.done) })
			}
			n.mu.Unlock()
			return
		}
	}
}

// Count returns the number of registered servers (for testing).
func (n *RPCNotifier) Count() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.servers)
}

// Notification param types for download events.

// DownloadStartedNotification is sent when a download begins.
type DownloadStartedNotification struct {
	GID         string `json:"gid"`
	FileName    string `json:"fileName"`
	TotalLength int64  `json:"totalLength"`
}

// DownloadProgressNotification is sent during download progress.
type DownloadProgressNotification struct {
	GID             string `json:"gid"`
	CompletedLength int64  `json:"completedLength"`
}

// DownloadCompleteNotification is sent when a download completes.
type DownloadCompleteNotification struct {
	GID         string `json:"gid"`
	TotalLength int64  `json:"totalLength"`
}

// DownloadErrorNotification is sent when a download encounters an error.
type DownloadErrorNotification struct {
	GID   string `json:"gid"`
	Error string `json:"error"`
}
