package warplib

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// newChecksumServer creates an HTTP test server that provides Content-Length,
// Accept-Ranges, and appropriate checksum headers based on the algorithm.
// It handles range requests similar to newRangeServer.
func newChecksumServer(t *testing.T, content []byte, algo ChecksumAlgorithm) *httptest.Server {
	t.Helper()

	// Calculate expected checksum based on algorithm
	var checksum []byte
	switch algo {
	case ChecksumMD5:
		hash := md5.Sum(content)
		checksum = hash[:]
	case ChecksumSHA256:
		hash := sha256.Sum256(content)
		checksum = hash[:]
	case ChecksumSHA512:
		hash := sha512.Sum512(content)
		checksum = hash[:]
	default:
		t.Fatalf("unsupported checksum algorithm: %s", algo)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")

		// Set appropriate checksum header
		switch algo {
		case ChecksumMD5:
			w.Header().Set("Content-MD5", base64.StdEncoding.EncodeToString(checksum))
		case ChecksumSHA256:
			w.Header().Set("Digest", "sha-256="+base64.StdEncoding.EncodeToString(checksum))
		case ChecksumSHA512:
			w.Header().Set("Digest", "sha-512="+base64.StdEncoding.EncodeToString(checksum))
		}

		// Handle range requests
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}

		rangeHeader := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(content) - 1
		if parts[1] != "" {
			if e, err := strconv.Atoi(parts[1]); err == nil {
				end = e
			}
		}

		if start > end || start < 0 || end >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		chunk := content[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
}

// newChecksumServerWrong creates a server that provides an incorrect checksum
func newChecksumServerWrong(t *testing.T, content []byte, algo ChecksumAlgorithm) *httptest.Server {
	t.Helper()

	// Create a deliberately wrong checksum (all zeros)
	var wrongChecksum []byte
	switch algo {
	case ChecksumMD5:
		wrongChecksum = make([]byte, 16)
	case ChecksumSHA256:
		wrongChecksum = make([]byte, 32)
	case ChecksumSHA512:
		wrongChecksum = make([]byte, 64)
	default:
		t.Fatalf("unsupported checksum algorithm: %s", algo)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")

		// Set wrong checksum header
		switch algo {
		case ChecksumMD5:
			w.Header().Set("Content-MD5", base64.StdEncoding.EncodeToString(wrongChecksum))
		case ChecksumSHA256:
			w.Header().Set("Digest", "sha-256="+base64.StdEncoding.EncodeToString(wrongChecksum))
		case ChecksumSHA512:
			w.Header().Set("Digest", "sha-512="+base64.StdEncoding.EncodeToString(wrongChecksum))
		}

		// Handle range requests
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}

		rangeHeader := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(content) - 1
		if parts[1] != "" {
			if e, err := strconv.Atoi(parts[1]); err == nil {
				end = e
			}
		}

		if start > end || start < 0 || end >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		chunk := content[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
}

// newChecksumServerMultiple creates a server that provides multiple checksum algorithms
func newChecksumServerMultiple(t *testing.T, content []byte) *httptest.Server {
	t.Helper()

	// Calculate checksums
	md5Hash := md5.Sum(content)
	sha256Hash := sha256.Sum256(content)
	sha512Hash := sha512.Sum512(content)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")

		// Set multiple checksums
		w.Header().Set("Content-MD5", base64.StdEncoding.EncodeToString(md5Hash[:]))
		digestValue := "sha-512=" + base64.StdEncoding.EncodeToString(sha512Hash[:]) +
			",sha-256=" + base64.StdEncoding.EncodeToString(sha256Hash[:])
		w.Header().Set("Digest", digestValue)

		// Handle range requests
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}

		rangeHeader := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(content) - 1
		if parts[1] != "" {
			if e, err := strconv.Atoi(parts[1]); err == nil {
				end = e
			}
		}

		if start > end || start < 0 || end >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		chunk := content[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
}

func TestDownloaderChecksumValidation_SHA256_Match(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("x"), 64*1024)
	srv := newChecksumServer(t, content, ChecksumSHA256)
	defer srv.Close()

	var validationCalled bool
	var result ChecksumResult

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
				result = r
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !validationCalled {
		t.Error("ChecksumValidationHandler was not called")
	}
	if !result.Match {
		t.Errorf("Expected checksum match, got mismatch. Expected: %x, Actual: %x",
			result.Expected, result.Actual)
	}
	if result.Algorithm != ChecksumSHA256 {
		t.Errorf("Expected algorithm %s, got %s", ChecksumSHA256, result.Algorithm)
	}

	// Verify downloaded content
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded content mismatch")
	}
}

