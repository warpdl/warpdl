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

func TestUniquifyPath_NoCollisionReturnsUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.txt")
	got, err := uniquifyPath(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != path {
		t.Errorf("got %q, want %q (no collision => unchanged)", got, path)
	}
}

func TestUniquifyPath_FirstCollisionInsertsParen1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := uniquifyPath(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(dir, "report (1).pdf")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquifyPath_SecondCollisionAdvancesCounter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	for _, p := range []string{
		path,
		filepath.Join(dir, "report (1).pdf"),
		filepath.Join(dir, "report (2).pdf"),
	} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := uniquifyPath(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(dir, "report (3).pdf")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquifyPath_NoExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Makefile")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := uniquifyPath(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(dir, "Makefile (1)")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquifyPath_LastExtensionOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.tar.gz")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := uniquifyPath(path)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Chrome convention: only the last ".gz" is treated as extension.
	want := filepath.Join(dir, "archive.tar (1).gz")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUniquifyPath_TooManyCollisions(t *testing.T) {
	dir := t.TempDir()
	base := "swarm.bin"
	if err := os.WriteFile(filepath.Join(dir, base), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	for n := 1; n < maxUniquifyAttempts; n++ {
		f := filepath.Join(dir, "swarm ("+itoa(n)+").bin")
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := uniquifyPath(filepath.Join(dir, base))
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if !strings.Contains(err.Error(), "too many collisions") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Integration: NewDownloader without LockFileName auto-renames when the
// destination already exists. Reproduces the user's "1Ton1...identifier"
// scenario in miniature: the prior failed attempt left a stub file at
// the same path; the new download must land on `name (1).bin` instead
// of erroring with ErrFileExists.
func TestNewDownloader_AutoRenameOnCollision(t *testing.T) {
	body := []byte("hello content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "13")
		w.Header().Set("Content-Disposition", `attachment; filename="x.bin"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	orig := DlDataDir
	DlDataDir = t.TempDir()
	defer func() { DlDataDir = orig }()

	dlDir := t.TempDir()
	stub := filepath.Join(dlDir, "x.bin")
	if err := os.WriteFile(stub, []byte("stub"), 0644); err != nil {
		t.Fatal(err)
	}

	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: dlDir,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if d.GetFileName() != "x (1).bin" {
		t.Errorf("filename = %q, want %q (auto-rename)", d.GetFileName(), "x (1).bin")
	}
	if !strings.HasSuffix(d.GetSavePath(), "x (1).bin") {
		t.Errorf("save path %q does not end with renamed leaf", d.GetSavePath())
	}
}

// LockFileName preserves the prior strict behaviour: a user who passed
// --filename foo.bin gets ErrFileExists on collision rather than a
// silent rename.
func TestNewDownloader_LockFileNameKeepsStrictCollision(t *testing.T) {
	body := []byte("hello content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "13")
		w.Header().Set("Content-Disposition", `attachment; filename="ignored.bin"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	orig := DlDataDir
	DlDataDir = t.TempDir()
	defer func() { DlDataDir = orig }()

	dlDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dlDir, "user.bin"), []byte("stub"), 0644); err != nil {
		t.Fatal(err)
	}

	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: dlDir,
		FileName:          "user.bin",
		LockFileName:      true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if d.GetFileName() != "user.bin" {
		t.Errorf("filename = %q, want %q (locked)", d.GetFileName(), "user.bin")
	}
	startErr := d.Start()
	if startErr == nil {
		t.Fatal("expected ErrFileExists when LockFileName=true and dest exists")
	}
	if !errors.Is(startErr, ErrFileExists) {
		t.Errorf("unexpected error: %v", startErr)
	}
}

// Overwrite=true bypasses uniquify regardless of LockFileName.
func TestNewDownloader_OverwriteSkipsUniquify(t *testing.T) {
	body := []byte("hello content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "13")
		w.Header().Set("Content-Disposition", `attachment; filename="x.bin"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	orig := DlDataDir
	DlDataDir = t.TempDir()
	defer func() { DlDataDir = orig }()

	dlDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dlDir, "x.bin"), []byte("stub"), 0644); err != nil {
		t.Fatal(err)
	}

	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: dlDir,
		Overwrite:         true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if d.GetFileName() != "x.bin" {
		t.Errorf("Overwrite=true should keep original name; got %q", d.GetFileName())
	}
}

// Tiny itoa to avoid importing strconv just for the stress helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := []byte{}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
