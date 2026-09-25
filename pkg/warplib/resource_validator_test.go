package warplib

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestResourceValidator(t *testing.T) {
	date := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	canonical := date.Format(http.TimeFormat)
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "strong etag", value: `"abc"`, want: `"abc"`},
		{name: "weak etag", value: `W/"abc"`, want: ""},
		{name: "http date", value: canonical, want: canonical},
		{name: "rfc850 date", value: date.Format(time.RFC850), want: canonical},
		{name: "garbage", value: "not-a-date", want: ""},
		{name: "empty", value: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resourceValidator(tt.value); got != tt.want {
				t.Fatalf("resourceValidator(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestStrongLastModified(t *testing.T) {
	lastModified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	header := func(lm, date time.Time) http.Header {
		h := http.Header{}
		if !lm.IsZero() {
			h.Set("Last-Modified", lm.Format(http.TimeFormat))
		}
		if !date.IsZero() {
			h.Set("Date", date.Format(http.TimeFormat))
		}
		return h
	}
	tests := []struct {
		name string
		h    http.Header
		want string
	}{
		{name: "old enough", h: header(lastModified, lastModified.Add(time.Hour)), want: lastModified.Format(http.TimeFormat)},
		{name: "exactly one second", h: header(lastModified, lastModified.Add(time.Second)), want: lastModified.Format(http.TimeFormat)},
		{name: "same second is weak", h: header(lastModified, lastModified)},
		{name: "missing date is weak", h: header(lastModified, time.Time{})},
		{name: "missing last-modified", h: header(time.Time{}, lastModified)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strongLastModified(tt.h); got != tt.want {
				t.Fatalf("strongLastModified = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResponseValidator(t *testing.T) {
	lm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	lmValue := lm.Format(http.TimeFormat)
	h := http.Header{}
	h.Set("Last-Modified", lmValue)
	h.Set("Date", lm.Add(time.Hour).Format(http.TimeFormat))

	if got := responseValidator(h, ""); got != lmValue {
		t.Fatalf("no ETag: got %q, want Last-Modified %q", got, lmValue)
	}
	h.Set("ETag", `"v1"`)
	if got := responseValidator(h, ""); got != `"v1"` {
		t.Fatalf("strong ETag preferred: got %q", got)
	}
	if got := responseValidator(h, lmValue); got != lmValue {
		t.Fatalf("bound date validator keeps its kind: got %q", got)
	}
	h.Del("ETag")
	if got := responseValidator(h, `"v1"`); got != "" {
		t.Fatalf("bound ETag without response ETag: got %q, want empty", got)
	}
}

func TestValidateResourceIdentityLastModified(t *testing.T) {
	lm := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Format(http.TimeFormat)
	other := time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC).Format(http.TimeFormat)
	response := func(status int, lastModified string) *http.Response {
		resp := &http.Response{StatusCode: status, Header: http.Header{}}
		if lastModified != "" {
			resp.Header.Set("Last-Modified", lastModified)
		}
		return resp
	}
	if err := validateResourceIdentity(response(http.StatusPartialContent, lm), lm); err != nil {
		t.Fatalf("matching Last-Modified rejected: %v", err)
	}
	for name, resp := range map[string]*http.Response{
		"changed":          response(http.StatusPartialContent, other),
		"missing on 206":   response(http.StatusPartialContent, ""),
		"if-range refused": response(http.StatusOK, lm),
	} {
		if err := validateResourceIdentity(resp, lm); !errors.Is(err, ErrResourceChanged) {
			t.Fatalf("%s: err = %v, want ErrResourceChanged", name, err)
		}
	}
}

// lastModifiedServer serves content with ranges and a Last-Modified
// validator but no ETag, honoring date-based If-Range like nginx does.
func lastModifiedServer(t *testing.T, content []byte, lastModified *atomic.Value, rangeRequests *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lm := lastModified.Load().(string)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Last-Modified", lm)
		rangeHeader := r.Header.Get("Range")
		ifRange := r.Header.Get("If-Range")
		if rangeHeader == "" || (ifRange != "" && ifRange != lm) {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}
		rangeRequests.Add(1)
		start, end := parseTestRange(t, rangeHeader, len(content))
		chunk := content[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloaderUsesLastModifiedForSegmentsWithoutETag(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := make([]byte, 4*MB)
	for i := range content {
		content[i] = byte(i * 7)
	}
	var lastModified atomic.Value
	lastModified.Store(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	var rangeRequests atomic.Int32
	srv := lastModifiedServer(t, content, &lastModified, &rangeRequests)

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		NumBaseParts:      4,
		MaxConnections:    4,
		MaxSegments:       4,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	defer func() { _ = d.Close() }()
	if !d.resumable {
		t.Fatal("download with a strong Last-Modified validator was not resumable")
	}
	if d.resourceETag != lastModified.Load().(string) {
		t.Fatalf("validator = %q, want Last-Modified %q", d.resourceETag, lastModified.Load())
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := rangeRequests.Load(); got < 4 {
		t.Fatalf("range requests = %d, want at least one per base part", got)
	}
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("segmented output mismatch")
	}
}

func TestDownloaderRejectsChangedLastModified(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("lm"), 64*1024)
	var lastModified atomic.Value
	lastModified.Store(time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	var rangeRequests atomic.Int32
	srv := lastModifiedServer(t, content, &lastModified, &rangeRequests)

	d, err := NewDownloader(srv.Client(), srv.URL+"/changing.bin", &DownloaderOpts{
		DownloadDirectory:   base,
		NumBaseParts:        2,
		MaxConnections:      2,
		MaxSegments:         2,
		DisableWorkStealing: true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	defer func() { _ = d.Close() }()
	lastModified.Store(time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat))
	if err := d.Start(); !errors.Is(err, ErrResourceChanged) {
		t.Fatalf("Start error = %v, want ErrResourceChanged", err)
	}
}
