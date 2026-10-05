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

// runProductionCLI dispatches args through the production CLI tree (GetApp) and
// returns the captured stdout/stderr plus the run error. The app is built inside
// the capture window because cli.App adopts os.Stdout as its writer at Setup.
// Shared by the queue and background CLI tests in this package.
func runProductionCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	stdout, stderr = captureOutput(func() {
		app := GetApp(BuildArgs{})
		err = app.Run(append([]string{"warpdl"}, args...))
	})
	return stdout, stderr, err
}

// TestQueueHelpArgShowsSubcommandHelp pins the queue subcommand "help" argument
// contract: each action must resolve its own help topic and exit successfully.
// Dispatch runs through the real CLI tree, where urfave/cli resolves the topic
// against the queue sub-app's command set. Hand-building a bare app context
// instead turns every case into cli's "No help topic" exit 3, which asserts
// nothing about the action.
func TestQueueHelpArgShowsSubcommandHelp(t *testing.T) {
	tests := []struct {
		subcommand string
		usage      string
		argsUsage  string
	}{
		{subcommand: "status", usage: "show queue status"},
		{subcommand: "pause", usage: "pause the queue (no new downloads start)"},
		{subcommand: "resume", usage: "resume the queue"},
		{subcommand: "move", usage: "move a queued download to a new position", argsUsage: "<hash> <position>"},
	}

	for _, tt := range tests {
		t.Run(tt.subcommand, func(t *testing.T) {
			stdout, _, err := runProductionCLI(t, "queue", tt.subcommand, "help")
			if err != nil {
				t.Fatalf("queue %s help: %v", tt.subcommand, err)
			}
			assertContains(t, stdout, tt.usage)
			if tt.argsUsage != "" {
				assertContains(t, stdout, tt.argsUsage)
			}
		})
	}
}

func TestQueueStatusClientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}
	// No server running - client connection will fail
	t.Setenv("WARPDL_SOCKET_PATH", "/nonexistent/socket.sock")
	app := cli.NewApp()
	ctx := newContext(app, nil, "queue")
	assertExitError(t, queueStatusAction(ctx))
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
	assertExitError(t, queueStatusAction(ctx))
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

func TestQueuePauseClientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}
	t.Setenv("WARPDL_SOCKET_PATH", "/nonexistent/socket.sock")
	app := cli.NewApp()
	ctx := newContext(app, nil, "pause")
	assertExitError(t, queuePauseAction(ctx))
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
	assertExitError(t, queuePauseAction(ctx))
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

func TestQueueResumeClientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}
	t.Setenv("WARPDL_SOCKET_PATH", "/nonexistent/socket.sock")
	app := cli.NewApp()
	ctx := newContext(app, nil, "resume")
	assertExitError(t, queueResumeAction(ctx))
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
	assertExitError(t, queueResumeAction(ctx))
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

// TestQueueMoveArgErrorsReportUsage pins move's argument validation: every
// malformed invocation prints its specific diagnostic plus the move help text,
// exits non-zero, and never reaches the daemon. Each row is a distinct guard in
// queueMoveAction (arg count, non-numeric position, position below one).
func TestQueueMoveArgErrorsReportUsage(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "no_args",
			wantMsg: "usage: warpdl queue move <hash> <position>",
		},
		{
			name:    "missing_position",
			args:    []string{"hash123"},
			wantMsg: "usage: warpdl queue move <hash> <position>",
		},
		{
			name:    "non_numeric_position",
			args:    []string{"hash123", "notanumber"},
			wantMsg: "invalid position 'notanumber': must be a number",
		},
		{
			name:    "position_below_one",
			args:    []string{"hash123", "0"},
			wantMsg: "invalid position '0': positions start at 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// An absent socket keeps the "never reached the daemon" check
			// deterministic if one of the guards regresses.
			t.Setenv("WARPDL_SOCKET_PATH", filepath.Join(t.TempDir(), "absent.sock"))

			stdout, stderr, err := runProductionCLI(t, append([]string{"queue", "move"}, tt.args...)...)

			assertExitError(t, err)
			assertContains(t, stdout, tt.wantMsg)
			assertContains(t, stdout, "move a queued download to a new position")
			assertNotContains(t, stderr, "queue move[")
		})
	}
}

func TestQueueMoveClientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow test in short mode")
	}
	t.Setenv("WARPDL_SOCKET_PATH", "/nonexistent/socket.sock")
	app := cli.NewApp()
	ctx := newContext(app, []string{"hash123", "1"}, "move")
	assertExitError(t, queueMoveAction(ctx))
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
	assertExitError(t, queueMoveAction(ctx))
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
