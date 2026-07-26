package warplib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloaderRejectsIgnoredRange(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("range-data"), 8192)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Deliberately advertise ranges but ignore every Range request.
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"ignored-range-version"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	var completed atomic.Bool
	var d *Downloader
	d, err := NewDownloader(srv.Client(), srv.URL+"/ignored.bin", &DownloaderOpts{
		DownloadDirectory:   base,
		NumBaseParts:        2,
		MaxConnections:      2,
		MaxSegments:         2,
		DisableWorkStealing: true,
		Handlers: &Handlers{
			// The API error handler stops the item/downloader. That stop must
			// not mask the worker error that triggered it.
			ErrorHandler: func(string, error) {
				if d != nil {
					d.Stop()
				}
			},
			DownloadCompleteHandler: func(hash string, _ int64) {
				if hash == MAIN_HASH {
					completed.Store(true)
				}
			},
		},
	})
	if err == nil {
		err = d.Start()
	}
	if !errors.Is(err, ErrResourceChanged) && !errors.Is(err, ErrInvalidRangeResponse) {
		t.Fatalf("download error = %v, want range/representation error", err)
	}
	if completed.Load() {
		t.Fatal("final completion handler called for ignored range response")
	}
}

func TestSingleByteDownloadUsesFullStream(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	var sawRange atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			sawRange.Store(true)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{'x'})
	}))
	defer srv.Close()

	d, err := NewDownloader(srv.Client(), srv.URL+"/one.bin", &DownloaderOpts{
		DownloadDirectory:   base,
		DisableWorkStealing: true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if d.resumable {
		t.Fatal("one-byte download incorrectly marked as ranged/resumable")
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if sawRange.Load() {
		t.Fatal("one-byte download sent a Range request")
	}
	data, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "x" {
		t.Fatalf("downloaded data = %q, want x", data)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close after Start: %v", err)
	}
}

func TestDownloaderRejectsTruncatedRangeWorker(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("x"), 64*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"truncated-version"`)
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			_, _ = w.Write(content)
			return
		}

		start, end := parseTestRange(t, rangeHeader, len(content))
		chunk := content[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)

		// The speed probe starts at byte 1. Let it succeed so construction is
		// not the subject of this test; truncate actual worker responses.
		if start == 1 {
			_, _ = w.Write(chunk)
			return
		}
		_, _ = w.Write(chunk[:len(chunk)/2])
	}))
	defer srv.Close()

	var completed atomic.Bool
	d, err := NewDownloader(srv.Client(), srv.URL+"/truncated.bin", &DownloaderOpts{
		DownloadDirectory:   base,
		NumBaseParts:        1,
		MaxConnections:      1,
		DisableWorkStealing: true,
		RetryConfig: &RetryConfig{
			MaxRetries:    1,
			BaseDelay:     0,
			MaxDelay:      0,
			BackoffFactor: 1,
		},
		Handlers: &Handlers{
			DownloadCompleteHandler: func(hash string, _ int64) {
				if hash == MAIN_HASH {
					completed.Store(true)
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	err = d.Start()
	if err == nil {
		t.Fatal("Start succeeded after a truncated range response")
	}
	if !errors.Is(err, ErrMaxRetriesExceeded) {
		t.Fatalf("Start error = %v, want ErrMaxRetriesExceeded", err)
	}
	if completed.Load() {
		t.Fatal("final completion handler called after truncated worker")
	}
}

func TestSegmentedDownloadRejectsChangedStrongETag(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	contentV1 := bytes.Repeat([]byte("v1"), 32*1024)
	contentV2 := bytes.Repeat([]byte("v2"), 32*1024)
	var version atomic.Int32
	version.Store(1)
	var sawIfRange atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := contentV1
		etag := `"version-1"`
		if version.Load() == 2 {
			content = contentV2
			etag = `"version-2"`
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", etag)
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			_, _ = w.Write(content)
			return
		}
		ifRange := r.Header.Get("If-Range")
		if ifRange != "" {
			sawIfRange.Store(true)
		}
		if ifRange != "" && ifRange != etag {
			// RFC If-Range mismatch: send the complete current representation.
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}
		start, end := parseTestRange(t, rangeHeader, len(content))
		chunk := content[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
	defer srv.Close()

	var completed atomic.Bool
	d, err := NewDownloader(srv.Client(), srv.URL+"/changing.bin", &DownloaderOpts{
		DownloadDirectory:   base,
		NumBaseParts:        2,
		MaxConnections:      2,
		MaxSegments:         2,
		DisableWorkStealing: true,
		Handlers: &Handlers{
			DownloadCompleteHandler: func(hash string, _ int64) {
				if hash == MAIN_HASH {
					completed.Store(true)
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	defer d.Close()
	if d.resourceETag != `"version-1"` {
		t.Fatalf("captured ETag = %q, want version-1", d.resourceETag)
	}

	version.Store(2)
	err = d.Start()
	if !errors.Is(err, ErrResourceChanged) {
		t.Fatalf("Start error = %v, want ErrResourceChanged", err)
	}
	if !sawIfRange.Load() {
		t.Fatal("segmented request omitted If-Range")
	}
	if completed.Load() {
		t.Fatal("completion fired after the remote representation changed")
	}
}

func TestDownloaderWithoutStrongETagUsesSingleFullStream(t *testing.T) {
	validators := []struct {
		name string
		etag string
	}{
		{name: "missing ETag"},
		{name: "weak ETag", etag: `W/"weak-version"`},
	}
	for _, tt := range validators {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			if err := SetConfigDir(base); err != nil {
				t.Fatalf("SetConfigDir: %v", err)
			}
			content := bytes.Repeat([]byte("full-stream"), 1024)
			mutated := append([]byte(nil), content...)
			for i := range mutated {
				mutated[i] ^= 0xff
			}
			var requests atomic.Int32
			var rangeRequests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request := requests.Add(1)
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set("Content-Type", "application/octet-stream")
				if tt.etag != "" {
					w.Header().Set("ETag", tt.etag)
				}
				if r.Header.Get("Range") != "" {
					rangeRequests.Add(1)
				}
				w.Header().Set("Content-Length", strconv.Itoa(len(content)))
				w.WriteHeader(http.StatusOK)
				if request == 1 {
					_, _ = w.Write(content)
				} else {
					// A second request deliberately serves a different
					// same-sized representation. Correct implementations
					// consume the metadata response body and never see it.
					_, _ = w.Write(mutated)
				}
			}))
			defer srv.Close()

			d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
				DownloadDirectory: base,
				NumBaseParts:      4,
				MaxConnections:    4,
				MaxSegments:       4,
				ForceParts:        true,
			})
			if err != nil {
				t.Fatalf("NewDownloader: %v", err)
			}
			defer d.Close()
			if d.resumable {
				t.Fatal("validator-less download was marked resumable")
			}
			if d.numBaseParts != 1 {
				t.Fatalf("base parts = %d, want 1", d.numBaseParts)
			}
			if err := d.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if got := rangeRequests.Load(); got != 0 {
				t.Fatalf("sent %d range requests without a strong validator", got)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("made %d requests without a strong validator, want exactly 1", got)
			}
			got, err := os.ReadFile(d.GetSavePath())
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			if !bytes.Equal(got, content) {
				t.Fatal("full-stream output mismatch")
			}
		})
	}
}

func TestLegacyPartialResumeWithoutStrongETagFailsBeforeRequest(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const (
		hash     = "legacy-no-validator"
		partHash = "part-a"
	)
	stateDir := filepath.Join(DlDataDir, hash)
	if err := WarpMkdirAll(stateDir, PrivateDirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(getFileName(stateDir, partHash), []byte("12345"), PrivateFileMode); err != nil {
		t.Fatalf("WriteFile part: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "file.bin"), nil, PrivateFileMode); err != nil {
		t.Fatalf("WriteFile destination: %v", err)
	}
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("network request should not be made")
	})}
	var compiled atomic.Bool
	d, err := initDownloader(client, hash, "http://example.test/file.bin", 10, &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "file.bin",
		Handlers: &Handlers{
			CompileCompleteHandler: func(string, int64) {
				compiled.Store(true)
			},
		},
	})
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer d.Close()

	err = d.Resume(map[int64]*ItemPart{
		0: {Hash: partHash, FinalOffset: 9},
	})
	if !errors.Is(err, ErrResourceChanged) {
		t.Fatalf("Resume error = %v, want ErrResourceChanged", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("resume made %d network requests without a strong validator", got)
	}
	if compiled.Load() {
		t.Fatal("partial legacy part was marked compiled")
	}
}

func TestResumeRejectsOversizedPersistedPartBeforeCompile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const (
		hash     = "oversized-resume"
		partHash = "part-a"
	)
	stateDir := filepath.Join(DlDataDir, hash)
	if err := WarpMkdirAll(stateDir, PrivateDirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(getFileName(stateDir, partHash), []byte("eleven-byte"), PrivateFileMode); err != nil {
		t.Fatalf("WriteFile part: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "file.bin"), nil, PrivateFileMode); err != nil {
		t.Fatalf("WriteFile destination: %v", err)
	}

	var compiled atomic.Bool
	d, err := initDownloader(&http.Client{}, hash, "http://example.test/file.bin", 10, &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "file.bin",
		Handlers: &Handlers{
			CompileCompleteHandler: func(string, int64) {
				compiled.Store(true)
			},
		},
	})
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer d.Close()

	err = d.Resume(map[int64]*ItemPart{
		0: {Hash: partHash, FinalOffset: 9},
	})
	if !errors.Is(err, ErrDownloadSizeMismatch) {
		t.Fatalf("Resume error = %v, want ErrDownloadSizeMismatch", err)
	}
	if compiled.Load() {
		t.Fatal("oversized persisted part was marked compiled")
	}
	data, err := os.ReadFile(filepath.Join(base, "file.bin"))
	if err != nil {
		t.Fatalf("ReadFile destination: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("oversized part wrote %d bytes to destination", len(data))
	}
}

func TestStartRejectsOversizedPhysicalDestinationBeforeCompletion(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := []byte("ten-bytes!")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	var completed atomic.Bool
	injected := make(chan error, 1)
	d, err := NewDownloader(srv.Client(), srv.URL+"/physical-size.bin", &DownloaderOpts{
		DownloadDirectory:   base,
		FileName:            "physical-size.bin",
		NumBaseParts:        1,
		MaxConnections:      1,
		MaxSegments:         1,
		DisableWorkStealing: true,
		Handlers: &Handlers{
			CompileCompleteHandler: func(string, int64) {
				f, openErr := os.OpenFile(
					filepath.Join(base, "physical-size.bin"),
					os.O_WRONLY|os.O_APPEND,
					0,
				)
				if openErr == nil {
					_, writeErr := f.Write([]byte("corrupt-tail"))
					openErr = errors.Join(writeErr, f.Close())
				}
				injected <- openErr
			},
			DownloadCompleteHandler: func(hash string, _ int64) {
				if hash == MAIN_HASH {
					completed.Store(true)
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	defer d.Close()

	err = d.Start()
	select {
	case hookErr := <-injected:
		if hookErr != nil {
			t.Fatalf("inject corrupt tail: %v", hookErr)
		}
	case <-time.After(time.Second):
		t.Fatal("compile hook did not inject corrupt tail")
	}
	if !errors.Is(err, ErrDownloadSizeMismatch) {
		t.Fatalf("Start error = %v, want ErrDownloadSizeMismatch", err)
	}
	if completed.Load() {
		t.Fatal("final completion handler called for oversized physical destination")
	}
}

func TestResumeRejectsOversizedPhysicalDestinationBeforeCompletion(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const (
		hash     = "resume-physical-size"
		partHash = "complete-part"
	)
	content := []byte("ten-bytes!")
	stateDir := filepath.Join(DlDataDir, hash)
	if err := WarpMkdirAll(stateDir, PrivateDirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(getFileName(stateDir, partHash), content, PrivateFileMode); err != nil {
		t.Fatalf("WriteFile part: %v", err)
	}
	destination := filepath.Join(base, "resume-physical-size.bin")
	if err := os.WriteFile(destination, nil, PrivateFileMode); err != nil {
		t.Fatalf("WriteFile destination: %v", err)
	}

	var completed atomic.Bool
	injected := make(chan error, 1)
	d, err := initDownloader(
		&http.Client{},
		hash,
		"http://example.test/resume-physical-size.bin",
		ContentLength(len(content)),
		&DownloaderOpts{
			DownloadDirectory: base,
			FileName:          "resume-physical-size.bin",
			Handlers: &Handlers{
				CompileCompleteHandler: func(string, int64) {
					f, openErr := os.OpenFile(destination, os.O_WRONLY|os.O_APPEND, 0)
					if openErr == nil {
						_, writeErr := f.Write([]byte("corrupt-tail"))
						openErr = errors.Join(writeErr, f.Close())
					}
					injected <- openErr
				},
				DownloadCompleteHandler: func(workerHash string, _ int64) {
					if workerHash == MAIN_HASH {
						completed.Store(true)
					}
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer d.Close()

	err = d.Resume(map[int64]*ItemPart{
		0: {Hash: partHash, FinalOffset: int64(len(content) - 1)},
	})
	select {
	case hookErr := <-injected:
		if hookErr != nil {
			t.Fatalf("inject corrupt tail: %v", hookErr)
		}
	case <-time.After(time.Second):
		t.Fatal("compile hook did not inject corrupt tail")
	}
	if !errors.Is(err, ErrDownloadSizeMismatch) {
		t.Fatalf("Resume error = %v, want ErrDownloadSizeMismatch", err)
	}
	if completed.Load() {
		t.Fatal("final completion handler called for oversized resumed destination")
	}
}

func TestPluginHeaderProvenanceRedactsDownloadLog(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const secret = "plugin-debug-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "1")
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		Headers: Headers{
			{Key: "X-Key", Value: "caller-custom-secret"},
		},
		PluginHeaders: Headers{
			{Key: "X-Plugin-Debug", Value: secret},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	resp, err := d.makeRequest(http.MethodGet)
	if err != nil {
		t.Fatalf("makeRequest: %v", err)
	}
	_ = resp.Body.Close()
	if err := d.closeLogWriter(); err != nil {
		t.Fatalf("closeLogWriter: %v", err)
	}
	logBytes, err := os.ReadFile(filepath.Join(d.dlPath, "logs.txt"))
	if err != nil {
		t.Fatalf("ReadFile log: %v", err)
	}
	for _, value := range []string{secret, "caller-custom-secret"} {
		if strings.Contains(string(logBytes), value) {
			t.Fatalf("custom header value %q leaked into log:\n%s", value, logBytes)
		}
	}
	if !strings.Contains(string(logBytes), "X-Plugin-Debug: [REDACTED]") {
		t.Fatalf("plugin header was not logged as redacted:\n%s", logBytes)
	}
	if !strings.Contains(string(logBytes), "X-Key: [REDACTED]") {
		t.Fatalf("caller custom header was not logged as redacted:\n%s", logBytes)
	}
}

func TestDownloaderRejectsNegativeConnectionAndSegmentLimits(t *testing.T) {
	tests := []struct {
		name string
		opts DownloaderOpts
		want error
	}{
		{
			name: "negative connections",
			opts: DownloaderOpts{MaxConnections: -1},
			want: ErrInvalidMaxConnections,
		},
		{
			name: "negative segments",
			opts: DownloaderOpts{MaxSegments: -1},
			want: ErrInvalidMaxSegments,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewDownloader(nil, "http://example.test/file.bin", &tt.opts); !errors.Is(err, tt.want) {
				t.Fatalf("NewDownloader error = %v, want %v", err, tt.want)
			}
			if _, err := initDownloader(nil, "hash", "http://example.test/file.bin", 10, &tt.opts); !errors.Is(err, tt.want) {
				t.Fatalf("initDownloader error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNonRangeRetryRollsBackDiscardedProgress(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("n"), 4096)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		request := requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		if request == 1 {
			// The metadata response is also the first transfer attempt.
			// Truncate it so the retry must discard progress and restart.
			_, _ = w.Write(content[:len(content)/2])
			return
		}
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	var progress atomic.Int64
	d, err := NewDownloader(srv.Client(), srv.URL+"/non-range.bin", &DownloaderOpts{
		DownloadDirectory: base,
		NumBaseParts:      1,
		MaxConnections:    1,
		RetryConfig: &RetryConfig{
			MaxRetries:    2,
			BaseDelay:     0,
			MaxDelay:      0,
			BackoffFactor: 1,
		},
		Handlers: &Handlers{
			DownloadProgressHandler: func(_ string, n int) {
				progress.Add(int64(n))
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := progress.Load(); got != int64(len(content)) {
		t.Fatalf("net progress = %d, want %d after retry rollback", got, len(content))
	}
}

func TestNonRangeRetryRevisitsStableSourceAfterRedirect(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	firstContent := bytes.Repeat([]byte("first-version"), 512)
	retryContent := bytes.Repeat([]byte("retry-version"), 512)
	if len(firstContent) != len(retryContent) {
		t.Fatal("test representations must have equal sizes")
	}

	var (
		sourceHits      atomic.Int32
		credentialLeaks atomic.Int32
		generationsMu   sync.Mutex
		generations     []string
	)
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			credentialLeaks.Add(1)
		}
		generation := r.URL.Query().Get("generation")
		generationsMu.Lock()
		generations = append(generations, generation)
		generationsMu.Unlock()

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(firstContent)))
		w.WriteHeader(http.StatusOK)
		if generation == "1" {
			_, _ = w.Write(firstContent[:1])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			// The retained metadata response stalls mid-body. RequestTimeout
			// must cancel it and retry through the stable source URL.
			<-r.Context().Done()
			return
		}
		_, _ = w.Write(retryContent)
	}))
	defer final.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer source-only" {
			http.Error(w, "missing source authorization", http.StatusUnauthorized)
			return
		}
		generation := sourceHits.Add(1)
		http.Redirect(w, r,
			fmt.Sprintf("%s/asset.bin?generation=%d&signature=ephemeral", final.URL, generation),
			http.StatusFound)
	}))
	defer source.Close()

	d, err := NewDownloader(source.Client(), source.URL+"/stable", &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "asset.bin",
		Headers: Headers{
			{Key: "Authorization", Value: "Bearer source-only"},
		},
		RequestTimeout: 100 * time.Millisecond,
		RetryConfig: &RetryConfig{
			MaxRetries:    2,
			BaseDelay:     0,
			MaxDelay:      0,
			BackoffFactor: 1,
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	defer d.Close()
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := sourceHits.Load(); got != 2 {
		t.Fatalf("source requests = %d, want metadata request plus retry", got)
	}
	if got := credentialLeaks.Load(); got != 0 {
		t.Fatalf("source Authorization leaked to redirect target %d times", got)
	}
	generationsMu.Lock()
	gotGenerations := append([]string(nil), generations...)
	generationsMu.Unlock()
	if fmt.Sprint(gotGenerations) != "[1 2]" {
		t.Fatalf("redirect generations = %v, want fresh target generations [1 2]", gotGenerations)
	}
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, retryContent) {
		t.Fatal("retry output did not replace the truncated first representation")
	}
}

type failAfterDataReadCloser struct {
	data []byte
	err  error
	done bool
}

func (r *failAfterDataReadCloser) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	if !r.done {
		r.done = true
		return 0, r.err
	}
	return 0, io.EOF
}

func (*failAfterDataReadCloser) Close() error { return nil }

type closeTrackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *closeTrackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestValidateResourceIdentityRequiresMatchingStrongETagOnPartialResponse(t *testing.T) {
	const expected = `"version-1"`
	tests := []struct {
		name       string
		statusCode int
		etag       string
		wantErr    bool
	}{
		{name: "matching", statusCode: http.StatusPartialContent, etag: expected},
		{name: "missing", statusCode: http.StatusPartialContent, wantErr: true},
		{name: "weak", statusCode: http.StatusPartialContent, etag: `W/"version-1"`, wantErr: true},
		{name: "different", statusCode: http.StatusPartialContent, etag: `"version-2"`, wantErr: true},
		{name: "if range rejected", statusCode: http.StatusOK, etag: expected, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Header:     make(http.Header),
			}
			if tt.etag != "" {
				resp.Header.Set("ETag", tt.etag)
			}
			err := validateResourceIdentity(resp, expected)
			if tt.wantErr && !errors.Is(err, ErrResourceChanged) {
				t.Fatalf("validateResourceIdentity error = %v, want ErrResourceChanged", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateResourceIdentity: %v", err)
			}
		})
	}
}

func TestDownloaderCloseReleasesRetainedInitialBody(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	body := &closeTrackingBody{Reader: strings.NewReader("hello")}
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          body,
			ContentLength: 5,
			Request:       req,
		}, nil
	})}
	d, err := NewDownloader(client, "http://example.test/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "file.bin",
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if body.closed.Load() {
		t.Fatal("validator-less metadata body closed before transfer or Downloader.Close")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !body.closed.Load() {
		t.Fatal("Downloader.Close did not release retained metadata body")
	}
}

func TestStopCancelsStalledRetainedInitialBody(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	progress := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("x"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	var stoppedCalls, errorCalls, completeCalls atomic.Int32
	d, err := NewDownloader(srv.Client(), srv.URL+"/stalled.bin", &DownloaderOpts{
		DownloadDirectory: base,
		Handlers: &Handlers{
			DownloadProgressHandler: func(_ string, _ int) {
				select {
				case <-progress:
				default:
					close(progress)
				}
			},
			ErrorHandler: func(string, error) {
				errorCalls.Add(1)
			},
			DownloadStoppedHandler: func() {
				stoppedCalls.Add(1)
			},
			DownloadCompleteHandler: func(string, int64) {
				completeCalls.Add(1)
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	defer d.Close()

	startDone := make(chan error, 1)
	go func() {
		startDone <- d.Start()
	}()
	select {
	case <-progress:
	case <-time.After(2 * time.Second):
		t.Fatal("retained response did not begin streaming")
	}

	d.Stop()
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("Start after Stop = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not unblock stalled retained response")
	}
	if got := stoppedCalls.Load(); got != 1 {
		t.Fatalf("DownloadStopped calls = %d, want 1", got)
	}
	if got := errorCalls.Load(); got != 0 {
		t.Fatalf("ErrorHandler calls = %d, want 0 for intentional Stop", got)
	}
	if got := completeCalls.Load(); got != 0 {
		t.Fatalf("DownloadComplete calls = %d, want 0", got)
	}
}

func TestFinishWorkersDoesNotMaskGenuineErrorJoinedWithCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	genuineErr := errors.New("genuine segment failure")
	var stoppedCalls atomic.Int32
	d := &Downloader{
		ctx:     ctx,
		stopped: 1,
		handlers: &Handlers{
			DownloadStoppedHandler: func() { stoppedCalls.Add(1) },
		},
		workerErrs: []error{errors.Join(
			fmt.Errorf("cancelled worker: %w", context.Canceled),
			genuineErr,
		)},
		l: log.New(io.Discard, "", 0),
	}
	terminal, err := d.finishWorkers()
	if !terminal {
		t.Fatal("finishWorkers did not report a terminal result")
	}
	if !errors.Is(err, genuineErr) {
		t.Fatalf("finishWorkers error = %v, want genuine worker failure", err)
	}
	if got := stoppedCalls.Load(); got != 0 {
		t.Fatalf("DownloadStopped calls = %d, want 0 for genuine failure", got)
	}
}

func TestPrepareDownloaderClosesProbeBodyOnEarlyReturn(t *testing.T) {
	body := &closeTrackingBody{Reader: strings.NewReader("probe")}
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header), // no Accept-Ranges: early return
			Body:          body,
			ContentLength: 5,
			Request:       req,
		}, nil
	})}
	d := &Downloader{
		client:        client,
		url:           "http://example.test/file.bin",
		chunk:         int(DEF_CHUNK_SIZE),
		contentLength: ContentLength(1024),
		resourceETag:  `"probe-close-test"`,
	}
	if err := d.prepareDownloader(); err != nil {
		t.Fatalf("prepareDownloader: %v", err)
	}
	if !body.closed.Load() {
		t.Fatal("probe response body was not closed")
	}
}

func TestDownloaderLogRedactsURLCredentialsAndQuery(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("x"), 64*1024)
	srv := newRangeServer(t, content)
	defer srv.Close()

	downloadURL, err := url.Parse(srv.URL + "/file.bin?token=query-secret&signature=also-secret#fragment")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	downloadURL.User = url.UserPassword("log-user", "userinfo-secret")

	d, err := NewDownloader(srv.Client(), downloadURL.String(), &DownloaderOpts{
		DownloadDirectory: base,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	logPath := filepath.Join(DlDataDir, d.GetHash(), "logs.txt")
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	logText := string(logData)
	for _, secret := range []string{
		"log-user",
		"userinfo-secret",
		"query-secret",
		"also-secret",
		"token=",
		"signature=",
		"fragment",
	} {
		if strings.Contains(logText, secret) {
			t.Fatalf("download log leaked %q:\n%s", secret, logText)
		}
	}
	if !strings.Contains(logText, "GET: "+srv.URL+"/file.bin") {
		t.Fatalf("download log omitted safe URL identity:\n%s", logText)
	}
}

func TestDownloaderPropagatesUnknownSizeWorkerError(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	readErr := errors.New("stream failed")
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        make(http.Header),
			Body:          &failAfterDataReadCloser{data: []byte("partial"), err: readErr},
			ContentLength: -1,
			Request:       req,
		}, nil
	})}

	var completed, stopped atomic.Bool
	var errorCalls atomic.Int32
	var d *Downloader
	d, err := NewDownloader(client, "http://example.test/unknown.bin", &DownloaderOpts{
		DownloadDirectory: base,
		Handlers: &Handlers{
			ErrorHandler: func(string, error) {
				errorCalls.Add(1)
				d.Stop()
			},
			DownloadCompleteHandler: func(hash string, _ int64) {
				if hash == MAIN_HASH {
					completed.Store(true)
				}
			},
			DownloadStoppedHandler: func() {
				stopped.Store(true)
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	err = d.Start()
	if !errors.Is(err, readErr) {
		t.Fatalf("Start error = %v, want wrapped stream error", err)
	}
	if completed.Load() {
		t.Fatal("final completion handler called after unknown-size stream error")
	}
	if got := errorCalls.Load(); got != 1 {
		t.Fatalf("ErrorHandler calls = %d, want 1", got)
	}
	if stopped.Load() {
		t.Fatal("genuine worker error was masked as an intentional stop")
	}
}

func TestInitDownloaderRestoresRuntimeOptions(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const hash = "restored"
	if err := WarpMkdirAll(filepath.Join(DlDataDir, hash), PrivateDirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	headers := Headers{{Key: "Authorization", Value: "Bearer restored"}}
	client := &http.Client{}
	d, err := initDownloader(
		client,
		hash,
		"http://example.test/file.bin",
		ContentLength(1024),
		&DownloaderOpts{
			FileName:          "file.bin",
			DownloadDirectory: base,
			NumBaseParts:      3,
			MaxConnections:    4,
			MaxSegments:       4,
			Headers:           headers,
			PluginHeaderNames: []string{"Authorization"},
			ResourceETag:      `"restored-version"`,
			LockFileName:      true,
		},
		withResumable(false),
	)
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer d.Close()

	if d.numBaseParts != 1 {
		t.Fatalf("numBaseParts = %d, want 1 for non-resumable transfer", d.numBaseParts)
	}
	if d.resumable {
		t.Fatal("persisted non-range capability was replaced by known content length")
	}
	if !d.enableWorkStealing {
		t.Fatal("work stealing default was not restored")
	}
	if !d.lockFileName {
		t.Fatal("LockFileName was not restored")
	}
	if i, ok := d.headers.Get("Authorization"); !ok || d.headers[i].Value != "Bearer restored" {
		t.Fatalf("restored headers = %#v", d.headers)
	}
	if d.client.CheckRedirect == nil {
		t.Fatal("redirect policy was not installed")
	}
	if client.CheckRedirect != nil {
		t.Fatal("initDownloader mutated caller-owned http.Client")
	}
	if d.resourceETag != `"restored-version"` {
		t.Fatalf("resource ETag = %q, want restored-version", d.resourceETag)
	}
	if _, ok := d.pluginHeaderNames["Authorization"]; !ok {
		t.Fatalf("plugin header provenance = %#v", d.pluginHeaderNames)
	}
}

func TestResumePropagatesMissingSegmentWorkerError(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	const hash = "resume-missing"
	if err := WarpMkdirAll(filepath.Join(DlDataDir, hash), PrivateDirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	var completed atomic.Bool
	d, err := initDownloader(
		&http.Client{},
		hash,
		"http://example.test/file.bin",
		ContentLength(10),
		&DownloaderOpts{
			FileName:          "file.bin",
			DownloadDirectory: base,
			Handlers: &Handlers{
				DownloadCompleteHandler: func(workerHash string, _ int64) {
					if workerHash == MAIN_HASH {
						completed.Store(true)
					}
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer d.Close()

	err = d.Resume(map[int64]*ItemPart{
		0: {Hash: "missing-part-file", FinalOffset: 9},
	})
	if err == nil {
		t.Fatal("Resume succeeded despite a missing segment file")
	}
	if completed.Load() {
		t.Fatal("final completion handler called after resume worker failure")
	}
}

func TestResumeRejectsIncompleteOrOverlappingPersistedPartition(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	tests := []struct {
		name  string
		parts map[int64]*ItemPart
	}{
		{
			name: "overlap with compensating tail gap",
			// The old byte-sum check saw 6+4 == 10 and reported success,
			// despite bytes 4-5 being counted twice and 8-9 not covered.
			parts: map[int64]*ItemPart{
				0: {Hash: "part-a", FinalOffset: 5, Compiled: true},
				4: {Hash: "part-b", FinalOffset: 7, Compiled: true},
			},
		},
		{
			name: "shortened parent persisted before missing child",
			parts: map[int64]*ItemPart{
				0: {Hash: "parent", FinalOffset: 4, Compiled: true},
			},
		},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := fmt.Sprintf("invalid-partition-%d", index)
			if err := WarpMkdirAll(filepath.Join(DlDataDir, hash), PrivateDirMode); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			var completed atomic.Bool
			d, err := initDownloader(
				&http.Client{},
				hash,
				"http://example.test/file.bin",
				ContentLength(10),
				&DownloaderOpts{
					FileName:          "file.bin",
					DownloadDirectory: base,
					Handlers: &Handlers{
						DownloadCompleteHandler: func(workerHash string, _ int64) {
							if workerHash == MAIN_HASH {
								completed.Store(true)
							}
						},
					},
				},
			)
			if err != nil {
				t.Fatalf("initDownloader: %v", err)
			}
			defer d.Close()

			err = d.Resume(tt.parts)
			if !errors.Is(err, ErrItemPartInvalidRange) {
				t.Fatalf("Resume error = %v, want ErrItemPartInvalidRange", err)
			}
			if completed.Load() {
				t.Fatal("final completion handler called for invalid persisted partition")
			}
		})
	}
}

func TestValidateResumePartCoverage(t *testing.T) {
	valid := map[int64]*ItemPart{
		0: {Hash: "single-byte", FinalOffset: 0},
		1: {Hash: "remaining", FinalOffset: 9},
	}
	if err := validateResumePartCoverage(valid, 10); err != nil {
		t.Fatalf("valid partition rejected: %v", err)
	}

	tests := []struct {
		name  string
		parts map[int64]*ItemPart
	}{
		{"first offset nonzero", map[int64]*ItemPart{1: {Hash: "a", FinalOffset: 9}}},
		{"negative start", map[int64]*ItemPart{-1: {Hash: "a", FinalOffset: 9}}},
		{"end past total", map[int64]*ItemPart{0: {Hash: "a", FinalOffset: 10}}},
		{"empty hash", map[int64]*ItemPart{0: {Hash: "", FinalOffset: 9}}},
		{
			"duplicate hash",
			map[int64]*ItemPart{
				0: {Hash: "same", FinalOffset: 4},
				5: {Hash: "same", FinalOffset: 9},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateResumePartCoverage(tt.parts, 10); !errors.Is(err, ErrItemPartInvalidRange) {
				t.Fatalf("error = %v, want ErrItemPartInvalidRange", err)
			}
		})
	}
}

func TestValidateRangeResponse(t *testing.T) {
	response := func(status int, contentRange string, length int64) *http.Response {
		h := make(http.Header)
		if contentRange != "" {
			h.Set("Content-Range", contentRange)
		}
		return &http.Response{
			StatusCode:    status,
			Status:        http.StatusText(status),
			Header:        h,
			ContentLength: length,
		}
	}

	if err := validateRangeResponse(
		response(http.StatusPartialContent, "bytes 10-19/100", 10),
		10,
		19,
		100,
	); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}

	tests := []struct {
		name string
		resp *http.Response
	}{
		{"status 200", response(http.StatusOK, "", 100)},
		{"missing header", response(http.StatusPartialContent, "", 10)},
		{"wrong start", response(http.StatusPartialContent, "bytes 0-19/100", 20)},
		{"wrong end", response(http.StatusPartialContent, "bytes 10-20/100", 11)},
		{"wrong total", response(http.StatusPartialContent, "bytes 10-19/101", 10)},
		{"wrong length", response(http.StatusPartialContent, "bytes 10-19/100", 9)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRangeResponse(tt.resp, 10, 19, 100)
			if !errors.Is(err, ErrInvalidRangeResponse) {
				t.Fatalf("error = %v, want ErrInvalidRangeResponse", err)
			}
		})
	}
}

func TestCopyBufferHonorsReducedWorkStealBoundary(t *testing.T) {
	partFile, err := os.Create(filepath.Join(t.TempDir(), "part.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer partFile.Close()

	finalOffset := new(atomic.Int64)
	finalOffset.Store(11)
	var progressCalls atomic.Int32
	p := &Part{
		pf:     partFile,
		hash:   "victim",
		chunk:  4,
		offset: 0,
		l:      log.New(io.Discard, "", 0),
		pfunc: func(string, int) {
			if progressCalls.Add(1) == 1 {
				finalOffset.Store(3)
			}
		},
		ofunc: func(string, int64) {},
	}

	if _, err := p.copyBufferTo(
		io.NopCloser(bytes.NewReader([]byte("abcdefghijkl"))),
		finalOffset,
		true,
	); err != nil {
		t.Fatalf("copyBufferTo: %v", err)
	}
	if got := p.getRead(); got != 4 {
		t.Fatalf("victim read %d bytes after boundary reduced to 3, want 4", got)
	}
}

type gatedReadCloser struct {
	reader  io.Reader
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *gatedReadCloser) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.reader.Read(p)
}

func (*gatedReadCloser) Close() error { return nil }

func TestWorkStealReservationDoesNotBlockOrOverlapInFlightRead(t *testing.T) {
	const (
		chunkSize = int64(MB)
		totalSize = int64(12 * MB)
	)
	partFile, err := os.Create(filepath.Join(t.TempDir(), "part.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer partFile.Close()

	finalOffset := new(atomic.Int64)
	finalOffset.Store(totalSize - 1)
	p := &Part{
		pf:     partFile,
		hash:   "blocked-victim",
		chunk:  chunkSize,
		offset: 0,
		l:      log.New(io.Discard, "", 0),
		pfunc:  func(string, int) {},
		ofunc:  func(string, int64) {},
	}
	d := &Downloader{enableWorkStealing: true}
	d.activeParts.Make()
	d.registerActivePart(p, finalOffset)
	info := d.activeParts.Get(p.hash)
	if info == nil {
		t.Fatal("active part was not registered")
	}

	reader := &gatedReadCloser{
		reader:  bytes.NewReader(bytes.Repeat([]byte("x"), int(totalSize))),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := p.copyBufferTo(reader, finalOffset, true)
		copyDone <- copyErr
	}()
	<-reader.started // reservation is published immediately before Read

	type stealResult struct {
		start int64
		end   int64
		ok    bool
	}
	stealDone := make(chan stealResult, 1)
	go func() {
		info.mu.Lock()
		defer info.mu.Unlock()
		safePos := info.getSafeCurrentPos()
		start, end, ok := calculateStealWork(info.offset, info.foff.Load(), safePos-info.offset)
		if ok {
			info.foff.Store(start - 1)
		}
		stealDone <- stealResult{start: start, end: end, ok: ok}
	}()

	var stolen stealResult
	select {
	case stolen = <-stealDone:
		// The reservation mutex must remain available while network Read is
		// blocked. The old implementation timed out here.
	case <-time.After(time.Second):
		close(reader.release)
		<-copyDone
		t.Fatal("work steal blocked behind an in-flight network read")
	}
	close(reader.release)
	if err := <-copyDone; err != nil {
		t.Fatalf("copyBufferTo: %v", err)
	}
	if !stolen.ok {
		t.Fatal("expected the remaining range to be stealable")
	}
	if got := p.getRead(); got != stolen.start {
		t.Fatalf("victim wrote through %d, stolen range starts at %d", got-1, stolen.start)
	}
	if finalOffset.Load() != stolen.start-1 {
		t.Fatalf("victim final offset = %d, want %d", finalOffset.Load(), stolen.start-1)
	}
}

func TestSlowSplitAndWorkStealReservationsNeverOverlap(t *testing.T) {
	type byteRange struct {
		start int64
		end   int64
		ok    bool
	}
	overlaps := func(a, b byteRange) bool {
		return a.ok && b.ok && a.start <= b.end && b.start <= a.end
	}

	for iteration := 0; iteration < 200; iteration++ {
		finalOffset := new(atomic.Int64)
		finalOffset.Store(64*MB - 1)
		read := int64(0)
		reservedThrough := new(atomic.Int64)
		reservedThrough.Store(-1)
		info := &activePartInfo{
			hash:            "victim",
			offset:          0,
			foff:            finalOffset,
			read:            &read,
			reservedThrough: reservedThrough,
		}
		part := &Part{
			hash:            "victim",
			offset:          0,
			read:            read,
			boundaryMu:      &info.mu,
			reservedThrough: reservedThrough,
		}
		d := &Downloader{
			contentLength: ContentLength(64 * MB),
			handlers: &Handlers{
				RespawnPartHandler: func(string, int64, int64, int64) {},
			},
		}

		start := make(chan struct{})
		var ownerChild, stolenChild byteRange
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			childStart, childEnd, ok := d.reserveSlowPartSplit(part, finalOffset)
			ownerChild = byteRange{start: childStart, end: childEnd, ok: ok}
		}()
		go func() {
			defer wg.Done()
			<-start
			info.mu.Lock()
			defer info.mu.Unlock()
			safePos := info.getSafeCurrentPos()
			childStart, childEnd, ok := calculateStealWork(
				info.offset,
				info.foff.Load(),
				safePos-info.offset,
			)
			if ok {
				info.foff.Store(childStart - 1)
			}
			stolenChild = byteRange{start: childStart, end: childEnd, ok: ok}
		}()
		close(start)
		wg.Wait()

		if !ownerChild.ok || !stolenChild.ok {
			t.Fatalf("iteration %d: split results owner=%+v steal=%+v", iteration, ownerChild, stolenChild)
		}
		if overlaps(ownerChild, stolenChild) {
			t.Fatalf("iteration %d: overlapping children owner=%+v steal=%+v", iteration, ownerChild, stolenChild)
		}
		parent := byteRange{start: 0, end: finalOffset.Load(), ok: true}
		if overlaps(parent, ownerChild) || overlaps(parent, stolenChild) {
			t.Fatalf("iteration %d: parent=%+v overlaps owner=%+v or steal=%+v",
				iteration, parent, ownerChild, stolenChild)
		}
	}
}

func TestExplicitFilenameCannotEscapeDownloadDirectory(t *testing.T) {
	base := t.TempDir()
	invalid := []string{
		"../outside.bin",
		`..\outside.bin`,
		filepath.Join(string(filepath.Separator), "tmp", "outside.bin"),
		`C:\outside.bin`,
		"file:stream",
		"report.",
		"report ",
		"CON",
		"nul.txt",
		"PRN.pdf",
		"COM1.log",
		"LPT9",
	}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := confinedDownloadPath(base, name); !errors.Is(err, ErrUnsafeFileName) {
				t.Fatalf("confinedDownloadPath(%q) error = %v, want ErrUnsafeFileName", name, err)
			}
			if _, err := NewDownloader(&http.Client{}, "http://example.test/file.bin", &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          name,
			}); !errors.Is(err, ErrUnsafeFileName) {
				t.Fatalf("HTTP filename %q error = %v, want ErrUnsafeFileName", name, err)
			}
			if _, err := newFTPProtocolDownloader("ftp://host/file.bin", &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          name,
			}); err == nil {
				t.Fatalf("FTP accepted unsafe filename %q", name)
			}
			if _, err := newSFTPProtocolDownloader("sftp://host/file.bin", &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          name,
			}); err == nil {
				t.Fatalf("SFTP accepted unsafe filename %q", name)
			}
		})
	}
}

func TestPortableFilenameAllowsOrdinaryDevicePrefixes(t *testing.T) {
	for _, name := range []string{"console.txt", "com10.log", "lpt0", ".env", "report final.txt"} {
		if err := validateDownloadFileName(name); err != nil {
			t.Errorf("validateDownloadFileName(%q): %v", name, err)
		}
	}
}

func TestOpenFreshDownloadFileRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows hosts")
	}
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("safe"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(base, "download.bin")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if _, err := openFreshDownloadFile(link, true); !errors.Is(err, ErrUnsafeFileName) {
		t.Fatalf("openFreshDownloadFile error = %v, want ErrUnsafeFileName", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "safe" {
		t.Fatalf("symlink target modified: %q", got)
	}
}

func parseTestRange(t *testing.T, value string, total int) (int, int) {
	t.Helper()
	value = strings.TrimPrefix(value, "bytes=")
	startValue, endValue, ok := strings.Cut(value, "-")
	if !ok {
		t.Fatalf("malformed Range header %q", value)
	}
	start, err := strconv.Atoi(startValue)
	if err != nil {
		t.Fatalf("malformed range start %q: %v", startValue, err)
	}
	end := total - 1
	if endValue != "" {
		end, err = strconv.Atoi(endValue)
		if err != nil {
			t.Fatalf("malformed range end %q: %v", endValue, err)
		}
	}
	return start, end
}
