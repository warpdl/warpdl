package warplib

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestSetContentLengthNegativeValues tests that setContentLength properly validates
// negative content length values.
func TestSetContentLengthNegativeValues(t *testing.T) {
	tests := []struct {
		name        string
		length      int64
		shouldError bool
		errorType   error
	}{
		{
			name:        "valid: -1 for unknown size",
			length:      -1,
			shouldError: false,
		},
		{
			name:        "invalid: zero content length",
			length:      0,
			shouldError: true,
			errorType:   ErrContentLengthInvalid,
		},
		{
			name:        "invalid: -2 negative value",
			length:      -2,
			shouldError: true,
			errorType:   ErrContentLengthInvalid,
		},
		{
			name:        "invalid: -500 negative value",
			length:      -500,
			shouldError: true,
			errorType:   ErrContentLengthInvalid,
		},
		{
			name:        "invalid: min int64 value",
			length:      math.MinInt64,
			shouldError: true,
			errorType:   ErrContentLengthInvalid,
		},
		{
			name:        "valid: 1024 bytes positive",
			length:      1024,
			shouldError: false,
		},
		{
			name:        "valid: 1GB positive",
			length:      GB,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			if err := SetConfigDir(base); err != nil {
				t.Fatalf("SetConfigDir: %v", err)
			}

			hash := "test_hash"
			if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}

			d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", ContentLength(1*MB), &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          "test.bin",
			})
			if err != nil {
				t.Fatalf("initDownloader: %v", err)
			}
			defer d.Close()

			err = d.setContentLength(tt.length)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error for content length %d, got nil", tt.length)
				} else if tt.errorType != nil && !errors.Is(err, tt.errorType) {
					t.Errorf("expected error to wrap %v, got %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for content length %d: %v", tt.length, err)
				}
				// Verify downloader flags for -1 case
				if tt.length == -1 {
					if d.resumable != false {
						t.Error("expected resumable=false for unknown size")
					}
					if d.numBaseParts != 1 {
						t.Errorf("expected numBaseParts=1, got %d", d.numBaseParts)
					}
					if d.maxConn != 1 {
						t.Errorf("expected maxConn=1, got %d", d.maxConn)
					}
					if d.maxParts != 1 {
						t.Errorf("expected maxParts=1, got %d", d.maxParts)
					}
				}
			}
		})
	}
}

// TestSetContentLengthMaxFileSize tests that setContentLength enforces max file size limits.
func TestSetContentLengthMaxFileSize(t *testing.T) {
	tests := []struct {
		name        string
		fileSize    int64
		maxFileSize int64
		shouldError bool
		errorType   error
	}{
		{
			name:        "valid: 50GB within default 100GB limit",
			fileSize:    50 * GB,
			maxFileSize: 0, // 0 means use default 100GB
			shouldError: false,
		},
		{
			name:        "valid: exactly at 100GB default limit",
			fileSize:    100 * GB,
			maxFileSize: 0,
			shouldError: false,
		},
		{
			name:        "invalid: exceeds 100GB default limit",
			fileSize:    101 * GB,
			maxFileSize: 0,
			shouldError: true,
			errorType:   ErrFileTooLarge,
		},
		{
			name:        "valid: 500MB within custom 1GB limit",
			fileSize:    500 * MB,
			maxFileSize: 1 * GB,
			shouldError: false,
		},
		{
			name:        "valid: exactly at custom 1GB limit",
			fileSize:    1 * GB,
			maxFileSize: 1 * GB,
			shouldError: false,
		},
		{
			name:        "invalid: exceeds custom 1GB limit",
			fileSize:    1*GB + 1,
			maxFileSize: 1 * GB,
			shouldError: true,
			errorType:   ErrFileTooLarge,
		},
		{
			name:        "valid: -1 unlimited allows 500GB",
			fileSize:    500 * GB,
			maxFileSize: -1, // -1 means unlimited
			shouldError: false,
		},
		{
			name:        "valid: -1 unlimited allows 5TB",
			fileSize:    5 * TB,
			maxFileSize: -1,
			shouldError: false,
		},
		{
			name:        "valid: custom 10GB limit with 5GB file",
			fileSize:    5 * GB,
			maxFileSize: 10 * GB,
			shouldError: false,
		},
		{
			name:        "invalid: custom 10GB limit with 11GB file",
			fileSize:    11 * GB,
			maxFileSize: 10 * GB,
			shouldError: true,
			errorType:   ErrFileTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			if err := SetConfigDir(base); err != nil {
				t.Fatalf("SetConfigDir: %v", err)
			}

			hash := "test_hash"
			if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}

			d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", ContentLength(1*MB), &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          "test.bin",
				MaxFileSize:       tt.maxFileSize,
			})
			if err != nil {
				t.Fatalf("initDownloader: %v", err)
			}
			defer d.Close()

			err = d.setContentLength(tt.fileSize)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error for file size %d with max %d, got nil", tt.fileSize, tt.maxFileSize)
				} else if tt.errorType != nil && !errors.Is(err, tt.errorType) {
					t.Errorf("expected error to wrap %v, got %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for file size %d with max %d: %v", tt.fileSize, tt.maxFileSize, err)
				}
			}
		})
	}
}

