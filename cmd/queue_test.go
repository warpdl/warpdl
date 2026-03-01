package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
)

func TestQueueStatusCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "queue")
	if err := queueStatusAction(ctx); err != nil {
		t.Fatalf("queueStatusAction: %v", err)
	}
}

func TestQueueStatusActiveWaiting(t *testing.T) {
	dir, err := os.MkdirTemp("", "wq")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "w.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	queueStatusOverride = &common.QueueStatusResponse{
		MaxConcurrent: 4,
		ActiveCount:   1,
		WaitingCount:  1,
		Paused:        false,
		Active:        []string{"abc123"},
		Waiting: []common.QueueItemInfo{
			{Hash: "def456", Position: 0, Priority: 1},
		},
	}
	defer func() { queueStatusOverride = nil }()

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "queue")
	if err := queueStatusAction(ctx); err != nil {
		t.Fatalf("queueStatusAction with active/waiting: %v", err)
	}
}

func TestQueueStatusPaused(t *testing.T) {
	dir, err := os.MkdirTemp("", "wq")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "w.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	queueStatusOverride = &common.QueueStatusResponse{
		MaxConcurrent: 2,
		Paused:        true,
	}
	defer func() { queueStatusOverride = nil }()

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "queue")
	if err := queueStatusAction(ctx); err != nil {
		t.Fatalf("queueStatusAction paused: %v", err)
	}
}

func TestQueueStatusHelpArg(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"help"}, "queue")
	_ = queueStatusAction(ctx)
}

func TestQueueStatusClientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}
	// No server running - client connection will fail
	t.Setenv("WARPDL_SOCKET_PATH", "/nonexistent/socket.sock")
	app := cli.NewApp()
	ctx := newContext(app, nil, "queue")
	// Should return nil (prints error, doesn't return it)
	if err := queueStatusAction(ctx); err != nil {
		t.Fatalf("queueStatusAction: %v", err)
	}
}

func TestQueueStatusServerError(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_QUEUE_STATUS: "queue status failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "queue")
	if err := queueStatusAction(ctx); err != nil {
		t.Fatalf("queueStatusAction: %v", err)
	}
}

func TestQueuePauseCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "pause")
	if err := queuePauseAction(ctx); err != nil {
		t.Fatalf("queuePauseAction: %v", err)
	}
}

func TestQueuePauseHelpArg(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"help"}, "pause")
	_ = queuePauseAction(ctx)
}

func TestQueuePauseClientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}
	t.Setenv("WARPDL_SOCKET_PATH", "/nonexistent/socket.sock")
	app := cli.NewApp()
	ctx := newContext(app, nil, "pause")
	if err := queuePauseAction(ctx); err != nil {
		t.Fatalf("queuePauseAction: %v", err)
	}
}

func TestQueuePauseServerError(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_QUEUE_PAUSE: "pause failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "pause")
	if err := queuePauseAction(ctx); err != nil {
		t.Fatalf("queuePauseAction: %v", err)
	}
}

func TestQueueResumeCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "resume")
	if err := queueResumeAction(ctx); err != nil {
		t.Fatalf("queueResumeAction: %v", err)
	}
}

func TestQueueResumeHelpArg(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"help"}, "resume")
	_ = queueResumeAction(ctx)
}

func TestQueueResumeClientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}
	t.Setenv("WARPDL_SOCKET_PATH", "/nonexistent/socket.sock")
	app := cli.NewApp()
	ctx := newContext(app, nil, "resume")
	if err := queueResumeAction(ctx); err != nil {
		t.Fatalf("queueResumeAction: %v", err)
	}
}

func TestQueueResumeServerError(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_QUEUE_RESUME: "resume failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "resume")
	if err := queueResumeAction(ctx); err != nil {
		t.Fatalf("queueResumeAction: %v", err)
	}
}

func TestQueueMoveCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"hash123", "1"}, "move")
	if err := queueMoveAction(ctx); err != nil {
		t.Fatalf("queueMoveAction: %v", err)
	}
}

func TestQueueMoveHelpArg(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"help"}, "move")
	_ = queueMoveAction(ctx)
}

func TestQueueMoveNoArgs(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "move")
	// Should return error about missing args
	_ = queueMoveAction(ctx)
}

func TestQueueMoveOneArg(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"hash123"}, "move")
	// Should return error about missing position
	_ = queueMoveAction(ctx)
}

func TestQueueMoveInvalidPosition(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"hash123", "notanumber"}, "move")
	// Should return error about invalid position
	_ = queueMoveAction(ctx)
}

func TestQueueMoveClientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}
	t.Setenv("WARPDL_SOCKET_PATH", "/nonexistent/socket.sock")
	app := cli.NewApp()
	ctx := newContext(app, []string{"hash123", "1"}, "move")
	if err := queueMoveAction(ctx); err != nil {
		t.Fatalf("queueMoveAction: %v", err)
	}
}

func TestQueueMoveServerError(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_QUEUE_MOVE: "move failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"hash123", "1"}, "move")
	if err := queueMoveAction(ctx); err != nil {
		t.Fatalf("queueMoveAction: %v", err)
	}
}

func TestPriorityName(t *testing.T) {
	tests := []struct {
		priority int
		expected string
	}{
		{0, "low"},
		{1, "normal"},
		{2, "high"},
		{-1, "unknown"},
		{3, "unknown"},
		{100, "unknown"},
	}

	for _, tc := range tests {
		result := priorityName(tc.priority)
		if result != tc.expected {
			t.Errorf("priorityName(%d) = %q, want %q", tc.priority, result, tc.expected)
		}
	}
}

func TestQueueCmdStructure(t *testing.T) {
	if queueCmd.Name != "queue" {
		t.Errorf("expected queue command name, got %s", queueCmd.Name)
	}
	if len(queueCmd.Subcommands) != 4 {
		t.Errorf("expected 4 subcommands, got %d", len(queueCmd.Subcommands))
	}

	// Verify subcommand names
	subNames := make(map[string]bool)
	for _, sub := range queueCmd.Subcommands {
		subNames[sub.Name] = true
	}
	expected := []string{"status", "pause", "resume", "move"}
	for _, name := range expected {
		if !subNames[name] {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}
