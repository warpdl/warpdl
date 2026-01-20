package cmd

import (
	"errors"
	"os"
	"testing"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

// MockClient implements the necessary client interface for testing batch downloads.
type MockClient struct {
	// DownloadFunc allows customizing download behavior per test
	DownloadFunc func(url, fileName, dir string, opts *warpcli.DownloadOpts) (*common.DownloadResponse, error)
	// Calls tracks all Download invocations
	Calls []MockDownloadCall
}

type MockDownloadCall struct {
	URL      string
	FileName string
	Dir      string
	Opts     *warpcli.DownloadOpts
}

func (m *MockClient) Download(url, fileName, dir string, opts *warpcli.DownloadOpts) (*common.DownloadResponse, error) {
	m.Calls = append(m.Calls, MockDownloadCall{
		URL:      url,
		FileName: fileName,
		Dir:      dir,
		Opts:     opts,
	})
	if m.DownloadFunc != nil {
		return m.DownloadFunc(url, fileName, dir, opts)
	}
	// Default: success
	return &common.DownloadResponse{
		DownloadId: "mock-" + url,
		FileName:   "file.zip",
	}, nil
}

func (m *MockClient) Close() error {
	return nil
}

func TestDownloadBatch_TwoURLsFromFile(t *testing.T) {
	// Create input file with 2 URLs
	content := `https://example.com/file1.zip
https://example.com/file2.tar.gz`
	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	mock := &MockClient{}

	// This should call DownloadBatch which doesn't exist yet
	result, err := DownloadBatch(mock, tmpFile, nil, &BatchDownloadOpts{
		DownloadDir: "/tmp/downloads",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both URLs were downloaded
	if len(mock.Calls) != 2 {
		t.Errorf("expected 2 download calls, got %d", len(mock.Calls))
	}

	// Verify result tracking
	if result.Succeeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", result.Succeeded)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}
}

func TestDownloadBatch_MixFileAndDirectURLs(t *testing.T) {
	// Create input file with 1 URL
	content := `https://example.com/file1.zip`
	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	mock := &MockClient{}

	// Direct URL args in addition to file
	directURLs := []string{
		"https://example.com/direct1.zip",
		"https://example.com/direct2.zip",
	}

	result, err := DownloadBatch(mock, tmpFile, directURLs, &BatchDownloadOpts{
		DownloadDir: "/tmp/downloads",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all 3 URLs were downloaded (1 from file + 2 direct)
	if len(mock.Calls) != 3 {
		t.Errorf("expected 3 download calls, got %d", len(mock.Calls))
	}

	if result.Total != 3 {
		t.Errorf("expected total 3, got %d", result.Total)
	}
}

func TestDownloadBatch_ContinueOnError(t *testing.T) {
	// Create input file with 3 URLs
	content := `https://example.com/file1.zip
https://example.com/fail.zip
https://example.com/file3.zip`
	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	// Mock client that fails on specific URL
	mock := &MockClient{
		DownloadFunc: func(url, fileName, dir string, opts *warpcli.DownloadOpts) (*common.DownloadResponse, error) {
			if url == "https://example.com/fail.zip" {
				return nil, errors.New("download failed: connection refused")
			}
			return &common.DownloadResponse{
				DownloadId: "mock-id",
				FileName:   "file.zip",
			}, nil
		},
	}

	result, err := DownloadBatch(mock, tmpFile, nil, &BatchDownloadOpts{
		DownloadDir: "/tmp/downloads",
	})
	// Should not return error - batch continues on individual failures
	if err != nil {
		t.Fatalf("batch should continue on individual errors, got: %v", err)
	}

	// All 3 URLs should have been attempted
	if len(mock.Calls) != 3 {
		t.Errorf("expected 3 download attempts, got %d", len(mock.Calls))
	}

	// Verify result tracking
	if result.Succeeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", result.Succeeded)
	}
	if result.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", result.Failed)
	}

	// Check error was tracked
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error tracked, got %d", len(result.Errors))
	}
}

func TestDownloadBatch_EmptyFile(t *testing.T) {
	// Create empty input file
	content := ``
	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	mock := &MockClient{}

	_, err := DownloadBatch(mock, tmpFile, nil, &BatchDownloadOpts{
		DownloadDir: "/tmp/downloads",
	})

	// Empty file should return an error
	if err == nil {
		t.Fatal("expected error for empty input file, got nil")
	}

	// Verify it's the correct error type
	var inputFileErr *InputFileError
	if !errors.As(err, &inputFileErr) {
		t.Errorf("expected InputFileError, got %T", err)
	}
	if !errors.Is(inputFileErr.Err, ErrInputFileEmpty) {
		t.Errorf("expected ErrInputFileEmpty, got %v", inputFileErr.Err)
	}

	// No downloads should have been attempted
	if len(mock.Calls) != 0 {
		t.Errorf("expected 0 download calls, got %d", len(mock.Calls))
	}
}

func TestDownloadBatch_OnlyDirectURLs(t *testing.T) {
	mock := &MockClient{}

	// No input file, only direct URLs
	directURLs := []string{
		"https://example.com/direct1.zip",
	}

	result, err := DownloadBatch(mock, "", directURLs, &BatchDownloadOpts{
		DownloadDir: "/tmp/downloads",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.Calls) != 1 {
		t.Errorf("expected 1 download call, got %d", len(mock.Calls))
	}
	if result.Succeeded != 1 {
		t.Errorf("expected 1 succeeded, got %d", result.Succeeded)
	}
}

// Tests for BatchResult helper methods

func TestBatchResult_NewBatchResult(t *testing.T) {
	result := NewBatchResult(5)

	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
	}
	if result.Succeeded != 0 {
		t.Errorf("expected succeeded 0, got %d", result.Succeeded)
	}
	if result.Failed != 0 {
		t.Errorf("expected failed 0, got %d", result.Failed)
	}
	if result.Errors == nil {
		t.Error("expected Errors slice to be initialized")
	}
}

func TestBatchResult_AddSuccess(t *testing.T) {
	result := NewBatchResult(3)
	result.AddSuccess()
	result.AddSuccess()

	if result.Succeeded != 2 {
		t.Errorf("expected succeeded 2, got %d", result.Succeeded)
	}
}

func TestBatchResult_AddError(t *testing.T) {
	result := NewBatchResult(3)
	result.AddError("https://example.com/fail.zip", errors.New("connection refused"))

	if result.Failed != 1 {
		t.Errorf("expected failed 1, got %d", result.Failed)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if result.Errors[0].URL != "https://example.com/fail.zip" {
		t.Errorf("expected URL 'https://example.com/fail.zip', got '%s'", result.Errors[0].URL)
	}
	if result.Errors[0].Reason != "connection refused" {
		t.Errorf("expected reason 'connection refused', got '%s'", result.Errors[0].Reason)
	}
}

func TestBatchResult_IsSuccess(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() *BatchResult
		expected bool
	}{
		{
			name: "all succeeded",
			setup: func() *BatchResult {
				r := NewBatchResult(2)
				r.AddSuccess()
				r.AddSuccess()
				return r
			},
			expected: true,
		},
		{
			name: "some failed",
			setup: func() *BatchResult {
				r := NewBatchResult(2)
				r.AddSuccess()
				r.AddError("url", errors.New("failed"))
				return r
			},
			expected: false,
		},
		{
			name: "all failed",
			setup: func() *BatchResult {
				r := NewBatchResult(1)
				r.AddError("url", errors.New("failed"))
				return r
			},
			expected: false,
		},
		{
			name: "empty",
			setup: func() *BatchResult {
				return NewBatchResult(0)
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.setup()
			if got := result.IsSuccess(); got != tt.expected {
				t.Errorf("IsSuccess() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBatchResult_HasErrors(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() *BatchResult
		expected bool
	}{
		{
			name: "no errors",
			setup: func() *BatchResult {
				r := NewBatchResult(2)
				r.AddSuccess()
				r.AddSuccess()
				return r
			},
			expected: false,
		},
		{
			name: "has errors",
			setup: func() *BatchResult {
				r := NewBatchResult(2)
				r.AddSuccess()
				r.AddError("url", errors.New("failed"))
				return r
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.setup()
			if got := result.HasErrors(); got != tt.expected {
				t.Errorf("HasErrors() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBatchResult_String(t *testing.T) {
	t.Run("success only", func(t *testing.T) {
		result := NewBatchResult(2)
		result.AddSuccess()
		result.AddSuccess()

		s := result.String()
		if !contains(s, "Total URLs: 2") {
			t.Errorf("expected 'Total URLs: 2' in output, got: %s", s)
		}
		if !contains(s, "Succeeded:  2") {
			t.Errorf("expected 'Succeeded:  2' in output, got: %s", s)
		}
		if !contains(s, "Failed:     0") {
			t.Errorf("expected 'Failed:     0' in output, got: %s", s)
		}
		if contains(s, "Failed downloads:") {
			t.Errorf("should not contain 'Failed downloads:' section when no errors")
		}
	})

	t.Run("with failures", func(t *testing.T) {
		result := NewBatchResult(3)
		result.AddSuccess()
		result.AddSuccess()
		result.AddError("https://example.com/fail.zip", errors.New("404 Not Found"))

		s := result.String()
		if !contains(s, "Total URLs: 3") {
			t.Errorf("expected 'Total URLs: 3' in output, got: %s", s)
		}
		if !contains(s, "Failed:     1") {
			t.Errorf("expected 'Failed:     1' in output, got: %s", s)
		}
		if !contains(s, "Failed downloads:") {
			t.Errorf("expected 'Failed downloads:' section")
		}
		if !contains(s, "https://example.com/fail.zip: 404 Not Found") {
			t.Errorf("expected error details in output, got: %s", s)
		}
	})
}

func TestNewBatchError(t *testing.T) {
	err := errors.New("connection timeout")
	be := NewBatchError("https://example.com/file.zip", err)

	if be.URL != "https://example.com/file.zip" {
		t.Errorf("expected URL 'https://example.com/file.zip', got '%s'", be.URL)
	}
	if be.Reason != "connection timeout" {
		t.Errorf("expected reason 'connection timeout', got '%s'", be.Reason)
	}
}

func TestSkippedURL(t *testing.T) {
	s := SkippedURL{
		LineNumber: 5,
		Content:    "ftp://example.com/file.zip",
		Reason:     "URL must start with http:// or https://",
	}

	if s.LineNumber != 5 {
		t.Errorf("expected LineNumber 5, got %d", s.LineNumber)
	}
	if s.Content != "ftp://example.com/file.zip" {
		t.Errorf("expected Content 'ftp://example.com/file.zip', got %q", s.Content)
	}
	if s.Reason != "URL must start with http:// or https://" {
		t.Errorf("expected Reason about http/https, got %q", s.Reason)
	}
}

func TestBatchResult_StringWithSkippedURLs(t *testing.T) {
	result := NewBatchResult(2)
	result.AddSuccess()
	result.AddSuccess()
	result.SkippedURLs = []SkippedURL{
		{LineNumber: 3, Content: "ftp://example.com/file.zip", Reason: "URL must start with http:// or https://"},
		{LineNumber: 5, Content: "magnet:?xt=abc", Reason: "URL must start with http:// or https://"},
	}

	s := result.String()

	// Check skipped count
	if !contains(s, "Skipped:    2 (invalid URLs)") {
		t.Errorf("expected 'Skipped:    2 (invalid URLs)' in output, got: %s", s)
	}

	// Check warning header
	if !contains(s, "Warning - Skipped URLs with invalid scheme:") {
		t.Errorf("expected warning header in output, got: %s", s)
	}

	// Check line number and content are included
	if !contains(s, "Line 3: ftp://example.com/file.zip") {
		t.Errorf("expected 'Line 3: ftp://example.com/file.zip' in output, got: %s", s)
	}
	if !contains(s, "Line 5: magnet:?xt=abc") {
		t.Errorf("expected 'Line 5: magnet:?xt=abc' in output, got: %s", s)
	}
}

func TestDownloadBatch_WithSkippedURLs(t *testing.T) {
	// Create input file with mix of valid and invalid URLs
	content := `https://example.com/file1.zip
ftp://example.com/invalid.zip
https://example.com/file2.zip
magnet:?xt=urn:btih:abc123`
	tmpFile := createTempInputFile(t, content)
	defer os.Remove(tmpFile)

	mock := &MockClient{}

	result, err := DownloadBatch(mock, tmpFile, nil, &BatchDownloadOpts{
		DownloadDir: "/tmp/downloads",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 2 valid URLs should be downloaded
	if len(mock.Calls) != 2 {
		t.Errorf("expected 2 download calls (valid URLs only), got %d", len(mock.Calls))
	}

	// Result should track 2 valid URLs
	if result.Total != 2 {
		t.Errorf("expected total 2 (valid URLs), got %d", result.Total)
	}
	if result.Succeeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", result.Succeeded)
	}

	// 2 invalid URLs should be tracked as skipped
	if len(result.SkippedURLs) != 2 {
		t.Errorf("expected 2 skipped URLs, got %d", len(result.SkippedURLs))
	}

	// Verify skipped URLs have correct line numbers
	foundFTP := false
	foundMagnet := false
	for _, s := range result.SkippedURLs {
		if s.LineNumber == 2 && s.Content == "ftp://example.com/invalid.zip" {
			foundFTP = true
		}
		if s.LineNumber == 4 && s.Content == "magnet:?xt=urn:btih:abc123" {
			foundMagnet = true
		}
	}
	if !foundFTP {
		t.Error("did not find expected skipped URL for ftp:// at line 2")
	}
	if !foundMagnet {
		t.Error("did not find expected skipped URL for magnet: at line 4")
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
