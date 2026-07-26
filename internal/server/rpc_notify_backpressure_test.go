package server

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRPCNotifierSlowClientDoesNotBlockBroadcast(t *testing.T) {
	notifier := NewRPCNotifier(nil)
	client, server, cleanup := newTestServer(t)
	defer cleanup()
	notifier.Register(server)

	start := time.Now()
	for i := 0; i < rpcNotificationQueueSize+2; i++ {
		notifier.Broadcast("download.progress", &DownloadProgressNotification{
			GID:             "slow",
			CompletedLength: int64(i),
		})
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("Broadcast blocked for %v", elapsed)
	}
	waitForCondition(t, time.Second, func() bool { return notifier.Count() == 0 })

	// Release the notifier worker if it reached jrpc2's synchronous test
	// channel before the eviction goroutine stopped the server.
	_, _ = client.Recv()
}

func TestRPCNotifierSnapshotsParamsBeforeQueueing(t *testing.T) {
	notifier := NewRPCNotifier(nil)
	client, server, cleanup := newTestServer(t)
	defer cleanup()
	notifier.Register(server)

	params := map[string]string{"value": "before"}
	notifier.Broadcast("snapshot", params)
	params["value"] = "after"

	data, err := client.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	var notification struct {
		Params map[string]string `json:"params"`
	}
	if err := json.Unmarshal(data, &notification); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := notification.Params["value"]; got != "before" {
		t.Fatalf("queued value = %q, want before", got)
	}
}
