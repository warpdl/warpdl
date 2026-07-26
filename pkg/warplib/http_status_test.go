package warplib

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewDownloaderRejectsForbidden exercises the regression that prompted
// this guard: an HTTP error body must not be saved as the requested file.
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
	if httpErr.Snippet != "" {
		t.Errorf("arbitrary response body was exposed: %q", httpErr.Snippet)
	}
	if strings.Contains(err.Error(), "project 12345") {
		t.Errorf("HTTP status error leaked response body: %v", err)
	}

	// Most important assertion: nothing was saved to disk.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("fetchInfo leaked file into download dir: %s", filepath.Join(dir, e.Name()))
	}
}

// Arbitrary 4xx/5xx bodies may echo tokens and signed URLs, so they are not
// copied into public errors.
func TestHTTPStatusErrorOmitsBody(t *testing.T) {
	const secret = "signed-url-token-that-must-not-escape"
	huge := strings.Repeat(secret, 512)
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
	if httpErr.Snippet != "" {
		t.Errorf("response snippet = %q, want empty", httpErr.Snippet)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("HTTP status error leaked response body: %v", err)
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

func TestHTTPStatusErrorRedactsRequestURLSecrets(t *testing.T) {
	requestURL, err := url.Parse("https://api-user:api-password@example.test/file?token=signed-secret#private")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	httpErr := newHTTPStatusError(&http.Response{
		StatusCode: http.StatusForbidden,
		Status:     "403 Forbidden",
		Body:       io.NopCloser(strings.NewReader("denied")),
		Request:    &http.Request{URL: requestURL},
	})

	if httpErr.URL != "https://example.test/file" {
		t.Fatalf("redacted URL = %q, want host and path only", httpErr.URL)
	}
	for _, secret := range []string{"api-user", "api-password", "signed-secret", "token=", "private"} {
		if strings.Contains(httpErr.Error(), secret) {
			t.Fatalf("HTTP status error leaked %q: %s", secret, httpErr)
		}
	}
}

// Regression guard: a complete 200 response follows the normal download path.
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

func TestNewDownloaderRejectsUnsolicitedPartialResponse(t *testing.T) {
	body := []byte("first-half")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.Header().Set("Content-Range", "bytes 0-9/20")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: dir,
		FileName:          "partial.bin",
	})
	if d != nil {
		defer d.Close()
	}
	if !errors.Is(err, ErrInvalidRangeResponse) {
		t.Fatalf("error = %v, want ErrInvalidRangeResponse", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "partial.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("partial response created destination: %v", statErr)
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

func TestNewDownloaderRejectsUnfollowedRedirectResponse(t *testing.T) {
	const targetSecret = "signed-cdn-token"
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1")
		_, _ = w.Write([]byte("x"))
	}))
	defer final.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/file.bin?token="+targetSecret, http.StatusFound)
	}))
	defer source.Close()

	client := source.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	d, err := NewDownloader(client, source.URL+"/source", &DownloaderOpts{
		DownloadDirectory: t.TempDir(),
		FileName:          "file.bin",
	})
	if d != nil {
		defer d.Close()
	}
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %T %v, want HTTPStatusError", err, err)
	}
	if statusErr.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", statusErr.StatusCode, http.StatusFound)
	}
	if strings.Contains(err.Error(), targetSecret) {
		t.Fatalf("redirect response error leaked target query: %v", err)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
