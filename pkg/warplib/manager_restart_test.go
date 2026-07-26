package warplib

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type restartProtocolDownloader struct {
	hash            string
	fileName        string
	downloadDir     string
	contentLength   int64
	probed          bool
	downloadCalls   int
	receivedHandler bool
}

func (d *restartProtocolDownloader) Probe(context.Context) (ProbeResult, error) {
	d.probed = true
	return ProbeResult{
		FileName:      d.fileName,
		ContentLength: d.contentLength,
		Resumable:     true,
	}, nil
}

func (d *restartProtocolDownloader) Download(_ context.Context, handlers *Handlers) error {
	if !d.probed {
		return ErrProbeRequired
	}
	d.downloadCalls++
	d.receivedHandler = handlers != nil
	if handlers != nil && handlers.DownloadProgressHandler != nil {
		handlers.DownloadProgressHandler(d.hash, int(d.contentLength))
	}
	if handlers != nil && handlers.DownloadCompleteHandler != nil {
		handlers.DownloadCompleteHandler(MAIN_HASH, d.contentLength)
	}
	return nil
}

func (d *restartProtocolDownloader) Resume(context.Context, map[int64]*ItemPart, *Handlers) error {
	return nil
}

func (d *restartProtocolDownloader) Capabilities() DownloadCapabilities {
	return DownloadCapabilities{SupportsResume: true}
}

func (d *restartProtocolDownloader) Close() error                 { return nil }
func (d *restartProtocolDownloader) Stop()                        {}
func (d *restartProtocolDownloader) IsStopped() bool              { return false }
func (d *restartProtocolDownloader) GetMaxConnections() int32     { return 1 }
func (d *restartProtocolDownloader) GetMaxParts() int32           { return 1 }
func (d *restartProtocolDownloader) GetHash() string              { return d.hash }
func (d *restartProtocolDownloader) GetFileName() string          { return d.fileName }
func (d *restartProtocolDownloader) GetDownloadDirectory() string { return d.downloadDir }
func (d *restartProtocolDownloader) GetSavePath() string {
	return filepath.Join(d.downloadDir, d.fileName)
}
func (d *restartProtocolDownloader) GetContentLength() ContentLength {
	return ContentLength(d.contentLength)
}

func TestWriteManagerDataAtomic_PreservesPreviousSnapshotOnRenameFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "userdata.warp")
	oldData := ManagerData{Items: ItemsMap{
		"old": {
			Hash:  "old",
			Name:  "old.bin",
			Parts: make(map[int64]*ItemPart),
		},
	}}
	if err := writeManagerDataAtomic(path, oldData); err != nil {
		t.Fatalf("write old snapshot: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read old snapshot: %v", err)
	}

	originalRename := renameManagerFile
	renameManagerFile = func(_, _ string) error {
		return errors.New("injected rename failure")
	}
	defer func() { renameManagerFile = originalRename }()

	newData := ManagerData{Items: ItemsMap{
		"new": {
			Hash:  "new",
			Name:  "new.bin",
			Parts: make(map[int64]*ItemPart),
		},
	}}
	if err := writeManagerDataAtomic(path, newData); err == nil {
		t.Fatal("expected rename failure")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved snapshot: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("previous snapshot changed after failed atomic replacement")
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".userdata.warp.tmp-*")); err != nil {
		t.Fatalf("glob temporary snapshots: %v", err)
	} else if len(matches) != 0 {
		t.Fatalf("temporary snapshots leaked: %v", matches)
	}
}

func TestManagerFlushPreservesResumeDataOnPreCommitFailure(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	const hash = "flush-precommit"
	manager.UpdateItem(&Item{
		Hash:       hash,
		Name:       "complete.bin",
		TotalSize:  1,
		Downloaded: 1,
		Parts:      make(map[int64]*ItemPart),
	})
	stateDir := GetPath(DlDataDir, hash)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	marker := filepath.Join(stateDir, "part")
	if err := os.WriteFile(marker, []byte("resume-data"), 0600); err != nil {
		t.Fatalf("write state marker: %v", err)
	}

	originalRename := renameManagerFile
	renameManagerFile = func(_, _ string) error {
		return errors.New("injected pre-commit failure")
	}
	if err := manager.Flush(); err == nil {
		t.Fatal("Flush unexpectedly succeeded")
	}
	renameManagerFile = originalRename
	defer func() { renameManagerFile = originalRename }()

	if manager.GetItem(hash) == nil {
		t.Fatal("pre-commit failure did not restore the in-memory item")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("pre-commit failure removed resume data: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestManagerDeletionKeepsCommittedStateAfterDirectorySyncFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remove func(*Manager, string) error
	}{
		{
			name: "flush-one",
			remove: func(manager *Manager, hash string) error {
				return manager.FlushOne(hash)
			},
		},
		{
			name: "purge-failed",
			remove: func(manager *Manager, hash string) error {
				return manager.PurgeFailedDownload(hash)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			if err := SetConfigDir(base); err != nil {
				t.Fatalf("SetConfigDir: %v", err)
			}
			manager, err := InitManager()
			if err != nil {
				t.Fatalf("InitManager: %v", err)
			}

			hash := "postcommit-" + tc.name
			manager.UpdateItem(&Item{
				Hash:       hash,
				Name:       "entry.bin",
				TotalSize:  1,
				Downloaded: 1,
				Parts:      make(map[int64]*ItemPart),
			})
			stateDir := GetPath(DlDataDir, hash)
			if err := os.MkdirAll(stateDir, 0700); err != nil {
				t.Fatalf("create state directory: %v", err)
			}

			originalSync := syncManagerParentDirectory
			syncFailure := errors.New("injected directory sync failure")
			syncManagerParentDirectory = func(string) error {
				return syncFailure
			}
			removeErr := tc.remove(manager, hash)
			syncManagerParentDirectory = originalSync
			defer func() { syncManagerParentDirectory = originalSync }()

			if !errors.Is(removeErr, syncFailure) {
				t.Fatalf("remove error = %v, want %v", removeErr, syncFailure)
			}
			if !managerStoreCommitSucceeded(removeErr) {
				t.Fatalf("post-rename failure was not classified as committed: %v", removeErr)
			}
			if manager.GetItem(hash) != nil {
				t.Fatal("committed deletion was rolled back in memory")
			}
			if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
				t.Fatalf("committed deletion left state directory, stat error = %v", err)
			}
			if err := manager.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			reopened, err := InitManager()
			if err != nil {
				t.Fatalf("reopen manager: %v", err)
			}
			defer reopened.Close()
			if reopened.GetItem(hash) != nil {
				t.Fatal("disk snapshot retained an item after committed deletion")
			}
		})
	}
}

