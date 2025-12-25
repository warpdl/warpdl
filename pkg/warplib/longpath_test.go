package warplib

import (
	"runtime"
	"strings"
	"testing"
)

// TestLongPathConstants verifies the long path constants are defined correctly.
// These tests WILL FAIL until the constants are implemented.
func TestLongPathConstants(t *testing.T) {
	t.Run("LongPathThreshold should be 240", func(t *testing.T) {
		// This will fail with: undefined: LongPathThreshold
		const expectedThreshold = 240
		if LongPathThreshold != expectedThreshold {
			t.Errorf("LongPathThreshold = %d, want %d", LongPathThreshold, expectedThreshold)
		}
	})

	t.Run("LongPathPrefix should be \\\\?\\", func(t *testing.T) {
		// This will fail with: undefined: LongPathPrefix
		const expectedPrefix = `\\?\`
		if LongPathPrefix != expectedPrefix {
			t.Errorf("LongPathPrefix = %q, want %q", LongPathPrefix, expectedPrefix)
		}
	})

	t.Run("UNCPrefix should be \\\\", func(t *testing.T) {
		// This will fail with: undefined: UNCPrefix
		const expectedUNCPrefix = `\\`
		if UNCPrefix != expectedUNCPrefix {
			t.Errorf("UNCPrefix = %q, want %q", UNCPrefix, expectedUNCPrefix)
		}
	})
}

// TestIsLongPath tests the helper function that determines if a path exceeds the threshold.
// These tests WILL FAIL until IsLongPath() is implemented.
func TestIsLongPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "empty path is not long",
			path:     "",
			expected: false,
		},
		{
			name:     "short path (10 chars) is not long",
			path:     "C:\\test.txt",
			expected: false,
		},
		{
			name:     "path at threshold (240 chars) is not long",
			path:     "C:\\" + strings.Repeat("a", 237), // C:\ = 3 chars + 237 = 240
			expected: false,
		},
		{
			name:     "path just over threshold (241 chars) is long",
			path:     "C:\\" + strings.Repeat("a", 238), // C:\ = 3 chars + 238 = 241
			expected: true,
		},
		{
			name:     "very long path (500 chars) is long",
			path:     "C:\\" + strings.Repeat("a", 497),
			expected: true,
		},
		{
			name:     "already prefixed long path counts full length",
			path:     `\\?\C:\` + strings.Repeat("a", 240),
			expected: true,
		},
		{
			name:     "UNC path over threshold is long",
			path:     `\\server\share\` + strings.Repeat("a", 230),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			// This will fail with: undefined: IsLongPath
			got := IsLongPath(tt.path)
			if got != tt.expected {
				t.Errorf("IsLongPath(%q) = %v, want %v (length: %d)",
					tt.path, got, tt.expected, len(tt.path))
			}
		})
	}
}

// TestNormalizePath_ShortPaths verifies that short paths remain unchanged.
// These tests WILL FAIL until NormalizePath() is implemented.
func TestNormalizePath_ShortPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "empty path unchanged",
			path: "",
		},
		{
			name: "simple short path",
			path: `C:\Users\test\file.txt`,
		},
		{
			name: "short path at 100 chars",
			path: `C:\` + strings.Repeat("a", 97),
		},
		{
			name: "short path at 200 chars",
			path: `C:\` + strings.Repeat("a", 197),
		},
		{
			name: "short path exactly at threshold (240 chars)",
			path: `C:\` + strings.Repeat("a", 237),
		},
		{
			name: "relative path",
			path: `relative\path\to\file.txt`,
		},
		{
			name: "current directory reference",
			path: `.\file.txt`,
		},
		{
			name: "parent directory reference",
			path: `..\file.txt`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			// This will fail with: undefined: NormalizePath
			got := NormalizePath(tt.path)
			if got != tt.path {
				t.Errorf("NormalizePath(%q) = %q, want %q (short path should be unchanged)",
					tt.path, got, tt.path)
			}
		})
	}
}

// TestNormalizePath_LongPaths verifies that long paths get the \\?\ prefix on Windows.
// These tests are Windows-specific and skip on other platforms.
func TestNormalizePath_LongPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("long path prefix tests only run on Windows")
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "path just over threshold gets prefix",
			path:     `C:\` + strings.Repeat("a", 238), // 241 chars
			expected: `\\?\C:\` + strings.Repeat("a", 238),
		},
		{
			name:     "very long path (300 chars) gets prefix",
			path:     `C:\` + strings.Repeat("b", 297),
			expected: `\\?\C:\` + strings.Repeat("b", 297),
		},
		{
			name:     "long path (500 chars) gets prefix",
			path:     `C:\Users\` + strings.Repeat("x", 492),
			expected: `\\?\C:\Users\` + strings.Repeat("x", 492),
		},
		{
			name:     "maximum Windows path (32767 chars) gets prefix",
			path:     `C:\` + strings.Repeat("m", 32764),
			expected: `\\?\C:\` + strings.Repeat("m", 32764),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			got := NormalizePath(tt.path)
			if got != tt.expected {
				t.Errorf("NormalizePath() failed for long path:\n  path len: %d\n  got:  %q\n  want: %q",
					len(tt.path), got, tt.expected)
			}

			// Verify prefix is present
			if !strings.HasPrefix(got, `\\?\`) {
				t.Errorf("NormalizePath() result missing \\\\?\\ prefix: %q", got)
			}
		})
	}
}

// TestNormalizePath_AlreadyPrefixed verifies that already-prefixed paths are not double-prefixed.
// These tests WILL FAIL until NormalizePath() is implemented.
func TestNormalizePath_AlreadyPrefixed(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "short path with prefix unchanged",
			path: `\\?\C:\short\path.txt`,
		},
		{
			name: "long path with prefix unchanged",
			path: `\\?\C:\` + strings.Repeat("a", 300),
		},
		{
			name: "UNC long path with prefix unchanged",
			path: `\\?\UNC\server\share\` + strings.Repeat("b", 300),
		},
		{
			name: "prefix with different drive",
			path: `\\?\D:\some\long\` + strings.Repeat("x", 250),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			// This will fail with: undefined: NormalizePath
			got := NormalizePath(tt.path)
			if got != tt.path {
				t.Errorf("NormalizePath(%q) = %q, want %q (already-prefixed should be unchanged)",
					tt.path, got, tt.path)
			}

			// Verify no double-prefixing
			if strings.HasPrefix(got, `\\?\\\?\`) {
				t.Errorf("NormalizePath() double-prefixed the path: %q", got)
			}
		})
	}
}

// TestNormalizePath_UNCPaths verifies UNC path handling on Windows.
// UNC paths (\\server\share) must become \\?\UNC\server\share when long.
// These tests are Windows-specific and skip on other platforms.
func TestNormalizePath_UNCPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC path tests only run on Windows")
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "short UNC path unchanged",
			path:     `\\server\share\file.txt`,
			expected: `\\server\share\file.txt`,
		},
		{
			name:     "long UNC path gets UNC prefix",
			path:     `\\server\share\` + strings.Repeat("a", 230),
			expected: `\\?\UNC\server\share\` + strings.Repeat("a", 230),
		},
		{
			name:     "very long UNC path (400 chars) gets UNC prefix",
			path:     `\\fileserver\documents\` + strings.Repeat("b", 380),
			expected: `\\?\UNC\fileserver\documents\` + strings.Repeat("b", 380),
		},
		{
			name:     "UNC path with nested directories",
			path:     `\\server\share\dept\team\project\` + strings.Repeat("x", 220),
			expected: `\\?\UNC\server\share\dept\team\project\` + strings.Repeat("x", 220),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			got := NormalizePath(tt.path)
			if got != tt.expected {
				t.Errorf("NormalizePath() UNC path handling failed:\n  path len: %d\n  got:  %q\n  want: %q",
					len(tt.path), got, tt.expected)
			}
		})
	}
}

// TestNormalizePath_ForwardSlashNormalization verifies forward slashes are converted to backslashes on Windows.
// Windows APIs with \\?\ prefix require backslashes.
// These tests are Windows-specific and skip on other platforms.
func TestNormalizePath_ForwardSlashNormalization(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("forward slash normalization tests only run on Windows")
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "short path with forward slashes gets normalized",
			path:     `C:/Users/test/file.txt`,
			expected: `C:\Users\test\file.txt`,
		},
		{
			name:     "long path with forward slashes gets normalized and prefixed",
			path:     `C:/` + strings.Repeat("a/", 120), // Creates long path with forward slashes
			expected: `\\?\C:\` + strings.Repeat(`a\`, 120),
		},
		{
			name:     "mixed slashes normalized",
			path:     `C:/Users\test/` + strings.Repeat("x", 230),
			expected: `\\?\C:\Users\test\` + strings.Repeat("x", 230),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			got := NormalizePath(tt.path)
			if got != tt.expected {
				t.Errorf("NormalizePath() slash normalization failed:\n  got:  %q\n  want: %q",
					got, tt.expected)
			}

			// Verify no forward slashes in result
			if strings.Contains(got, "/") {
				t.Errorf("NormalizePath() result contains forward slashes: %q", got)
			}
		})
	}
}

// TestNormalizePath_RelativePaths verifies relative paths cannot get \\?\ prefix.
// The \\?\ prefix only works with absolute paths.
// These tests WILL FAIL until NormalizePath() is implemented.
func TestNormalizePath_RelativePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "relative path remains unchanged even if long",
			path: `relative\path\` + strings.Repeat("a", 250),
		},
		{
			name: "current directory reference with long path",
			path: `.\` + strings.Repeat("b", 250),
		},
		{
			name: "parent directory reference with long path",
			path: `..\` + strings.Repeat("c", 250),
		},
		{
			name: "deeply nested relative path",
			path: strings.Repeat(`subdir\`, 50), // Creates very long relative path
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			// This will fail with: undefined: NormalizePath
			got := NormalizePath(tt.path)
			if got != tt.path {
				t.Errorf("NormalizePath(%q) = %q, want %q (relative paths should remain unchanged)",
					tt.path, got, tt.path)
			}

			// Verify no \\?\ prefix for relative paths
			if strings.HasPrefix(got, `\\?\`) {
				t.Errorf("NormalizePath() incorrectly added prefix to relative path: %q", got)
			}
		})
	}
}

// TestNormalizePath_EdgeCases tests edge cases and special scenarios on Windows.
// These tests are Windows-specific and skip on other platforms.
func TestNormalizePath_EdgeCases(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("edge case tests only run on Windows")
	}

	tests := []struct {
		name     string
		path     string
		expected string
		validate func(t *testing.T, result string)
	}{
		{
			name:     "path with spaces",
			path:     `C:\Program Files\` + strings.Repeat("a", 230),
			expected: `\\?\C:\Program Files\` + strings.Repeat("a", 230),
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, " ") {
					t.Errorf("spaces should be preserved in result")
				}
			},
		},
		{
			name:     "path with special characters",
			path:     `C:\test-path_123\` + strings.Repeat("x", 230),
			expected: `\\?\C:\test-path_123\` + strings.Repeat("x", 230),
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "-") || !strings.Contains(result, "_") {
					t.Errorf("special characters should be preserved")
				}
			},
		},
		{
			name:     "path with trailing backslash",
			path:     `C:\` + strings.Repeat("a", 238) + `\`,
			expected: `\\?\C:\` + strings.Repeat("a", 238) + `\`,
			validate: func(t *testing.T, result string) {
				if !strings.HasSuffix(result, `\`) {
					t.Errorf("trailing backslash should be preserved")
				}
			},
		},
		{
			name:     "path with Unicode characters",
			path:     `C:\文档\` + strings.Repeat("文", 120), // Chinese characters
			expected: `\\?\C:\文档\` + strings.Repeat("文", 120),
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "文") {
					t.Errorf("Unicode characters should be preserved")
				}
			},
		},
		{
			name:     "drive letter only",
			path:     `C:\`,
			expected: `C:\`,
			validate: nil,
		},
		{
			name:     "network path with dollar sign (admin share)",
			path:     `\\server\C$\` + strings.Repeat("a", 230),
			expected: `\\?\UNC\server\C$\` + strings.Repeat("a", 230),
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "$") {
					t.Errorf("dollar sign should be preserved in admin share")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			got := NormalizePath(tt.path)
			if got != tt.expected {
				t.Errorf("NormalizePath() edge case failed:\n  got:  %q\n  want: %q",
					got, tt.expected)
			}

			if tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

// TestNormalizePath_PlatformBehavior verifies platform-specific behavior.
// On non-Windows platforms, NormalizePath should return the path unchanged.
func TestNormalizePath_PlatformBehavior(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Run("non-Windows platforms return path unchanged", func(t *testing.T) {
			t.Helper()
			// Even long paths should remain unchanged on Unix
			longPath := "/home/user/" + strings.Repeat("a", 300)
			// This will fail with: undefined: NormalizePath
			got := NormalizePath(longPath)
			if got != longPath {
				t.Errorf("NormalizePath() on %s should return path unchanged:\n  got:  %q\n  want: %q",
					runtime.GOOS, got, longPath)
			}
		})
	} else {
		t.Run("Windows applies transformations", func(t *testing.T) {
			t.Helper()
			longPath := `C:\` + strings.Repeat("a", 250)
			// This will fail with: undefined: NormalizePath
			got := NormalizePath(longPath)
			// On Windows, long paths should get prefix
			if !strings.HasPrefix(got, `\\?\`) {
				t.Errorf("NormalizePath() on Windows should add prefix for long paths: %q", got)
			}
		})
	}
}

// TestNormalizePath_Integration tests realistic scenarios.
// These tests WILL FAIL until NormalizePath() is implemented.
func TestNormalizePath_Integration(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		description string
		expectPrefx bool
	}{
		{
			name:        "typical download path (short)",
			path:        `C:\Users\Alice\Downloads\file.warp`,
			description: "normal user download location",
			expectPrefx: false,
		},
		{
			name:        "deeply nested project structure (long)",
			path:        `C:\Users\Alice\Projects\CompanyName\DepartmentName\TeamName\ProjectName\SourceCode\Backend\Services\` + strings.Repeat("Module", 30),
			description: "realistic deep project hierarchy",
			expectPrefx: true,
		},
		{
			name:        "network share download (long)",
			path:        `\\fileserver\shared\Downloads\Users\Alice\Work\Projects\` + strings.Repeat("x", 220),
			description: "long path on network share",
			expectPrefx: true,
		},
		{
			name:        "temp download location (short)",
			path:        `C:\Temp\warpdl\abc123.warp`,
			description: "temp directory for downloads",
			expectPrefx: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()
			// This will fail with: undefined: NormalizePath
			got := NormalizePath(tt.path)

			if tt.expectPrefx && runtime.GOOS == "windows" {
				if !strings.HasPrefix(got, `\\?\`) {
					t.Errorf("Expected \\\\?\\ prefix for %s, got: %q", tt.description, got)
				}
			} else {
				if strings.HasPrefix(got, `\\?\`) && runtime.GOOS != "windows" {
					t.Errorf("Unexpected \\\\?\\ prefix on non-Windows for %s, got: %q", tt.description, got)
				}
			}

			t.Logf("Path: %s (len=%d) -> %s (len=%d)",
				tt.path, len(tt.path), got, len(got))
		})
	}
}
