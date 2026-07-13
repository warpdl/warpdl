package server

import (
	"context"
	"log"
	"sync"

	jsonrpc "github.com/gumeniukcom/golang-jsonrpc2/v2"
)

// RPCNotifier maintains the set of per-connection pushers of the connected
// JSON-RPC WebSocket clients and broadcasts push notifications to all of
// them. Pushers are registered when a connection is accepted and
// unregistered when it closes; a pusher whose connection died between those
// two points is pruned on the first failed broadcast.
type RPCNotifier struct {
	mu      sync.RWMutex
	pushers map[jsonrpc.Pusher]struct{}
	log     *log.Logger
}

// NewRPCNotifier creates a new notifier.
func NewRPCNotifier(l *log.Logger) *RPCNotifier {
	return &RPCNotifier{
		pushers: make(map[jsonrpc.Pusher]struct{}),
		log:     l,
	}
}

// Register adds a connection's pusher to the broadcast set.
func (n *RPCNotifier) Register(p jsonrpc.Pusher) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.pushers[p] = struct{}{}
}

// Unregister removes a connection's pusher from the broadcast set.
func (n *RPCNotifier) Unregister(p jsonrpc.Pusher) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.pushers, p)
}

// Broadcast sends a push notification to all registered pushers.
// Pushers that fail to send (e.g., disconnected) are unregistered.
func (n *RPCNotifier) Broadcast(method string, params any) {
	n.mu.RLock()
	pushers := make([]jsonrpc.Pusher, 0, len(n.pushers))
	for p := range n.pushers {
		pushers = append(pushers, p)
	}
	n.mu.RUnlock()

	var failed []jsonrpc.Pusher
	for _, p := range pushers {
		if err := p.Notify(context.Background(), method, params); err != nil {
			if n.log != nil {
				n.log.Printf("RPC push failed: %v", err)
			}
			failed = append(failed, p)
		}
	}

	if len(failed) > 0 {
		n.mu.Lock()
		for _, p := range failed {
			delete(n.pushers, p)
		}
		n.mu.Unlock()
	}
}

// Count returns the number of registered pushers (for testing).
func (n *RPCNotifier) Count() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.pushers)
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