func TestManagerCloseCanRetryAfterTransientPersistFailure(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	originalRename := renameManagerFile
	renameManagerFile = func(_, _ string) error {
		return errors.New("transient rename failure")
	}
	defer func() { renameManagerFile = originalRename }()
	manager.UpdateItem(&Item{
		Hash:  "retry-close",
		Name:  "retry.bin",
		Parts: make(map[int64]*ItemPart),
	})
	if err := manager.Close(); err == nil {
		t.Fatal("first Close succeeded despite injected persistence failure")
	}
	if manager.closed {
		t.Fatal("manager marked closed after failed final persist")
	}

	renameManagerFile = originalRename
	if err := manager.Close(); err != nil {
		t.Fatalf("retry Close: %v", err)
	}

	reopened, err := InitManager()
	if err != nil {
		t.Fatalf("reopen manager: %v", err)
	}
	defer reopened.Close()
	if reopened.GetItem("retry-close") == nil {
		t.Fatal("retry did not persist pending manager state")
	}
}

func TestInitManager_InitializesDecodedItemSynchronization(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	data := ManagerData{Items: ItemsMap{
		"decoded": {
			Hash: "decoded",
			Name: "decoded.bin",
			Parts: map[int64]*ItemPart{
				0: {Hash: "part", FinalOffset: 10},
			},
		},
	}}
	if err := writeManagerDataAtomic(__USERDATA_FILE_NAME, data); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	item := manager.GetItem("decoded")
	if item == nil {
		t.Fatal("decoded item not found")
	}
	if item.mu != manager.mu {
		t.Fatal("decoded item does not use manager mutex")
	}
	if !item.HasParts() {
		t.Fatal("decoded part index was not reconstructed")
	}
	if offset, part := item.getPart("part"); offset != 0 || part == nil {
		t.Fatalf("decoded memPart lookup = (%d, %v)", offset, part)
	}
}

