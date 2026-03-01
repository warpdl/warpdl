package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCookiesFrom_FileNotFound(t *testing.T) {
	err := validateCookiesFrom("/nonexistent/path/cookies.sqlite")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestValidateCookiesFrom_IsDirectory(t *testing.T) {
	dir := t.TempDir()
	err := validateCookiesFrom(dir)
	if err == nil {
		t.Fatal("expected error for directory, got nil")
	}
}

func TestValidateCookiesFrom_ValidPath(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "cookies.sqlite")
	if err := os.WriteFile(fpath, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	err := validateCookiesFrom(fpath)
	if err != nil {
		t.Fatalf("unexpected error for valid file: %v", err)
	}
}

func TestValidateCookiesFrom_AutoKeyword(t *testing.T) {
	err := validateCookiesFrom("auto")
	if err != nil {
		t.Fatalf("unexpected error for 'auto' keyword: %v", err)
	}
}

func TestValidateCookiesFrom_EmptyString(t *testing.T) {
	err := validateCookiesFrom("")
	if err != nil {
		t.Fatalf("unexpected error for empty string: %v", err)
	}
}