func TestDownloaderChecksumValidation_SHA512_Match(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("y"), 128*1024)
	srv := newChecksumServer(t, content, ChecksumSHA512)
	defer srv.Close()

	var validationCalled bool
	var result ChecksumResult

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
				result = r
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !validationCalled {
		t.Error("ChecksumValidationHandler was not called")
	}
	if !result.Match {
		t.Errorf("Expected checksum match, got mismatch. Expected: %x, Actual: %x",
			result.Expected, result.Actual)
	}
	if result.Algorithm != ChecksumSHA512 {
		t.Errorf("Expected algorithm %s, got %s", ChecksumSHA512, result.Algorithm)
	}

	// Verify downloaded content
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded content mismatch")
	}
}

func TestDownloaderChecksumValidation_MD5_Match(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("z"), 64*1024)
	srv := newChecksumServer(t, content, ChecksumMD5)
	defer srv.Close()

	var validationCalled bool
	var result ChecksumResult

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
				result = r
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !validationCalled {
		t.Error("ChecksumValidationHandler was not called")
	}
	if !result.Match {
		t.Errorf("Expected checksum match, got mismatch. Expected: %x, Actual: %x",
			result.Expected, result.Actual)
	}
	if result.Algorithm != ChecksumMD5 {
		t.Errorf("Expected algorithm %s, got %s", ChecksumMD5, result.Algorithm)
	}

	// Verify downloaded content
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded content mismatch")
	}
}

func TestDownloaderChecksumValidation_Mismatch(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("a"), 64*1024)
	srv := newChecksumServerWrong(t, content, ChecksumSHA256)
	defer srv.Close()

	var validationCalled bool
	var result ChecksumResult

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
				result = r
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	err = d.Start()
	if err == nil {
		t.Fatal("Expected error due to checksum mismatch, got nil")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("Expected ErrChecksumMismatch, got: %v", err)
	}

	if !validationCalled {
		t.Error("ChecksumValidationHandler was not called")
	}
	if result.Match {
		t.Error("Expected checksum mismatch, got match")
	}
	if result.Algorithm != ChecksumSHA256 {
		t.Errorf("Expected algorithm %s, got %s", ChecksumSHA256, result.Algorithm)
	}
}

func TestDownloaderChecksumValidation_MismatchNoFail(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("b"), 64*1024)
	srv := newChecksumServerWrong(t, content, ChecksumSHA256)
	defer srv.Close()

	var validationCalled bool
	var result ChecksumResult

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: false, // Don't fail on mismatch
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
				result = r
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	// Should succeed despite mismatch
	if err := d.Start(); err != nil {
		t.Fatalf("Start should succeed with FailOnMismatch=false: %v", err)
	}

	if !validationCalled {
		t.Error("ChecksumValidationHandler was not called")
	}
	if result.Match {
		t.Error("Expected checksum mismatch, got match")
	}
	if result.Algorithm != ChecksumSHA256 {
		t.Errorf("Expected algorithm %s, got %s", ChecksumSHA256, result.Algorithm)
	}

	// Verify content was downloaded despite mismatch
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded content mismatch")
	}
}

