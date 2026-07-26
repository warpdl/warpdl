package api

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type recordingCloser struct {
	calls int
	err   error
}

func (c *recordingCloser) Close() error {
	c.calls++
	return c.err
}

type closerFunc func() error

func (f closerFunc) Close() error {
	return f()
}

func TestResolveDownloadScheduleRejectsInvalidInputs(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.Local)
	tests := []struct {
		name   string
		params common.DownloadParams
		want   string
	}{
		{
			name: "mutually exclusive start fields",
			params: common.DownloadParams{
				StartAt: "2026-07-26 12:00",
				StartIn: "1h",
			},
			want: "mutually exclusive",
		},
		{
			name:   "malformed absolute start",
			params: common.DownloadParams{StartAt: "tomorrow"},
			want:   "invalid start time",
		},
		{
			name:   "malformed relative start",
			params: common.DownloadParams{StartIn: "eventually"},
			want:   "invalid start delay",
		},
		{
			name:   "nonpositive relative start",
			params: common.DownloadParams{StartIn: "0s"},
			want:   "greater than zero",
		},
		{
			name:   "malformed standalone recurrence",
			params: common.DownloadParams{Schedule: "not a cron expression"},
			want:   "invalid schedule",
		},
		{
			name: "malformed recurrence with initial start",
			params: common.DownloadParams{
				StartIn:  "1h",
				Schedule: "not a cron expression",
			},
			want: "invalid schedule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, scheduled, err := resolveDownloadSchedule(&tt.params, now)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("resolveDownloadSchedule() error = %v, want containing %q", err, tt.want)
			}
			if scheduled {
				t.Fatal("invalid schedule reported as scheduled")
			}
		})
	}
}

func TestCleanupDownloadRegistrationReleasesRegisteredResources(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	const hash = "registered-cleanup"
	api.manager.UpdateItem(&warplib.Item{
		Hash:  hash,
		Name:  "orphan.bin",
		Parts: make(map[int64]*warplib.ItemPart),
	})
	pool.AddDownload(hash, nil)

	if err := cleanupDownloadRegistration(api.manager, pool, hash, nil); err != nil {
		t.Fatalf("cleanupDownloadRegistration() error = %v", err)
	}
	if item := api.manager.GetItem(hash); item != nil {
		t.Fatalf("registered item was not purged: %+v", item)
	}
	if pool.HasDownload(hash) {
		t.Fatal("pool registration was not removed")
	}
}

func TestCleanupDownloadRegistrationUsesFallbackCloser(t *testing.T) {
	api, _, cleanup := newTestApi(t)
	defer cleanup()

	closeErr := errors.New("close fallback")
	fallback := &recordingCloser{err: closeErr}
	err := cleanupDownloadRegistration(api.manager, nil, "unregistered-cleanup", fallback)
	if !errors.Is(err, closeErr) {
		t.Fatalf("cleanupDownloadRegistration() error = %v, want %v", err, closeErr)
	}
	if fallback.calls != 1 {
		t.Fatalf("fallback Close() calls = %d, want 1", fallback.calls)
	}
}

func TestCleanupDownloadRegistrationRemovesGenerationLast(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	const hash = "cleanup-generation-order"
	api.manager.UpdateItem(&warplib.Item{
		Hash:  hash,
		Name:  "orphan.bin",
		Parts: make(map[int64]*warplib.ItemPart),
	})
	generation, ok := pool.BeginDownload(hash, nil)
	if !ok {
		t.Fatal("reserve cleanup generation")
	}

	replacementStartedDuringClose := false
	fallback := closerFunc(func() error {
		replacement, reserved := pool.BeginDownload(hash, nil)
		replacementStartedDuringClose = reserved
		if replacement != nil {
			replacement.Abort()
		}
		return nil
	})
	if err := cleanupDownloadRegistration(
		api.manager,
		pool,
		hash,
		fallback,
		generation,
	); err != nil {
		t.Fatalf("cleanupDownloadRegistration() error = %v", err)
	}
	if replacementStartedDuringClose {
		t.Fatal("same-hash replacement was admitted before manager cleanup")
	}
	if pool.HasDownload(hash) {
		t.Fatal("cleanup generation remains registered")
	}
	if replacement, reserved := pool.BeginDownload(hash, nil); !reserved {
		t.Fatal("same-hash replacement was not admitted after cleanup")
	} else {
		replacement.Abort()
	}
}

func TestAPIReportAsyncDownloadErrorUsesManagerCleanup(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	api.reportAsyncDownloadError(pool, "unknown-download", errors.New("start failed"))
	if got := pool.GetError("unknown-download"); got == nil || got.Message != "start failed" {
		t.Fatalf("recorded error = %+v", got)
	}
}
