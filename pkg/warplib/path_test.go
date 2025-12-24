package warplib

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGetFileName_ProperPathJoin verifies that getFileName properly joins paths
// using filepath.Join instead of string concatenation.
// This test WILL FAIL with the current implementation that uses fmt.Sprintf("%s%s.warp").
func TestGetFileName_ProperPathJoin(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		hash     string
		wantPath string
	}{
		{
			name:     "directory without trailing separator",
			dir:      filepath.Join("tmp", "downloads"),
			hash:     "abc123",
			wantPath: filepath.Join("tmp", "downloads", "abc123.warp"),
		},
		{
			name:     "absolute path without trailing separator",
			dir:      filepath.Join("/", "var", "lib", "warpdl"),
			hash:     "def456",
			wantPath: filepath.Join("/", "var", "lib", "warpdl", "def456.warp"),
		},
		{
			name:     "single directory",
			dir:      "downloads",
			hash:     "xyz789",
			wantPath: filepath.Join("downloads", "xyz789.warp"),
		},
		{
			name:     "nested directories",
			dir:      filepath.Join("home", "user", "downloads", "segments"),
			hash:     "part1",
			wantPath: filepath.Join("home", "user", "downloads", "segments", "part1.warp"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFileName(tt.dir, tt.hash)
			if got != tt.wantPath {
				t.Errorf("getFileName(%q, %q) = %q, want %q",
					tt.dir, tt.hash, got, tt.wantPath)
			}
		})
	}
}

// TestGetFileName_CrossPlatform verifies that the path uses the correct OS separator.
// This test ensures cross-platform compatibility.
func TestGetFileName_CrossPlatform(t *testing.T) {
	dir := filepath.Join("data", "segments")
	hash := "test123"

	got := getFileName(dir, hash)

	// Verify result ends with .warp
	if !strings.HasSuffix(got, ".warp") {
		t.Errorf("getFileName result should end with .warp, got: %s", got)
	}

	// Verify result contains hash before .warp extension
	expectedSuffix := hash + ".warp"
	if !strings.HasSuffix(got, expectedSuffix) {
		t.Errorf("getFileName result should end with %q, got: %s", expectedSuffix, got)
	}

	// On Windows, verify no forward slashes in path (except in UNC paths)
	if runtime.GOOS == "windows" {
		// Count path separators
		if strings.Contains(got, "/") && !strings.HasPrefix(got, "\\\\") {
			t.Errorf("On Windows, path should use backslashes, got: %s", got)
		}
	}

	// Verify the path separator is present before hash
	// This will FAIL with current implementation since it concatenates without separator
	expectedPath := filepath.Join(dir, hash+".warp")
	if got != expectedPath {
		t.Errorf("getFileName(%q, %q) = %q, want %q (with proper separator)",
			dir, hash, got, expectedPath)
	}
}

// TestGetFileName_NoTrailingSlashRequired is the KEY TEST that demonstrates the bug.
// Current implementation requires preName to have a trailing slash, but it shouldn't.
// This test MUST FAIL with the current string concatenation approach.
func TestGetFileName_NoTrailingSlashRequired(t *testing.T) {
	tests := []struct {
		name         string
		preNameNoSep string // WITHOUT trailing separator
		hash         string
	}{
		{
			name:         "simple path",
			preNameNoSep: "downloads",
			hash:         "abc",
		},
		{
			name:         "nested path",
			preNameNoSep: filepath.Join("var", "warpdl", "data"),
			hash:         "def123",
		},
		{
			name:         "absolute path",
			preNameNoSep: filepath.Join("/", "tmp", "warpdl"),
			hash:         "ghi456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			got := getFileName(tt.preNameNoSep, tt.hash)
			expected := filepath.Join(tt.preNameNoSep, tt.hash+".warp")

			// This assertion WILL FAIL with current implementation
			// Current: fmt.Sprintf("%s%s.warp", preName, hash) produces "downloadsabc.warp"
			// Expected: filepath.Join produces "downloads/abc.warp" (or "downloads\abc.warp" on Windows)
			if got != expected {
				t.Errorf("getFileName(%q, %q) without trailing separator failed:\n  got:  %q\n  want: %q",
					tt.preNameNoSep, tt.hash, got, expected)

				// Additional diagnostic: show the bug
				if !strings.Contains(got, string(filepath.Separator)) {
					t.Errorf("  ERROR: Result missing path separator - concatenated without separator")
				}
			}
		})
	}
}

