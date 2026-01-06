package warplib

import (
	"bytes"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test_parseFileName(t *testing.T) {
	type args struct {
		req *http.Request
		cd  string
	}
	tests := []struct {
		name   string
		args   args
		wantFn string
	}{
		{
			name: "No Content Disposition",
			args: args{
				req: &http.Request{URL: &url.URL{Path: "hello/world.jpeg"}},
			},
			wantFn: "world.jpeg",
		},
		{
			name: "Has Content Disposition",
			args: args{
				req: &http.Request{URL: &url.URL{Path: "hello/world.jpg"}},
				cd:  `attachment; filename="world.jpg"`,
			},
			wantFn: "world.jpg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotFn := parseFileName(tt.args.req, tt.args.cd); gotFn != tt.wantFn {
				t.Errorf("parseFileName() = %v, want %v", gotFn, tt.wantFn)
			}
		})
	}
}

func TestGetPath(t *testing.T) {
	type args struct {
		directory string
		file      string
	}
	tests := []struct {
		name     string
		args     args
		wantPath string
	}{
		{"current dir", args{".", "hello.bin"}, filepath.Join(".", "hello.bin")},
		{"nested path", args{"home/bin/dir", "hello.bin"}, filepath.Join("home/bin/dir", "hello.bin")},
		{"absolute path", args{"/home/user", "file.txt"}, filepath.Join("/home/user", "file.txt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotPath := GetPath(tt.args.directory, tt.args.file); gotPath != tt.wantPath {
				t.Errorf("GetPath() = %v, want %v", gotPath, tt.wantPath)
			}
		})
	}
}

func TestPlace(t *testing.T) {
	src := []int{1, 2, 4}
	got := Place(src, 3, 2)
	if len(got) != 4 || got[2] != 3 {
		t.Fatalf("unexpected placement result: %v", got)
	}
}

func TestGetDownloadTime(t *testing.T) {
	d := getDownloadTime(MB, 2*MB)
	if d <= 0 {
		t.Fatalf("expected positive duration, got %v", d)
	}
}

func TestSetConfigDir(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	abs, _ := filepath.Abs(base)
	if ConfigDir != abs {
		t.Fatalf("expected ConfigDir %s, got %s", abs, ConfigDir)
	}
	if _, err := os.Stat(DlDataDir); err != nil {
		t.Fatalf("expected DlDataDir to exist: %v", err)
	}
}

func TestSetConfigDirEmpty(t *testing.T) {
	if err := setConfigDir(""); err == nil {
		t.Fatalf("expected error for empty config dir")
	}
}

func TestWlog(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	wlog(logger, "hello %s", "world")
	if got := buf.String(); got == "" || got[len(got)-1] != '\n' {
		t.Fatalf("expected newline in log output, got %q", got)
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal filename", "video.mp4", "video.mp4"},
		{"question mark", "Can ACP Solve This Mystery?.3gp", "Can ACP Solve This Mystery_.3gp"},
		{"multiple special chars", "file<>:\"|?*.txt", "file_______.txt"},
		{"url encoded question mark", "file%3F.txt", "file_.txt"},
		{"forward slash", "path/to/file.txt", "path_to_file.txt"},
		{"backslash", "path\\to\\file.txt", "path_to_file.txt"},
		{"reserved name CON", "CON.txt", "_CON.txt"},
		{"reserved name lowercase con", "con.txt", "_con.txt"},
		{"reserved name NUL", "NUL", "_NUL"},
		{"reserved name COM1", "COM1.txt", "_COM1.txt"},
		{"leading dots", "...file.txt", "file.txt"},
		{"trailing dots", "file...", "file"},
		{"leading spaces", "  file.txt", "file.txt"},
		{"trailing spaces", "file.txt  ", "file.txt"},
		{"all question marks", "???", "___"},
		{"empty string", "", ""},
		{"just dots", "...", "download"},
		{"control characters", "file\x00\x01\x1f.txt", "file.txt"},
		{"extension preserved", "My:Video?.mp4", "My_Video_.mp4"},
		{"colon in name", "2023:12:24.log", "2023_12_24.log"},
		{"pipe character", "file|name.txt", "file_name.txt"},
		{"asterisk", "*.txt", "_.txt"},
		{"unicode preserved", "fichier_日本語.txt", "fichier_日本語.txt"},
		{"unicode with special", "fichier?日本語.txt", "fichier_日本語.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFilename(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func Test_parseFileName_SpecialChars(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			req *http.Request
			cd  string
		}
		wantFn string
	}{
		{
			name: "URL with question mark encoded",
			args: struct {
				req *http.Request
				cd  string
			}{
				req: &http.Request{URL: &url.URL{Path: "/videos/Can%20ACP%20Solve%20This%20Mystery%3F.3gp"}},
				cd:  "",
			},
			wantFn: "Can ACP Solve This Mystery_.3gp",
		},
		{
			name: "Content-Disposition with special chars",
			args: struct {
				req *http.Request
				cd  string
			}{
				req: &http.Request{URL: &url.URL{Path: "/file"}},
				cd:  `attachment; filename="What's This?.mp4"`,
			},
			wantFn: "What's This_.mp4",
		},
		{
			name: "URL with colon",
			args: struct {
				req *http.Request
				cd  string
			}{
				req: &http.Request{URL: &url.URL{Path: "/files/2023:01:01.log"}},
				cd:  "",
			},
			wantFn: "2023_01_01.log",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotFn := parseFileName(tt.args.req, tt.args.cd); gotFn != tt.wantFn {
				t.Errorf("parseFileName() = %v, want %v", gotFn, tt.wantFn)
			}
		})
	}
}

