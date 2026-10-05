package warplib

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestResumeUnderConcurrentPartMutation drives a real Item.Resume() over a
// local HTTP server while another goroutine mutates the same item's persisted
// parts through the production addPart path used by download callbacks.
// Resume must iterate the Parts snapshot taken under the item lock; iterating
// the live map would race with these writes and with the download's own part
// callbacks. The server holds its response until the mutation loop stops, so
// completion (which clears Parts) cannot interleave with a parts write, and
// the resumed destination must be byte-identical to the served content.
func TestResumeUnderConcurrentPartMutation(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	const etag = `"concurrent-resume-v1"`
	content := bytes.Repeat([]byte("warpdl-concurrent-resume"), 4096)
	half := int64(len(content) / 2)

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		<-release
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", etag)
		rangeSpec := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		startValue, endValue, ok := strings.Cut(rangeSpec, "-")
		if !ok {
			http.Error(w, "missing range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		start, startErr := strconv.ParseInt(startValue, 10, 64)
		end, endErr := strconv.ParseInt(endValue, 10, 64)
		if startErr != nil || endErr != nil || start < 0 || end < start || end >= int64(len(content)) {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		chunk := content[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
	defer srv.Close()

	item := &Item{
		Hash:             "concurrent-resume",
		Name:             "file.bin",
		Url:              srv.URL + "/file.bin",
		TotalSize:        ContentLength(len(content)),
		Downloaded:       ContentLength(half),
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Protocol:         ProtoHTTP,
		ResourceETag:     etag,
		Parts: map[int64]*ItemPart{
			0:    {Hash: "part0", FinalOffset: half - 1, Compiled: true},
			half: {Hash: "part1", FinalOffset: int64(len(content)) - 1},
		},
		memPart: map[string]int64{"part0": 0, "part1": half},
		mu:      m.mu,
	}
	m.UpdateItem(item)

	dlPath := filepath.Join(DlDataDir, item.Hash)
	if err := WarpMkdirAll(dlPath, PrivateDirMode); err != nil {
		t.Fatalf("MkdirAll download state directory: %v", err)
	}
	savePath := GetPath(base, item.Name)
	if err := os.WriteFile(savePath, content[:half], DefaultFileMode); err != nil {
		t.Fatalf("WriteFile compiled prefix: %v", err)
	}
	if err := os.WriteFile(getFileName(dlPath, "part1"), nil, DefaultFileMode); err != nil {
		t.Fatalf("WriteFile pending part: %v", err)
	}

	resumed, err := m.ResumeDownload(&http.Client{}, item.Hash, nil)
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}
	defer resumed.dAlloc.Close()

	// Rewrite the pending part with identical values: a real Parts map write
	// that leaves the download plan valid.
	partEnd := int64(len(content)) - 1
	mutateStop := make(chan struct{})
	mutateDone := make(chan struct{})
	mutateReady := make(chan struct{})
	go func() {
		defer close(mutateDone)
		resumed.addPart("part1", half, partEnd)
		close(mutateReady)
		for {
			select {
			case <-mutateStop:
				return
			default:
			}
			resumed.addPart("part1", half, partEnd)
		}
	}()
	<-mutateReady

	resumeDone := make(chan error, 1)
	go func() { resumeDone <- resumed.Resume() }()

	select {
	case <-started:
	case err := <-resumeDone:
		close(mutateStop)
		<-mutateDone
		close(release)
		t.Fatalf("Resume ended before any server request: %v", err)
	}
	close(mutateStop)
	<-mutateDone
	close(release)

	if err := <-resumeDone; err != nil {
		t.Fatalf("Resume: %v", err)
	}

	got, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("ReadFile resumed destination: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("resumed destination mismatch: got %d bytes, want %d", len(got), len(content))
	}
	downloaded, total := resumed.GetDownloaded(), resumed.GetTotalSize()
	if downloaded != ContentLength(len(content)) || total != ContentLength(len(content)) {
		t.Fatalf("item state after resume = %d/%d, want %d/%d",
			downloaded, total, len(content), len(content))
	}
}
