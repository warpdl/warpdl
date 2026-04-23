package warplib

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewDownloaderRejectsForbidden exercises the regression that prompted
// this guard: Google Drive API returned 403 JSON ("Drive API not enabled"),
// warpdl treated it as a 2KB file, and saved the error body to disk under
// a URL-derived filename. After the fix, fetchInfo must surface the status
// and body snippet as an *HTTPStatusError and must not create any file.
func TestNewDownloaderRejectsForbidden(t *testing.T) {
	const errBody = `{"error":{"code":403,"message":"Google Drive API has not been used in project 12345 before or it is disabled."}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(errBody))
	}))
	defer srv.Close()

	dir := t.TempDir()
	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: dir,
		FileName:          "should-not-appear.bin",
	})
	if d != nil {
		defer d.Close()
	}
	if err == nil {
		t.Fatal("expected error from fetchInfo for 403 response, got nil")
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPStatusError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want 403", httpErr.StatusCode)
	}
	if !strings.Contains(httpErr.Snippet, "Drive API has not been used") {
		t.Errorf("Snippet missing server message: %q", httpErr.Snippet)
	}

	// Most important assertion: nothing was saved to disk.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("fetchInfo leaked file into download dir: %s", filepath.Join(dir, e.Name()))
	}
}

// A 4xx body longer than the peek limit must be truncated (and marked
// with an ellipsis) so the error message stays bounded even when the
// server streams a giant HTML error page.
func TestHTTPStatusErrorTruncatesLongBody(t *testing.T) {
	huge := strings.Repeat("x", httpErrorBodyPeek*4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(huge))
	}))
	defer srv.Close()

	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: t.TempDir(),
		FileName:          "x",
	})
	if d != nil {
		defer d.Close()
	}
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPStatusError, got %T", err)
	}
	// Snippet must be capped: original body + ellipsis byte.
	if ln := len(httpErr.Snippet); ln > httpErrorBodyPeek+10 {
		t.Errorf("snippet too long: %d bytes", ln)
	}
	if !strings.HasSuffix(httpErr.Snippet, "…") {
		t.Errorf("expected ellipsis to mark truncation, got tail %q", tail(httpErr.Snippet, 8))
	}
}

// An empty 4xx body must still produce a useful error.
func TestHTTPStatusErrorEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: t.TempDir(),
		FileName:          "x",
	})
	if d != nil {
		defer d.Close()
	}
	if err == nil {
		t.Fatal("expected error for 401")
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPStatusError, got %T", err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", httpErr.StatusCode)
	}
	if !strings.Contains(httpErr.Error(), "401") {
		t.Errorf("error message missing status: %q", httpErr.Error())
	}
}

// Regression guard: 2xx responses must behave exactly as before — no
// early return, normal download path.
func TestNewDownloaderAcceptsSuccessStatus(t *testing.T) {
	body := []byte("hello")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.Header().Set("Content-Disposition", `attachment; filename="hi.txt"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("200 response should not error: %v", err)
	}
	defer d.Close()
	if d.GetFileName() != "hi.txt" {
		t.Errorf("filename = %q, want hi.txt", d.GetFileName())
	}
}

// 3xx redirects are followed by net/http; fetchInfo only sees the final
// response. If that final response is 4xx the guard must still fire.
func TestNewDownloaderRejects4xxAfterRedirect(t *testing.T) {
	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv404.Close()
	srvRedir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv404.URL, http.StatusFound)
	}))
	defer srvRedir.Close()

	d, err := NewDownloader(srvRedir.Client(), srvRedir.URL, &DownloaderOpts{
		DownloadDirectory: t.TempDir(),
		FileName:          "x",
	})
	if d != nil {
		defer d.Close()
	}
	if err == nil {
		t.Fatal("expected 404-after-redirect to surface as error")
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPStatusError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", httpErr.StatusCode)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