// TestDefaultConfigDirReturnsError tests that defaultConfigDir returns error instead of panicking
func TestDefaultConfigDirReturnsError(t *testing.T) {
	// This test verifies that defaultConfigDir returns (string, error) tuple
	dir, err := defaultConfigDir()

	// In normal conditions it should succeed
	if err != nil {
		t.Logf("defaultConfigDir returned error (this is expected in some CI environments): %v", err)
		// Error is acceptable - the key is it didn't panic
		return
	}

	if dir == "" {
		t.Errorf("defaultConfigDir returned empty string with nil error")
	}
}

// TestInitConfigDirWithInvalidPath tests that initConfigDir handles invalid paths gracefully
func TestInitConfigDirWithInvalidPath(t *testing.T) {
	// Save original config to restore later
	originalConfig := ConfigDir
	originalDlData := DlDataDir
	defer func() {
		ConfigDir = originalConfig
		DlDataDir = originalDlData
	}()

	// Test with a completely invalid path that cannot be created (e.g., path with null bytes)
	invalidPath := "/dev/null/cannot/create/this\x00path"

	// initConfigDir should handle this gracefully without panicking
	err := initConfigDir(invalidPath)

	// We expect it to either succeed with a fallback or, if it returns an error,
	// to leave ConfigDir unchanged from its original value.
	if err != nil {
		// When initConfigDir returns an error, the temp-dir fallback has also failed,
		// so ConfigDir should retain its previous value from package initialization.
		if ConfigDir != originalConfig {
			t.Errorf("initConfigDir returned error but modified ConfigDir: got %q, want %q (original)", ConfigDir, originalConfig)
		}
	}
}

// TestInitConfigDirWithTempDirFallback tests temp directory fallback when default config fails
func TestInitConfigDirWithTempDirFallback(t *testing.T) {
	originalConfig := ConfigDir
	originalDlData := DlDataDir
	defer func() {
		ConfigDir = originalConfig
		DlDataDir = originalDlData
	}()

	// Create a read-only directory to simulate permission issues
	tempBase := t.TempDir()
	readOnlyDir := filepath.Join(tempBase, "readonly")
	if err := os.Mkdir(readOnlyDir, 0444); err != nil {
		t.Fatalf("failed to create read-only dir: %v", err)
	}

	invalidPath := filepath.Join(readOnlyDir, "warpdl")

	// This should fall back to temp dir instead of panicking
	err := initConfigDir(invalidPath)

	// Should either succeed with fallback or at minimum not panic
	if err != nil {
		t.Logf("initConfigDir returned error (fallback expected): %v", err)
	}

	// Verify ConfigDir was set to something valid
	if ConfigDir == "" {
		t.Errorf("ConfigDir is empty after initConfigDir")
	}

	// If it succeeded (fell back to temp), verify temp dir was used
	if err == nil && !strings.HasPrefix(ConfigDir, os.TempDir()) {
		t.Logf("Warning: expected temp dir fallback, got: %s", ConfigDir)
	}
}

// TestSetConfigDirInvalidPathReturnsError tests that setConfigDir returns error for invalid paths
func TestSetConfigDirInvalidPathReturnsError(t *testing.T) {
	originalConfig := ConfigDir
	originalDlData := DlDataDir
	defer func() {
		ConfigDir = originalConfig
		DlDataDir = originalDlData
	}()

	// Test with path containing null byte (invalid on all platforms)
	invalidPath := "/tmp/test\x00invalid"

	err := setConfigDir(invalidPath)
	if err == nil {
		t.Errorf("setConfigDir should return error for path with null byte")
	}
}

func TestGetMinPartSize(t *testing.T) {
	tests := []struct {
		name        string
		fileSize    int64
		expectedMin int64
	}{
		// <100MB -> 512KB
		{"small 1MB", 1 * MB, 512 * KB},
		{"small 50MB", 50 * MB, 512 * KB},
		{"boundary 100MB-1", 100*MB - 1, 512 * KB},

		// 100MB-1GB -> 1MB
		{"boundary 100MB", 100 * MB, 1 * MB},
		{"medium 500MB", 500 * MB, 1 * MB},
		{"boundary 1GB-1", 1*GB - 1, 1 * MB},

		// 1GB-10GB -> 2MB
		{"boundary 1GB", 1 * GB, 2 * MB},
		{"large 5GB", 5 * GB, 2 * MB},
		{"boundary 10GB-1", 10*GB - 1, 2 * MB},

		// >10GB -> 4MB (max cap)
		{"boundary 10GB", 10 * GB, 4 * MB},
		{"huge 50GB", 50 * GB, 4 * MB},

		// Edge cases
		{"zero", 0, 512 * KB},
		{"unknown -1", -1, 512 * KB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMinPartSize(tt.fileSize)
			if got != tt.expectedMin {
				t.Errorf("getMinPartSize(%d) = %d, want %d", tt.fileSize, got, tt.expectedMin)
			}
		})
	}
}