func TestRestoredQueuedFreshItemCanStart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("queued-restart"), 128)
	server := newE2ETestServer(t, content)
	defer server.Close()
	client := &http.Client{}

	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	first.SetMaxConcurrentDownloads(1, nil)
	first.GetQueue().Pause()

	downloader, err := NewDownloader(client, server.URL+"/queued.bin", &DownloaderOpts{
		FileName:          "queued.bin",
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
		Overwrite:         true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	hash := downloader.GetHash()
	if err := first.AddDownload(downloader, &AddDownloadOpts{}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	if !first.GetQueue().IsWaiting(hash) {
		t.Fatal("paused queue did not persist new item as waiting")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first manager: %v", err)
	}
	_ = downloader.Close()

	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	defer second.Close()
	second.SetSchemeRouter(NewSchemeRouter(client))

	runDone := make(chan error, 1)
	second.SetMaxConcurrentDownloads(1, func(restoredHash string) {
		item, restoreErr := second.ResumeDownload(client, restoredHash, &ResumeDownloadOpts{Fresh: true})
		if restoreErr != nil {
			runDone <- restoreErr
			return
		}
		go func() {
			runDone <- item.Start()
		}()
	})
	if !second.GetQueue().IsPaused() {
		t.Fatal("restored queue lost paused state")
	}
	if second.GetQueue().ActiveCount() != 0 {
		t.Fatal("paused restored queue started work")
	}

	second.GetQueue().Resume()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("restored queued download: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restored queued download timed out")
	}

	got, err := os.ReadFile(filepath.Join(base, "queued.bin"))
	if err != nil {
		t.Fatalf("read completed file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("completed file differs: got %d bytes, want %d", len(got), len(content))
	}
}

func TestManagerPersistsQuiescedStoppedQueueSlotForRestart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	first.SetMaxConcurrentDownloads(1, nil)
	queue := first.GetQueue()
	addPersistedRunnableQueueItem(first, "interrupted")
	addPersistedRunnableQueueItem(first, "waiting")
	queue.Add("interrupted", PriorityHigh)
	queue.Add("waiting", PriorityNormal)
	queue.Quiesce()
	queue.OnStopped("interrupted")
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	started := make(chan string, 1)
	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	defer second.Close()
	second.SetMaxConcurrentDownloads(1, func(hash string) { started <- hash })
	select {
	case hash := <-started:
		if hash != "interrupted" {
			t.Fatalf("first restored queue start = %q, want interrupted", hash)
		}
	case <-time.After(time.Second):
		t.Fatal("quiesced interrupted queue slot was not restored")
	}
	if !second.GetQueue().IsWaiting("waiting") {
		t.Fatal("waiting queue entry was lost while restoring interrupted work")
	}
}

func TestManagerRestoreDropsCompletedAndMissingQueueEntries(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const (
		completedHash     = "completed-before-queue-release"
		zeroCompletedHash = "completed-empty-file"
		incompleteHash    = "still-runnable"
		zeroRunnableHash  = "queued-empty-file"
		unknownHash       = "unknown-size"
		missingHash       = "missing-item"
	)
	data := ManagerData{
		Items: ItemsMap{
			completedHash: {
				Hash:       completedHash,
				Name:       "complete.bin",
				TotalSize:  32,
				Downloaded: 32,
				Parts:      make(map[int64]*ItemPart),
			},
			zeroCompletedHash: {
				Hash:      zeroCompletedHash,
				Name:      "complete-empty.bin",
				TotalSize: 0,
				Parts:     nil,
			},
			incompleteHash: {
				Hash:       incompleteHash,
				Name:       "incomplete.bin",
				TotalSize:  32,
				Downloaded: 8,
				Parts:      make(map[int64]*ItemPart),
			},
			zeroRunnableHash: {
				Hash:      zeroRunnableHash,
				Name:      "queued-empty.bin",
				TotalSize: 0,
				Parts:     make(map[int64]*ItemPart),
			},
			unknownHash: {
				Hash:      unknownHash,
				Name:      "unknown.bin",
				TotalSize: -1,
				Parts:     nil,
			},
		},
		QueueState: &QueueState{
			MaxConcurrent: 1,
			Active: []QueuedItemState{
				{Hash: completedHash, Priority: PriorityHigh},
				{Hash: incompleteHash, Priority: PriorityNormal},
			},
			Waiting: []QueuedItemState{
				{Hash: missingHash, Priority: PriorityHigh},
				{Hash: completedHash, Priority: PriorityLow},
				{Hash: zeroCompletedHash, Priority: PriorityLow},
				{Hash: zeroRunnableHash, Priority: PriorityLow},
				{Hash: unknownHash, Priority: PriorityLow},
			},
		},
	}
	if err := writeManagerDataAtomic(__USERDATA_FILE_NAME, data); err != nil {
		t.Fatalf("write crash-window snapshot: %v", err)
	}

	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()

	var started []string
	manager.SetMaxConcurrentDownloads(1, func(hash string) {
		started = append(started, hash)
	})
	if len(started) != 1 || started[0] != incompleteHash {
		t.Fatalf("restored starts = %v, want only %q", started, incompleteHash)
	}
	queue := manager.GetQueue()
	for _, hash := range []string{completedHash, zeroCompletedHash, missingHash} {
		if queue.IsActive(hash) || queue.IsWaiting(hash) {
			t.Fatalf("non-runnable hash %q survived queue reconciliation", hash)
		}
	}
	if !queue.IsActive(incompleteHash) {
		t.Fatal("runnable restored item did not retain the queue slot")
	}
	for _, hash := range []string{zeroRunnableHash, unknownHash} {
		if !queue.IsWaiting(hash) {
			t.Fatalf("runnable edge-case hash %q was removed during reconciliation", hash)
		}
	}
	if manager.GetItem(completedHash) == nil {
		t.Fatal("queue reconciliation removed completed download history")
	}

	snapshotFile, err := os.Open(__USERDATA_FILE_NAME)
	if err != nil {
		t.Fatalf("open reconciled snapshot: %v", err)
	}
	var persisted ManagerData
	decodeErr := gob.NewDecoder(snapshotFile).Decode(&persisted)
	closeErr := snapshotFile.Close()
	if decodeErr != nil {
		t.Fatalf("decode reconciled snapshot: %v", decodeErr)
	}
	if closeErr != nil {
		t.Fatalf("close reconciled snapshot: %v", closeErr)
	}
	if persisted.QueueState == nil {
		t.Fatal("reconciled queue state was not persisted")
	}
	for _, item := range append(
		append([]QueuedItemState(nil), persisted.QueueState.Active...),
		persisted.QueueState.Waiting...,
	) {
		if item.Hash == completedHash || item.Hash == zeroCompletedHash || item.Hash == missingHash {
			t.Fatalf("stale queue entry %q remained in persisted snapshot", item.Hash)
		}
	}
}

func TestManagerDeletionRemovesWaitingQueueMembership(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	manager.SetMaxConcurrentDownloads(1, nil)
	manager.GetQueue().Pause()
	const hash = "waiting-purge"
	manager.UpdateItem(&Item{
		Hash:      hash,
		Name:      "waiting.bin",
		TotalSize: 10,
		Parts:     make(map[int64]*ItemPart),
	})
	manager.GetQueue().Add(hash, PriorityNormal)
	if !manager.GetQueue().IsWaiting(hash) {
		t.Fatal("test item was not queued")
	}

	if err := manager.PurgeFailedDownload(hash); err != nil {
		t.Fatalf("purge waiting item: %v", err)
	}
	if manager.GetItem(hash) != nil || manager.GetQueue().IsWaiting(hash) {
		t.Fatal("item deletion left live manager or queue state")
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	restored, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager restored: %v", err)
	}
	restored.SetMaxConcurrentDownloads(1, nil)
	if restored.GetItem(hash) != nil ||
		restored.GetQueue().IsWaiting(hash) ||
		restored.GetQueue().IsActive(hash) {
		t.Fatal("deleted waiting hash survived in the persisted queue snapshot")
	}
	if err := restored.Close(); err != nil {
		t.Fatalf("close restored: %v", err)
	}
}

func TestManagerFlushPreservesRunnablePendingState(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()
	manager.SetMaxConcurrentDownloads(1, nil)
	manager.GetQueue().Pause()

	const queuedHash = "waiting-history"
	manager.UpdateItem(&Item{
		Hash:      queuedHash,
		Name:      "waiting.bin",
		TotalSize: 10,
		Parts:     make(map[int64]*ItemPart),
	})
	manager.GetQueue().Add(queuedHash, PriorityNormal)

	const scheduledHash = "future-schedule"
	manager.UpdateItem(&Item{
		Hash:          scheduledHash,
		Name:          "scheduled.bin",
		TotalSize:     10,
		Parts:         make(map[int64]*ItemPart),
		ScheduleState: ScheduleStateScheduled,
		ScheduledAt:   time.Now().Add(time.Hour),
	})

	if err := manager.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if manager.GetItem(queuedHash) == nil || !manager.GetQueue().IsWaiting(queuedHash) {
		t.Fatal("Flush discarded runnable waiting item")
	}
	if manager.GetItem(scheduledHash) == nil {
		t.Fatal("Flush discarded future scheduled item")
	}
	for _, hash := range []string{queuedHash, scheduledHash} {
		if err := manager.FlushOne(hash); !errors.Is(err, ErrFlushItemDownloading) {
			t.Fatalf("FlushOne(%q) error = %v, want ErrFlushItemDownloading", hash, err)
		}
	}
}

func TestFlushOneRejectsQueueActiveItemWithoutDownloader(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()
	manager.SetMaxConcurrentDownloads(1, nil)
	const hash = "active-without-downloader"
	manager.UpdateItem(&Item{
		Hash:      hash,
		Name:      "active.bin",
		TotalSize: 10,
		Parts:     make(map[int64]*ItemPart),
	})
	manager.GetQueue().Add(hash, PriorityNormal)
	if err := manager.FlushOne(hash); !errors.Is(err, ErrFlushItemDownloading) {
		t.Fatalf("FlushOne error = %v, want ErrFlushItemDownloading", err)
	}
	if manager.GetItem(hash) == nil || !manager.GetQueue().IsActive(hash) {
		t.Fatal("rejected active flush mutated manager or queue state")
	}
}

func TestPurgeQueueRemovalRollsBackOnPreCommitFailure(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()
	manager.SetMaxConcurrentDownloads(1, nil)
	manager.GetQueue().Pause()
	const hash = "waiting-rollback"
	manager.UpdateItem(&Item{
		Hash:      hash,
		Name:      "rollback.bin",
		TotalSize: 10,
		Parts:     make(map[int64]*ItemPart),
	})
	manager.GetQueue().Add(hash, PriorityNormal)

	originalRename := renameManagerFile
	t.Cleanup(func() { renameManagerFile = originalRename })
	renameManagerFile = func(_, _ string) error {
		return errors.New("injected queue snapshot failure")
	}
	err = manager.PurgeFailedDownload(hash)
	renameManagerFile = originalRename
	if err == nil {
		t.Fatal("PurgeFailedDownload unexpectedly succeeded")
	}
	if manager.GetItem(hash) == nil || !manager.GetQueue().IsWaiting(hash) {
		t.Fatal("pre-commit failure did not restore item and queue membership")
	}
}

func TestRestoredActiveQueueReusesOnlyClaimedEmptyDestination(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("claimed-empty-restart"), 64)
	server := newE2ETestServer(t, content)
	defer server.Close()
	client := server.Client()
	crashAfterOpen := errors.New("simulated crash after destination open")

	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	var firstStartErr error
	first.SetMaxConcurrentDownloads(1, func(hash string) {
		item := first.GetItem(hash)
		if item == nil {
			firstStartErr = ErrDownloadNotFound
			return
		}
		firstStartErr = item.Start()
	})
	downloader, err := NewDownloader(client, server.URL+"/claimed.bin", &DownloaderOpts{
		FileName:          "claimed.bin",
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
		Handlers: &Handlers{
			DestinationClaimedHandler: func() error {
				return crashAfterOpen
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	hash := downloader.GetHash()
	if err := first.AddDownload(downloader, &AddDownloadOpts{AbsoluteLocation: base}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	if !errors.Is(firstStartErr, crashAfterOpen) {
		t.Fatalf("first Start error = %v, want simulated crash", firstStartErr)
	}
	item := first.GetItem(hash)
	if item == nil {
		t.Fatal("active item missing")
	}
	snapshot := item.Snapshot()
	if !snapshot.DestinationClaimed || item.HasParts() || snapshot.Downloaded != 0 {
		t.Fatalf("pre-crash state = %+v, hasParts=%v", snapshot, item.HasParts())
	}
	destination := filepath.Join(base, "claimed.bin")
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatalf("Stat claimed destination: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("claimed destination size = %d, want empty", info.Size())
	}
	if !first.GetQueue().IsActive(hash) {
		t.Fatal("crashed item was not persisted as active")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first manager: %v", err)
	}
	_ = downloader.Close()

	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	defer second.Close()
	var restartErr error
	second.SetMaxConcurrentDownloads(1, func(restoredHash string) {
		restored, restoreErr := second.ResumeDownload(client, restoredHash, &ResumeDownloadOpts{Fresh: true})
		if restoreErr != nil {
			restartErr = restoreErr
			return
		}
		restartErr = restored.Start()
	})
	if restartErr != nil {
		t.Fatalf("restored active queue item: %v", restartErr)
	}

	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("ReadFile completed destination: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("completed destination differs: got %d bytes, want %d", len(got), len(content))
	}
	if second.GetItem(hash).Snapshot().DestinationClaimed {
		t.Fatal("destination claim was not cleared after completion")
	}
	if second.GetQueue().ActiveCount() != 0 {
		t.Fatal("completed restored item retained its queue slot")
	}
}

func TestFreshReconstructionPreservesUnclaimedOrNonEmptyDestination(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("strict-collision"), 32)
	server := newE2ETestServer(t, content)
	defer server.Close()

	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer manager.Close()
	const hash = "unclaimed-empty"
	manager.UpdateItem(&Item{
		Hash:             hash,
		Name:             "existing.bin",
		Url:              server.URL + "/existing.bin",
		TotalSize:        ContentLength(len(content)),
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
	})
	destination := filepath.Join(base, "existing.bin")
	if err := os.WriteFile(destination, nil, DefaultFileMode); err != nil {
		t.Fatalf("WriteFile existing destination: %v", err)
	}

	restored, err := manager.ResumeDownload(server.Client(), hash, &ResumeDownloadOpts{Fresh: true})
	if err != nil {
		t.Fatalf("ResumeDownload fresh: %v", err)
	}
	err = restored.Start()
	if !errors.Is(err, ErrFileExists) {
		t.Fatalf("Start error = %v, want ErrFileExists", err)
	}
	info, statErr := os.Stat(destination)
	if statErr != nil {
		t.Fatalf("Stat existing destination: %v", statErr)
	}
	if info.Size() != 0 {
		t.Fatalf("unclaimed destination was modified: size=%d", info.Size())
	}

	const claimedHash = "claimed-non-empty"
	manager.UpdateItem(&Item{
		Hash:               claimedHash,
		Name:               "changed.bin",
		Url:                server.URL + "/changed.bin",
		TotalSize:          ContentLength(len(content)),
		DownloadLocation:   base,
		AbsoluteLocation:   base,
		Resumable:          true,
		Parts:              make(map[int64]*ItemPart),
		DestinationClaimed: true,
	})
	changedDestination := filepath.Join(base, "changed.bin")
	replacement := []byte("user replacement")
	if err := os.WriteFile(changedDestination, replacement, DefaultFileMode); err != nil {
		t.Fatalf("WriteFile changed destination: %v", err)
	}
	claimed, err := manager.ResumeDownload(server.Client(), claimedHash, &ResumeDownloadOpts{Fresh: true})
	if err != nil {
		t.Fatalf("ResumeDownload claimed fresh: %v", err)
	}
	err = claimed.Start()
	if !errors.Is(err, ErrFileExists) {
		t.Fatalf("claimed Start error = %v, want ErrFileExists for non-empty replacement", err)
	}
	unchanged, err := os.ReadFile(changedDestination)
	if err != nil {
		t.Fatalf("ReadFile changed destination: %v", err)
	}
	if !bytes.Equal(unchanged, replacement) {
		t.Fatal("non-empty replacement was overwritten")
	}
}

func TestRestoredScheduledFreshItemCanStart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("scheduled-restart"), 96)
	server := newE2ETestServer(t, content)
	defer server.Close()
	client := &http.Client{}

	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	downloader, err := NewDownloader(client, server.URL+"/scheduled.bin", &DownloaderOpts{
		FileName:          "scheduled.bin",
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
		Overwrite:         true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	hash := downloader.GetHash()
	if err := first.AddDownload(downloader, &AddDownloadOpts{SkipQueue: true}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	if err := first.ConfigureSchedule(hash, time.Now().Add(time.Hour), "", ScheduleStateScheduled); err != nil {
		t.Fatalf("ConfigureSchedule: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	_ = downloader.Close()

	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	defer second.Close()
	restored, err := second.ResumeDownload(client, hash, &ResumeDownloadOpts{Fresh: true})
	if err != nil {
		t.Fatalf("reconstruct scheduled item: %v", err)
	}
	if err := restored.Start(); err != nil {
		t.Fatalf("start restored scheduled item: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(base, "scheduled.bin"))
	if err != nil {
		t.Fatalf("read completed file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("completed file differs: got %d bytes, want %d", len(got), len(content))
	}
}

func TestFreshRecurringHTTPReprobesGrowingResourceAfterRestart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	var resource struct {
		sync.RWMutex
		body []byte
		etag string
	}
	resource.body = bytes.Repeat([]byte("first-version-"), 4096)
	resource.etag = `"v1"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resource.RLock()
		body := append([]byte(nil), resource.body...)
		etag := resource.etag
		resource.RUnlock()

		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="server-name.bin"`)
		w.Header().Set("ETag", etag)
		rangeValue := r.Header.Get("Range")
		if rangeValue == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
		bounds := strings.SplitN(strings.TrimPrefix(rangeValue, "bytes="), "-", 2)
		start, _ := strconv.Atoi(bounds[0])
		end := len(body) - 1
		if len(bounds) == 2 && bounds[1] != "" {
			end, _ = strconv.Atoi(bounds[1])
		}
		if start < 0 || start > end || end >= len(body) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		chunk := body[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
	defer server.Close()

	firstBody := append([]byte(nil), resource.body...)
	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	downloader, err := NewDownloader(server.Client(), server.URL+"/resource", &DownloaderOpts{
		FileName:          "first.bin",
		LockFileName:      true,
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
	})
	if err != nil {
		t.Fatalf("NewDownloader first: %v", err)
	}
	hash := downloader.GetHash()
	if err := first.AddDownload(downloader, &AddDownloadOpts{AbsoluteLocation: base}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	if err := downloader.Start(); err != nil {
		t.Fatalf("first occurrence: %v", err)
	}
	if err := first.RenameItem(hash, "second-occurrence.bin"); err != nil {
		t.Fatalf("RenameItem: %v", err)
	}
	if err := first.ConfigureSchedule(hash, time.Now().Add(time.Hour), "* * * * *", ScheduleStateScheduled); err != nil {
		t.Fatalf("ConfigureSchedule: %v", err)
	}

	resource.Lock()
	resource.body = append(firstBody, bytes.Repeat([]byte("grown-version-"), 5000)...)
	resource.etag = `"v2"`
	secondBody := append([]byte(nil), resource.body...)
	resource.Unlock()

	if err := first.Close(); err != nil {
		t.Fatalf("close first manager: %v", err)
	}

	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	defer second.Close()
	restored, err := second.ResumeDownload(server.Client(), hash, &ResumeDownloadOpts{Fresh: true})
	if err != nil {
		t.Fatalf("fresh reconstruction: %v", err)
	}
	reprobed := restored.Snapshot()
	if reprobed.TotalSize != ContentLength(len(secondBody)) {
		t.Fatalf("reprobed size = %d, want %d (stale first size %d)",
			reprobed.TotalSize, len(secondBody), len(firstBody))
	}
	if reprobed.ResourceETag != `"v2"` {
		t.Fatalf("reprobed ETag = %q, want v2", reprobed.ResourceETag)
	}
	if reprobed.Name != "second-occurrence.bin" {
		t.Fatalf("timestamped occurrence name changed to %q", reprobed.Name)
	}
	if err := restored.Start(); err != nil {
		t.Fatalf("second occurrence: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(base, "second-occurrence.bin"))
	if err != nil {
		t.Fatalf("ReadFile second occurrence: %v", err)
	}
	if !bytes.Equal(got, secondBody) {
		t.Fatalf("second occurrence bytes = %d, want full grown resource %d", len(got), len(secondBody))
	}
}

func TestScheduledHTTPRestoresOperationalConfigAndProxy(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("proxied-scheduled-content-"), 4096)
	proxyServer := newE2ETestServer(t, content)
	defer proxyServer.Close()
	proxyClient, err := NewHTTPClientWithProxy(proxyServer.URL)
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy: %v", err)
	}
	retryConfig := &RetryConfig{
		MaxRetries:    7,
		BaseDelay:     17 * time.Millisecond,
		MaxDelay:      3 * time.Second,
		JitterFactor:  0.2,
		BackoffFactor: 1.7,
	}
	checksumConfig := &ChecksumConfig{Enabled: true, FailOnMismatch: true}

	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	downloader, err := NewDownloader(proxyClient, "http://origin.invalid/scheduled-proxy.bin", &DownloaderOpts{
		ForceParts:          true,
		NumBaseParts:        2,
		FileName:            "scheduled-proxy.bin",
		LockFileName:        true,
		DownloadDirectory:   base,
		MaxConnections:      3,
		MaxSegments:         4,
		Overwrite:           true,
		RetryConfig:         retryConfig,
		RequestTimeout:      4 * time.Second,
		MaxFileSize:         int64(len(content) * 2),
		ChecksumConfig:      checksumConfig,
		SpeedLimit:          123456,
		DisableWorkStealing: true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	hash := downloader.GetHash()
	if err := first.AddDownload(downloader, &AddDownloadOpts{
		AbsoluteLocation: base,
		SkipQueue:        true,
		TransferConfig: TransferConfig{
			ProxyURL: proxyServer.URL,
		},
	}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	if err := first.ConfigureSchedule(hash, time.Now().Add(time.Hour), "", ScheduleStateScheduled); err != nil {
		t.Fatalf("ConfigureSchedule: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first manager: %v", err)
	}
	_ = downloader.Close()

	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	defer second.Close()
	snapshot := second.GetItem(hash).Snapshot()
	config := snapshot.TransferConfig
	if config.ProxyURL != proxyServer.URL || config.ProxyCredentialsRequired {
		t.Fatalf("persisted proxy config = %+v", config)
	}
	if !config.ForceParts || config.NumBaseParts != 1 ||
		config.MaxConnections != 3 || config.MaxSegments != 4 ||
		!config.Overwrite || !config.LockFileName ||
		config.RequestTimeout != 4*time.Second ||
		config.MaxFileSize != int64(len(content)*2) ||
		config.SpeedLimit != 123456 || !config.DisableWorkStealing {
		t.Fatalf("persisted operational config = %+v", config)
	}
	if config.RetryConfig == nil || *config.RetryConfig != *retryConfig {
		t.Fatalf("persisted retry config = %+v, want %+v", config.RetryConfig, retryConfig)
	}
	if config.ChecksumConfig == nil || *config.ChecksumConfig != *checksumConfig {
		t.Fatalf("persisted checksum config = %+v, want %+v", config.ChecksumConfig, checksumConfig)
	}
	publicJSON, err := json.Marshal(second.GetItem(hash))
	if err != nil {
		t.Fatalf("Marshal item: %v", err)
	}
	if bytes.Contains(publicJSON, []byte(proxyServer.URL)) ||
		bytes.Contains(publicJSON, []byte("TransferConfig")) {
		t.Fatalf("public item JSON exposed reconstruction config: %s", publicJSON)
	}

	// The supplied client cannot resolve origin.invalid. Successful probe and
	// transfer therefore prove ResumeDownload rebuilt the persisted proxy.
	restored, err := second.ResumeDownload(&http.Client{}, hash, &ResumeDownloadOpts{Fresh: true})
	if err != nil {
		t.Fatalf("fresh proxy reconstruction: %v", err)
	}
	if err := restored.Start(); err != nil {
		t.Fatalf("proxied scheduled transfer: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(base, "scheduled-proxy.bin"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("proxied bytes = %d, want %d", len(got), len(content))
	}
}

func TestManagerNeverPersistsProxyUserinfo(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("proxy-secret-boundary"), 64)
	server := newE2ETestServer(t, content)
	defer server.Close()
	manager, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	downloader, err := NewDownloader(server.Client(), server.URL+"/file.bin", &DownloaderOpts{
		FileName:          "file.bin",
		DownloadDirectory: base,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := manager.AddDownload(downloader, &AddDownloadOpts{
		AbsoluteLocation: base,
		SkipQueue:        true,
		TransferConfig: TransferConfig{
			ProxyURL: "http://proxy-user:proxy-password@proxy.example:8080",
		},
	}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	snapshot := manager.GetItem(downloader.GetHash()).Snapshot()
	if snapshot.TransferConfig.ProxyURL != "http://proxy.example:8080" ||
		!snapshot.TransferConfig.ProxyCredentialsRequired {
		t.Fatalf("sanitized proxy config = %+v", snapshot.TransferConfig)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	persisted, err := os.ReadFile(filepath.Join(base, "userdata.warp"))
	if err != nil {
		t.Fatalf("ReadFile userdata: %v", err)
	}
	if bytes.Contains(persisted, []byte("proxy-user")) ||
		bytes.Contains(persisted, []byte("proxy-password")) {
		t.Fatal("userdata contains proxy userinfo")
	}
	reopened, err := InitManager()
	if err != nil {
		t.Fatalf("reopen manager: %v", err)
	}
	defer reopened.Close()
	if _, err := reopened.ResumeDownload(server.Client(), downloader.GetHash(), &ResumeDownloadOpts{Fresh: true}); !errors.Is(err, ErrProxyCredentialsRequired) {
		t.Fatalf("authenticated proxy reconstruction error = %v", err)
	}
}

func TestRestoredScheduledProtocolFreshItemCanStart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const hash = "restored-scheduled-ftp"
	initial := &restartProtocolDownloader{
		hash:          hash,
		fileName:      "scheduled.tar",
		downloadDir:   base,
		contentLength: 768,
		probed:        true,
	}

	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	if err := first.AddProtocolDownload(
		initial,
		ProbeResult{FileName: initial.fileName, ContentLength: initial.contentLength, Resumable: true},
		"ftp://example.invalid/scheduled.tar",
		ProtoFTP,
		&Handlers{},
		&AddDownloadOpts{AbsoluteLocation: base, SkipQueue: true},
	); err != nil {
		t.Fatalf("AddProtocolDownload: %v", err)
	}
	if err := first.ConfigureSchedule(hash, time.Now().Add(time.Hour), "", ScheduleStateScheduled); err != nil {
		t.Fatalf("ConfigureSchedule: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	defer second.Close()

	var reconstructed *restartProtocolDownloader
	router := NewSchemeRouter(nil)
	router.Register("ftp", func(rawURL string, _ *DownloaderOpts) (ProtocolDownloader, error) {
		if rawURL != "ftp://example.invalid/scheduled.tar" {
			t.Fatalf("reconstructed URL = %q", rawURL)
		}
		reconstructed = &restartProtocolDownloader{
			hash:          hash,
			fileName:      initial.fileName,
			downloadDir:   base,
			contentLength: initial.contentLength,
		}
		return reconstructed, nil
	})
	second.SetSchemeRouter(router)

	restored, err := second.ResumeDownload(nil, hash, &ResumeDownloadOpts{Fresh: true})
	if err != nil {
		t.Fatalf("reconstruct scheduled protocol item: %v", err)
	}
	if err := restored.Start(); err != nil {
		t.Fatalf("start scheduled protocol item: %v", err)
	}
	if reconstructed == nil || reconstructed.downloadCalls != 1 || !reconstructed.receivedHandler {
		t.Fatalf("reconstructed downloader = %+v", reconstructed)
	}
	if restored.GetDownloaded() != ContentLength(initial.contentLength) {
		t.Fatalf("downloaded = %d, want %d", restored.GetDownloaded(), initial.contentLength)
	}
}

func TestUsernameOnlyProtocolQueueReconstructsAfterRestart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const hash = "queued-sftp-username"
	initial := &restartProtocolDownloader{
		hash:          hash,
		fileName:      "archive.tar",
		downloadDir:   base,
		contentLength: 768,
		probed:        true,
	}

	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	first.SetMaxConcurrentDownloads(1, nil)
	first.GetQueue().Pause()
	if err := first.AddProtocolDownload(
		initial,
		ProbeResult{FileName: "archive.tar", ContentLength: 768, Resumable: true},
		"sftp://example.invalid/archive.tar",
		ProtoSFTP,
		&Handlers{},
		&AddDownloadOpts{
			AbsoluteLocation: base,
			TransferConfig: TransferConfig{
				ProtocolUsername: "alice",
			},
		},
	); err != nil {
		t.Fatalf("AddProtocolDownload: %v", err)
	}
	if !first.GetQueue().IsWaiting(hash) {
		t.Fatal("username-only protocol item was not persisted as waiting")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	defer second.Close()
	router := NewSchemeRouter(nil)
	var reconstructedURL string
	restoredDownloader := &restartProtocolDownloader{
		hash:          hash,
		fileName:      "archive.tar",
		downloadDir:   base,
		contentLength: 768,
	}
	router.Register("sftp", func(rawURL string, _ *DownloaderOpts) (ProtocolDownloader, error) {
		reconstructedURL = rawURL
		return restoredDownloader, nil
	})
	second.SetSchemeRouter(router)

	runDone := make(chan error, 1)
	second.SetMaxConcurrentDownloads(0, func(restoredHash string) {
		item, restoreErr := second.ResumeDownload(nil, restoredHash, &ResumeDownloadOpts{Fresh: true})
		if restoreErr != nil {
			runDone <- restoreErr
			return
		}
		runDone <- item.Start()
	})
	if !second.GetQueue().IsPaused() {
		t.Fatal("paused queue state was not restored")
	}
	select {
	case err := <-runDone:
		t.Fatalf("paused unlimited queue started early: %v", err)
	default:
	}
	second.GetQueue().Resume()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("restored username-only transfer: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restored username-only transfer did not start")
	}
	if reconstructedURL != "sftp://alice@example.invalid/archive.tar" {
		t.Fatalf("reconstructed URL = %q", reconstructedURL)
	}
	publicJSON, err := json.Marshal(second.GetItem(hash))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if bytes.Contains(publicJSON, []byte("alice@")) ||
		bytes.Contains(publicJSON, []byte(`"ProtocolUsername"`)) {
		t.Fatalf("public JSON exposed protocol username/config: %s", publicJSON)
	}
}

func TestRecurringNextOccurrenceAndCancellationPersistAcrossRestart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	next := time.Now().Add(24 * time.Hour).Truncate(time.Second)

	first, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager first: %v", err)
	}
	first.UpdateItem(&Item{
		Hash:  "recurring",
		Name:  "backup.bin",
		Url:   "https://example.invalid/backup.bin",
		Parts: make(map[int64]*ItemPart),
	})
	if err := first.ConfigureSchedule("recurring", next, "0 2 * * *", ScheduleStateScheduled); err != nil {
		t.Fatalf("ConfigureSchedule: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager second: %v", err)
	}
	info, ok := second.GetScheduleInfo("recurring")
	if !ok {
		t.Fatal("recurring item missing after restart")
	}
	if info.State != ScheduleStateScheduled || info.CronExpr != "0 2 * * *" || !info.ScheduledAt.Equal(next) {
		t.Fatalf("restored schedule = %+v", info)
	}
	if err := second.SetScheduleState("recurring", ScheduleStateCancelled); err != nil {
		t.Fatalf("SetScheduleState(cancelled): %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second: %v", err)
	}

	third, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager third: %v", err)
	}
	defer third.Close()
	info, ok = third.GetScheduleInfo("recurring")
	if !ok || info.State != ScheduleStateCancelled {
		t.Fatalf("cancelled schedule after restart = %+v, found=%v", info, ok)
	}
}
