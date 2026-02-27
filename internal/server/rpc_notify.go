package server

import (
	"context"
	"log"
	"sync"

	"github.com/creachadair/jrpc2"
)

// RPCNotifier maintains a set of connected jrpc2 WebSocket servers
// and broadcasts push notifications to all of them.
type RPCNotifier struct {
	mu      sync.RWMutex
	servers map[*jrpc2.Server]struct{}
	log     *log.Logger
}

// NewRPCNotifier creates a new notifier.
func NewRPCNotifier(l *log.Logger) *RPCNotifier {
	return &RPCNotifier{
		servers: make(map[*jrpc2.Server]struct{}),
		log:     l,
	}
}

// Register adds a server to the broadcast set.
func (n *RPCNotifier) Register(srv *jrpc2.Server) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.servers[srv] = struct{}{}
}

// Unregister removes a server from the broadcast set.
func (n *RPCNotifier) Unregister(srv *jrpc2.Server) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.servers, srv)
}

// Broadcast sends a push notification to all registered servers.
// Servers that fail to receive (e.g., disconnected) are unregistered.
func (n *RPCNotifier) Broadcast(method string, params any) {
	n.mu.RLock()
	servers := make([]*jrpc2.Server, 0, len(n.servers))
	for srv := range n.servers {
		servers = append(servers, srv)
	}
	n.mu.RUnlock()

	var failed []*jrpc2.Server
	for _, srv := range servers {
		if err := srv.Notify(context.Background(), method, params); err != nil {
			if n.log != nil {
				n.log.Printf("RPC push failed: %v", err)
			}
			failed = append(failed, srv)
		}
	}

	if len(failed) > 0 {
		n.mu.Lock()
		for _, srv := range failed {
			delete(n.servers, srv)
		}
		n.mu.Unlock()
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
