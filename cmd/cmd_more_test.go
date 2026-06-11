package cmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// withInvalidDaemonURI points getClient() at a malformed daemon URI so it
// fails before any RPC, exercising the "new_client" error branch of a
// command. Returns a cleanup func to defer.
func withInvalidDaemonURI(t *testing.T) func() {
	t.Helper()
	old := daemonURI
	daemonURI = "://invalid-uri"
	return func() { daemonURI = old }
}

// TestList_NewClientError covers list()'s new_client error branch: an
// invalid daemon URI makes getClient() fail, list() reports it and
// returns nil.
func TestList_NewClientError(t *testing.T) {
	defer withInvalidDaemonURI(t)()

	app := cli.NewApp()
	ctx := newContext(app, nil, "list")
	stdout, stderr := captureOutput(func() {
		if err := list(ctx); err != nil {
			t.Errorf("list: unexpected error: %v", err)
		}
	})
	if !strings.Contains(stdout+stderr, "new_client") {
		t.Fatalf("expected new_client error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestQueuePause_NewClientError covers queuePauseAction's new_client
// error branch.
func TestQueuePause_NewClientError(t *testing.T) {
	defer withInvalidDaemonURI(t)()

	app := cli.NewApp()
	ctx := newContext(app, nil, "pause")
	stdout, stderr := captureOutput(func() {
		if err := queuePauseAction(ctx); err != nil {
			t.Errorf("queuePauseAction: unexpected error: %v", err)
		}
	})
	if !strings.Contains(stdout+stderr, "new_client") {
		t.Fatalf("expected new_client error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestQueueResume_NewClientError covers queueResumeAction's new_client
// error branch.
func TestQueueResume_NewClientError(t *testing.T) {
	defer withInvalidDaemonURI(t)()

	app := cli.NewApp()
	ctx := newContext(app, nil, "resume")
	stdout, stderr := captureOutput(func() {
		if err := queueResumeAction(ctx); err != nil {
			t.Errorf("queueResumeAction: unexpected error: %v", err)
		}
	})
	if !strings.Contains(stdout+stderr, "new_client") {
		t.Fatalf("expected new_client error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestQueueMove_NewClientError covers queueMoveAction's new_client error
// branch. Two valid args (hash + numeric position) get past the argument
// validation so getClient() is reached and fails.
func TestQueueMove_NewClientError(t *testing.T) {
	defer withInvalidDaemonURI(t)()

	app := cli.NewApp()
	ctx := newContext(app, []string{"somehash", "0"}, "move")
	stdout, stderr := captureOutput(func() {
		if err := queueMoveAction(ctx); err != nil {
			t.Errorf("queueMoveAction: unexpected error: %v", err)
		}
	})
	if !strings.Contains(stdout+stderr, "new_client") {
		t.Fatalf("expected new_client error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestAttach_NewClientError covers attach()'s new_client error branch.
func TestAttach_NewClientError(t *testing.T) {
	defer withInvalidDaemonURI(t)()

	app := cli.NewApp()
	ctx := newContext(app, []string{"somehash"}, "attach")
	stdout, stderr := captureOutput(func() {
		if err := attach(ctx); err != nil {
			t.Errorf("attach: unexpected error: %v", err)
		}
	})
	if !strings.Contains(stdout+stderr, "new_client") {
		t.Fatalf("expected new_client error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestStop_NewClientError covers stop()'s new_client error branch.
func TestStop_NewClientError(t *testing.T) {
	defer withInvalidDaemonURI(t)()

	app := cli.NewApp()
	ctx := newContext(app, []string{"somehash"}, "stop")
	stdout, stderr := captureOutput(func() {
		if err := stop(ctx); err != nil {
			t.Errorf("stop: unexpected error: %v", err)
		}
	})
	if !strings.Contains(stdout+stderr, "new_client") {
		t.Fatalf("expected new_client error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestResume_NewClientError covers resume()'s new_client error branch.
// Unlike the other commands, resume() propagates the getClient() error
// via its named return value (a bare `return` after PrintRuntimeErr), so
// the error is non-nil here. The resume globals are set to inert values
// so the path reaches getClient() (which fails on the invalid URI)
// without other side effects.
func TestResume_NewClientError(t *testing.T) {
	defer withInvalidDaemonURI(t)()

	oldMaxParts, oldMaxConns, oldForce, oldProxy, oldUA := maxParts, maxConns, forceParts, proxyURL, userAgent
	maxParts, maxConns, forceParts, proxyURL, userAgent = 1, 1, false, "", ""
	defer func() {
		maxParts, maxConns, forceParts, proxyURL, userAgent = oldMaxParts, oldMaxConns, oldForce, oldProxy, oldUA
	}()

	app := cli.NewApp()
	ctx := newContext(app, []string{"somehash"}, "resume")
	var resumeErr error
	stdout, stderr := captureOutput(func() {
		resumeErr = resume(ctx)
	})
	// resume() reports the error and returns it (named return).
	if resumeErr == nil {
		t.Fatalf("expected non-nil error from resume new_client branch")
	}
	if !strings.Contains(stdout+stderr, "new_client") {
		t.Fatalf("expected new_client error output, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestIsBatchSubmissionCompleteInManager_ZeroTotal covers the total <= 0
// guard in isBatchSubmissionCompleteInManager. The daemon's list reports
// the matching download with TotalSize 0 (unknown size), which the
// function treats as "not complete" (returns false) because completeness
// cannot be inferred without a known total.
func TestIsBatchSubmissionCompleteInManager_ZeroTotal(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	listOverride = []*warplib.Item{{
		Hash:       "zero-total",
		Name:       "f.bin",
		TotalSize:  0, // unknown total → cannot judge completeness
		Downloaded: 5,
		DateAdded:  time.Now(),
		Parts:      make(map[int64]*warplib.ItemPart),
	}}
	defer func() { listOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	sub := BatchSubmission{DownloadID: "zero-total", SavePath: filepath.Join(t.TempDir(), "absent.bin")}
	if isBatchSubmissionCompleteInManager(sub) {
		t.Fatal("expected false for zero-total manager item")
	}
}

// TestIsBatchSubmissionCompleteInManager_NoMatch covers the loop-fallthrough
// return: the manager has items but none match the submission's
// DownloadID, so the function returns false.
func TestIsBatchSubmissionCompleteInManager_NoMatch(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	listOverride = []*warplib.Item{{
		Hash:       "other-id",
		Name:       "f.bin",
		TotalSize:  10,
		Downloaded: 10,
		DateAdded:  time.Now(),
		Parts:      make(map[int64]*warplib.ItemPart),
	}}
	defer func() { listOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	sub := BatchSubmission{DownloadID: "missing-id", SavePath: filepath.Join(t.TempDir(), "absent.bin")}
	if isBatchSubmissionCompleteInManager(sub) {
		t.Fatal("expected false when no manager item matches")
	}
}

// TestIsBatchSubmissionCompleteInManager_ListError covers the List-error
// branch: with an invalid daemon URI the inner getClient() succeeds-or
// the List call fails; either way the function must report not-complete
// (false) rather than panicking. We use a daemon that errors UPDATE_LIST.
func TestIsBatchSubmissionCompleteInManager_ListError(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_LIST: "list failed",
	})
	defer srv.close()

	sub := BatchSubmission{DownloadID: "any", SavePath: filepath.Join(t.TempDir(), "absent.bin")}
	if isBatchSubmissionCompleteInManager(sub) {
		t.Fatal("expected false when manager list errors")
	}
}

// TestIsBatchSubmissionCompleteInManager_ClientError covers the
// getClient-failure branch: an invalid daemon URI makes the inner
// getClient() fail, and the function returns false.
func TestIsBatchSubmissionCompleteInManager_ClientError(t *testing.T) {
	defer withInvalidDaemonURI(t)()

	sub := BatchSubmission{DownloadID: "any", SavePath: filepath.Join(t.TempDir(), "absent.bin")}
	if isBatchSubmissionCompleteInManager(sub) {
		t.Fatal("expected false when getClient fails")
	}
}