// TestNewDownloaderRejectsInvalidContentLength tests that NewDownloader rejects
// servers that return invalid Content-Length headers via integration test.
// Note: Go's http package treats invalid negative Content-Length as -1 (unknown),
// so we can only test 0 which is rejected by our validation.
func TestNewDownloaderRejectsInvalidContentLength(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	tests := []struct {
		name          string
		contentLength string
		shouldError   bool
		errorType     error
	}{
		{
			name:          "invalid: zero content length",
			contentLength: "0",
			shouldError:   true,
			errorType:     ErrContentLengthInvalid,
		},
		// Note: Go's http.Response.ContentLength treats invalid values (like -2, -500)
		// as -1 (unknown size), so we cannot test these cases via integration tests.
		// They are covered by unit tests in TestSetContentLengthNegativeValues.
		{
			name:          "valid: normal file size",
			contentLength: strconv.FormatInt(1*MB, 10),
			shouldError:   false,
		},
		{
			name:          "valid: -1 unknown size",
			contentLength: "-1",
			shouldError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server that returns specific Content-Length
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", tt.contentLength)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			d, err := NewDownloader(server.Client(), server.URL, &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          "test.bin",
			})
			if d != nil {
				defer d.Close()
			}

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error for Content-Length: %s, got nil", tt.contentLength)
				} else if tt.errorType != nil && !errors.Is(err, tt.errorType) {
					t.Errorf("expected error to wrap %v, got %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for Content-Length: %s: %v", tt.contentLength, err)
				}
			}
		})
	}
}

// TestNewDownloaderRejectsFileTooLarge tests that NewDownloader rejects files
// that exceed the configured max file size via integration test.
func TestNewDownloaderRejectsFileTooLarge(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	tests := []struct {
		name        string
		fileSize    int64
		maxFileSize int64
		shouldError bool
		errorType   error
	}{
		{
			name:        "valid: 50GB within default 100GB",
			fileSize:    50 * GB,
			maxFileSize: 0, // 0 = use default 100GB
			shouldError: false,
		},
		{
			name:        "invalid: 150GB exceeds default 100GB",
			fileSize:    150 * GB,
			maxFileSize: 0,
			shouldError: true,
			errorType:   ErrFileTooLarge,
		},
		{
			name:        "valid: 500MB within custom 1GB",
			fileSize:    500 * MB,
			maxFileSize: 1 * GB,
			shouldError: false,
		},
		{
			name:        "invalid: 2GB exceeds custom 1GB",
			fileSize:    2 * GB,
			maxFileSize: 1 * GB,
			shouldError: true,
			errorType:   ErrFileTooLarge,
		},
		{
			name:        "valid: 10TB with unlimited (-1)",
			fileSize:    10 * TB,
			maxFileSize: -1,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server that returns specific Content-Length
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", strconv.FormatInt(tt.fileSize, 10))
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			d, err := NewDownloader(server.Client(), server.URL, &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          "test.bin",
				MaxFileSize:       tt.maxFileSize,
			})
			if d != nil {
				defer d.Close()
			}

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error for file size %d with max %d, got nil", tt.fileSize, tt.maxFileSize)
				} else if tt.errorType != nil && !errors.Is(err, tt.errorType) {
					t.Errorf("expected error to wrap %v, got %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for file size %d with max %d: %v", tt.fileSize, tt.maxFileSize, err)
				}
			}
		})
	}
}

// TestSetContentLengthBoundaryValues tests edge cases around boundary values.
func TestSetContentLengthBoundaryValues(t *testing.T) {
	tests := []struct {
		name        string
		length      int64
		maxFileSize int64
		shouldError bool
		errorType   error
	}{
		{
			name:        "boundary: 1 byte (minimum valid)",
			length:      1,
			maxFileSize: 0,
			shouldError: false,
		},
		{
			name:        "boundary: max int64 with unlimited",
			length:      math.MaxInt64,
			maxFileSize: -1, // unlimited
			shouldError: false,
		},
		{
			name:        "boundary: -1 (special case unknown)",
			length:      -1,
			maxFileSize: 0,
			shouldError: false,
		},
		{
			name:        "boundary: 0 (invalid)",
			length:      0,
			maxFileSize: 0,
			shouldError: true,
			errorType:   ErrContentLengthInvalid,
		},
		{
			name:        "boundary: -2 (first invalid negative)",
			length:      -2,
			maxFileSize: 0,
			shouldError: true,
			errorType:   ErrContentLengthInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			if err := SetConfigDir(base); err != nil {
				t.Fatalf("SetConfigDir: %v", err)
			}

			hash := "test_hash"
			if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}

			d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", ContentLength(1*MB), &DownloaderOpts{
				DownloadDirectory: base,
				FileName:          "test.bin",
				MaxFileSize:       tt.maxFileSize,
			})
			if err != nil {
				t.Fatalf("initDownloader: %v", err)
			}
			defer d.Close()

			err = d.setContentLength(tt.length)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error for content length %d, got nil", tt.length)
				} else if tt.errorType != nil && !errors.Is(err, tt.errorType) {
					t.Errorf("expected error to wrap %v, got %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for content length %d: %v", tt.length, err)
				}
			}
		})
	}
}
