package cmd

import (
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
