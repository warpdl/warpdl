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

	result, err := DownloadBatch(mock, tmpFile, nil, &BatchDownloadOpts{
		DownloadDir: "/tmp/downloads",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No URLs to download
	if len(mock.Calls) != 0 {
		t.Errorf("expected 0 download calls, got %d", len(mock.Calls))
	}
	if result.Total != 0 {
		t.Errorf("expected total 0, got %d", result.Total)
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