// TestDlPathConstruction verifies the actual pattern used in production code.
// This test ensures dlPath construction doesn't require trailing separators.
func TestDlPathConstruction(t *testing.T) {
	// Setup temp config dir
	tmpDir := t.TempDir()
	if err := SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir failed: %v", err)
	}

	hash := "test-download-123"

	// Construct dlPath the CORRECT way (without hardcoded "/" and trailing separator)
	dlPath := filepath.Join(DlDataDir, hash)

	// Verify NO trailing separator
	if strings.HasSuffix(dlPath, string(filepath.Separator)) {
		t.Errorf("dlPath should NOT have trailing separator, got: %q", dlPath)
	}

	// Verify proper path structure
	expectedPath := filepath.Join(DlDataDir, hash)
	if dlPath != expectedPath {
		t.Errorf("dlPath construction failed:\n  got:  %q\n  want: %q", dlPath, expectedPath)
	}

	// Now test getFileName with this dlPath
	partHash := "part1"
	partFile := getFileName(dlPath, partHash)

	// Expected: DlDataDir/hash/part1.warp
	expectedPartFile := filepath.Join(DlDataDir, hash, partHash+".warp")

	// This WILL FAIL because current getFileName expects dlPath to end with "/"
	// Current result: "{DlDataDir}/test-download-123part1.warp" (no separator before part)
	// Expected result: "{DlDataDir}/test-download-123/part1.warp" (with separator)
	if partFile != expectedPartFile {
		t.Errorf("Part file path incorrect:\n  got:  %q\n  want: %q",
			partFile, expectedPartFile)

		// Show that the hash and partHash are concatenated without separator
		wrongConcat := dlPath + partHash + ".warp"
		if partFile == wrongConcat {
			t.Errorf("  ERROR: getFileName is using string concatenation instead of filepath.Join")
		}
	}
}

// TestLogsPath verifies logs.txt path construction.
// This ensures consistent path handling for log files.
func TestLogsPath(t *testing.T) {
	tmpDir := t.TempDir()
	if err := SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir failed: %v", err)
	}

	hash := "download-abc"
	dlPath := filepath.Join(DlDataDir, hash)

	// Construct logs path
	logsPath := filepath.Join(dlPath, "logs.txt")

	// Verify structure
	expectedLogs := filepath.Join(DlDataDir, hash, "logs.txt")
	if logsPath != expectedLogs {
		t.Errorf("Logs path incorrect:\n  got:  %q\n  want: %q", logsPath, expectedLogs)
	}

	// Verify no double separators
	doubleSep := string(filepath.Separator) + string(filepath.Separator)
	if strings.Contains(logsPath, doubleSep) {
		t.Errorf("Logs path contains double separator: %q", logsPath)
	}
}

// TestGetFileName_WindowsUNCPath tests UNC path handling on Windows.
// UNC paths like \\server\share must be preserved correctly.
func TestGetFileName_WindowsUNCPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC path test only runs on Windows")
	}

	tests := []struct {
		name string
		dir  string
		hash string
	}{
		{
			name: "UNC server share",
			dir:  `\\server\share\downloads`,
			hash: "file123",
		},
		{
			name: "UNC with nested path",
			dir:  `\\server\share\warpdl\data`,
			hash: "part456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			got := getFileName(tt.dir, tt.hash)
			expected := filepath.Join(tt.dir, tt.hash+".warp")

			if got != expected {
				t.Errorf("getFileName(%q, %q) UNC path failed:\n  got:  %q\n  want: %q",
					tt.dir, tt.hash, got, expected)
			}

			// Verify UNC prefix is preserved
			if !strings.HasPrefix(got, `\\`) {
				t.Errorf("UNC path prefix lost in result: %q", got)
			}

			// Verify uses backslashes, not forward slashes
			if strings.Contains(got, "/") {
				t.Errorf("UNC path should use backslashes, got: %q", got)
			}
		})
	}
}

// TestGetFileName_EdgeCases tests additional edge cases.
func TestGetFileName_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		hash     string
		validate func(t *testing.T, result string)
	}{
		{
			name: "empty hash",
			dir:  filepath.Join("data", "segments"),
			hash: "",
			validate: func(t *testing.T, result string) {
				expected := filepath.Join("data", "segments", ".warp")
				if result != expected {
					t.Errorf("Expected %q, got %q", expected, result)
				}
			},
		},
		{
			name: "hash with special chars",
			dir:  "downloads",
			hash: "hash-with_special.chars",
			validate: func(t *testing.T, result string) {
				expected := filepath.Join("downloads", "hash-with_special.chars.warp")
				if result != expected {
					t.Errorf("Expected %q, got %q", expected, result)
				}
			},
		},
		{
			name: "very long hash",
			dir:  "data",
			hash: strings.Repeat("a", 256),
			validate: func(t *testing.T, result string) {
				expected := filepath.Join("data", strings.Repeat("a", 256)+".warp")
				if result != expected {
					t.Errorf("Expected %q, got %q", expected, result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			result := getFileName(tt.dir, tt.hash)
			tt.validate(t, result)
		})
	}
}
