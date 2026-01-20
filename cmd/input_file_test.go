package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseInputFile_BasicURLs(t *testing.T) {
	// Create temp file with 3 URLs
	content := `https://example.com/file1.zip
https://example.com/file2.tar.gz
https://example.com/file3.iso`

	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	result, err := ParseInputFile(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.URLs) != 3 {
		t.Errorf("expected 3 URLs, got %d", len(result.URLs))
	}

	expected := []string{
		"https://example.com/file1.zip",
		"https://example.com/file2.tar.gz",
		"https://example.com/file3.iso",
	}
	for i, url := range result.URLs {
		if url != expected[i] {
			t.Errorf("URL %d: expected %q, got %q", i, expected[i], url)
		}
	}
}

func TestParseInputFile_SkipComments(t *testing.T) {
	content := `# This is a comment
https://example.com/file1.zip
# Another comment
https://example.com/file2.zip
  # Indented comment`

	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	result, err := ParseInputFile(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.URLs) != 2 {
		t.Errorf("expected 2 URLs (comments skipped), got %d", len(result.URLs))
	}

	if result.SkippedLines != 3 {
		t.Errorf("expected 3 skipped comment lines, got %d", result.SkippedLines)
	}
}

func TestParseInputFile_SkipEmptyLines(t *testing.T) {
	content := `https://example.com/file1.zip

https://example.com/file2.zip

`

	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	result, err := ParseInputFile(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.URLs) != 2 {
		t.Errorf("expected 2 URLs (empty lines skipped), got %d", len(result.URLs))
	}
}

func TestParseInputFile_TrimWhitespace(t *testing.T) {
	content := `  https://example.com/file1.zip
	https://example.com/file2.zip
https://example.com/file3.zip`

	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	result, err := ParseInputFile(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.URLs) != 3 {
		t.Errorf("expected 3 URLs, got %d", len(result.URLs))
	}

	// Verify whitespace was trimmed
	for i, url := range result.URLs {
		if url[0] == ' ' || url[0] == '\t' {
			t.Errorf("URL %d has leading whitespace: %q", i, url)
		}
		lastChar := url[len(url)-1]
		if lastChar == ' ' || lastChar == '\t' {
			t.Errorf("URL %d has trailing whitespace: %q", i, url)
		}
	}
}

func TestParseInputFile_MixedContent(t *testing.T) {
	content := `# Download list
https://example.com/file1.zip

# Second section
https://example.com/file2.zip
  https://example.com/file3.zip

# End of list`

	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	result, err := ParseInputFile(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.URLs) != 3 {
		t.Errorf("expected 3 URLs from mixed content, got %d", len(result.URLs))
	}

	// Verify total line counts
	if result.TotalLines < 8 {
		t.Errorf("expected at least 8 total lines, got %d", result.TotalLines)
	}
}

func TestParseInputFile_FileNotFound(t *testing.T) {
	_, err := ParseInputFile("/nonexistent/path/urls.txt")
	if err == nil {
		t.Error("expected error for non-existent file")
	}

	// Verify error type
	var inputFileErr *InputFileError
	if !errors.As(err, &inputFileErr) {
		t.Fatalf("expected InputFileError, got %T", err)
	}
	if !errors.Is(inputFileErr.Err, ErrInputFileNotFound) {
		t.Errorf("expected ErrInputFileNotFound, got %v", inputFileErr.Err)
	}
	if inputFileErr.Path != "/nonexistent/path/urls.txt" {
		t.Errorf("expected path '/nonexistent/path/urls.txt', got '%s'", inputFileErr.Path)
	}
}

func TestParseInputFile_PermissionDenied(t *testing.T) {
	// Create temp file with no read permissions
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "unreadable.txt")
	err := os.WriteFile(tmpFile, []byte("https://example.com"), 0000)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	_, err = ParseInputFile(tmpFile)
	if err == nil {
		t.Error("expected error for unreadable file")
	}

	// Verify error type
	var inputFileErr *InputFileError
	if !errors.As(err, &inputFileErr) {
		t.Fatalf("expected InputFileError, got %T", err)
	}
	if !errors.Is(inputFileErr.Err, ErrInputFilePermission) {
		t.Errorf("expected ErrInputFilePermission, got %v", inputFileErr.Err)
	}
}

func TestParseInputFile_EmptyFile(t *testing.T) {
	// Create empty file
	tmpFile := createTempInputFile(t, "")
	defer os.Remove(tmpFile)

	_, err := ParseInputFile(tmpFile)
	if err == nil {
		t.Error("expected error for empty file")
	}

	// Verify error type
	var inputFileErr *InputFileError
	if !errors.As(err, &inputFileErr) {
		t.Fatalf("expected InputFileError, got %T", err)
	}
	if !errors.Is(inputFileErr.Err, ErrInputFileEmpty) {
		t.Errorf("expected ErrInputFileEmpty, got %v", inputFileErr.Err)
	}
}

func TestParseInputFile_OnlyCommentsAndEmptyLines(t *testing.T) {
	// Create file with only comments and empty lines (no valid URLs)
	content := `# This is a comment
# Another comment


# More comments`
	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	result, err := ParseInputFile(tmpFile)
	if err == nil {
		t.Error("expected error for file with no valid URLs")
	}

	// Verify error type
	var inputFileErr *InputFileError
	if !errors.As(err, &inputFileErr) {
		t.Fatalf("expected InputFileError, got %T", err)
	}
	if !errors.Is(inputFileErr.Err, ErrInputFileEmpty) {
		t.Errorf("expected ErrInputFileEmpty, got %v", inputFileErr.Err)
	}

	// Result should still be returned with metadata
	if result == nil {
		t.Error("expected result to be returned even on empty file error")
	}
	if result.SkippedLines != 3 {
		t.Errorf("expected 3 skipped comment lines, got %d", result.SkippedLines)
	}
}

func TestInputFileError_Error(t *testing.T) {
	err := NewInputFileError("/path/to/file.txt", ErrInputFileNotFound)
	expected := "input file not found: /path/to/file.txt"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestInputFileError_Unwrap(t *testing.T) {
	err := NewInputFileError("/path/to/file.txt", ErrInputFileNotFound)
	unwrapped := err.Unwrap()
	if unwrapped != ErrInputFileNotFound {
		t.Errorf("expected ErrInputFileNotFound, got %v", unwrapped)
	}
}

func TestNewInputFileError(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		err      error
		wantPath string
		wantErr  error
	}{
		{
			name:     "file not found",
			path:     "/tmp/missing.txt",
			err:      ErrInputFileNotFound,
			wantPath: "/tmp/missing.txt",
			wantErr:  ErrInputFileNotFound,
		},
		{
			name:     "permission denied",
			path:     "/etc/shadow",
			err:      ErrInputFilePermission,
			wantPath: "/etc/shadow",
			wantErr:  ErrInputFilePermission,
		},
		{
			name:     "empty file",
			path:     "/tmp/empty.txt",
			err:      ErrInputFileEmpty,
			wantPath: "/tmp/empty.txt",
			wantErr:  ErrInputFileEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewInputFileError(tt.path, tt.err)
			if err.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", err.Path, tt.wantPath)
			}
			if err.Err != tt.wantErr {
				t.Errorf("Err = %v, want %v", err.Err, tt.wantErr)
			}
		})
	}
}

// Helper function to create temporary input file
func createTempInputFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "urls.txt")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	return tmpFile
}