func TestDownloaderChecksumValidation_NoChecksum_Silent(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("c"), 64*1024)
	// Use regular range server without checksum headers
	srv := newRangeServer(t, content)
	defer srv.Close()

	var validationCalled bool

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	// Should succeed without validation
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if validationCalled {
		t.Error("ChecksumValidationHandler should not be called when no checksum provided")
	}

	// Verify content downloaded correctly
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded content mismatch")
	}
}

func TestDownloaderChecksumValidation_Disabled(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("d"), 64*1024)
	srv := newChecksumServer(t, content, ChecksumSHA256)
	defer srv.Close()

	var validationCalled bool

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        false, // Disabled
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	// Should succeed without validation
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if validationCalled {
		t.Error("ChecksumValidationHandler should not be called when validation is disabled")
	}

	// Verify content downloaded correctly
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded content mismatch")
	}
}

func TestDownloaderChecksumValidation_PreferSHA512(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("e"), 64*1024)
	srv := newChecksumServerMultiple(t, content)
	defer srv.Close()

	var validationCalled bool
	var result ChecksumResult

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
				result = r
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !validationCalled {
		t.Error("ChecksumValidationHandler was not called")
	}
	if !result.Match {
		t.Errorf("Expected checksum match, got mismatch")
	}
	// Verify SHA-512 was chosen over SHA-256 and MD5
	if result.Algorithm != ChecksumSHA512 {
		t.Errorf("Expected algorithm %s (strongest), got %s", ChecksumSHA512, result.Algorithm)
	}

	// Verify downloaded content
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded content mismatch")
	}
}

func TestDownloaderChecksumValidation_ChecksumProgress(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("f"), 256*1024) // Larger file for progress tracking
	srv := newChecksumServer(t, content, ChecksumSHA256)
	defer srv.Close()

	var progressCalled bool
	var lastBytesHashed int64

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumProgressHandler: func(bytesHashed int64) {
				progressCalled = true
				if bytesHashed > lastBytesHashed {
					lastBytesHashed = bytesHashed
				}
			},
			ChecksumValidationHandler: func(r ChecksumResult) {
				// Validation handler
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !progressCalled {
		t.Error("ChecksumProgressHandler was not called")
	}
	if lastBytesHashed != int64(len(content)) {
		t.Errorf("Expected final bytes hashed to be %d, got %d", len(content), lastBytesHashed)
	}
}

func TestDownloaderChecksumValidation_NilConfig_UsesDefault(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("g"), 64*1024)
	srv := newChecksumServer(t, content, ChecksumSHA256)
	defer srv.Close()

	var validationCalled bool

	// Don't provide ChecksumConfig - should use defaults (enabled=true, fail=true)
	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		ChecksumConfig:    nil, // Use defaults
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Default behavior should enable validation
	if !validationCalled {
		t.Error("ChecksumValidationHandler should be called with default config")
	}
}

func TestDownloaderChecksumValidation_MultipartDownload(t *testing.T) {

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Larger content to trigger multipart download
	content := bytes.Repeat([]byte("h"), 512*1024)
	srv := newChecksumServer(t, content, ChecksumSHA256)
	defer srv.Close()

	var validationCalled bool
	var result ChecksumResult

	d, err := NewDownloader(srv.Client(), srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		MaxConnections:    4,
		MaxSegments:       4,
		ChecksumConfig: &ChecksumConfig{
			Enabled:        true,
			FailOnMismatch: true,
		},
		Handlers: &Handlers{
			ChecksumValidationHandler: func(r ChecksumResult) {
				validationCalled = true
				result = r
			},
		},
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !validationCalled {
		t.Error("ChecksumValidationHandler was not called for multipart download")
	}
	if !result.Match {
		t.Errorf("Expected checksum match for multipart download, got mismatch")
	}

	// Verify downloaded content is correct
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded content mismatch in multipart download")
	}
}
