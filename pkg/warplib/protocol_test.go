package warplib

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// Compile-time check that mockProtocolDownloader implements ProtocolDownloader
var _ ProtocolDownloader = (*mockProtocolDownloader)(nil)

// mockProtocolDownloader is a test implementation of ProtocolDownloader
type mockProtocolDownloader struct {
	probed        bool
	stopped       bool
	maxConn       int32
	maxParts      int32
	hash          string
	fileName      string
	downloadDir   string
	savePath      string
	contentLength ContentLength
	capabilities  DownloadCapabilities
	probeResult   ProbeResult
	probeErr      error
	downloadErr   error
	resumeErr     error
}

func (m *mockProtocolDownloader) Probe(_ context.Context) (ProbeResult, error) {
	if m.probeErr != nil {
		return ProbeResult{}, m.probeErr
	}
	m.probed = true
	return m.probeResult, nil
}

func (m *mockProtocolDownloader) Download(_ context.Context, _ *Handlers) error {
	if !m.probed {
		return ErrProbeRequired
	}
	return m.downloadErr
}

func (m *mockProtocolDownloader) Resume(_ context.Context, _ map[int64]*ItemPart, _ *Handlers) error {
	if !m.probed {
		return ErrProbeRequired
	}
	return m.resumeErr
}

func (m *mockProtocolDownloader) Capabilities() DownloadCapabilities {
	return m.capabilities
}

func (m *mockProtocolDownloader) Close() error                    { return nil }
func (m *mockProtocolDownloader) Stop()                           { m.stopped = true }
func (m *mockProtocolDownloader) IsStopped() bool                 { return m.stopped }
func (m *mockProtocolDownloader) GetMaxConnections() int32        { return m.maxConn }
func (m *mockProtocolDownloader) GetMaxParts() int32              { return m.maxParts }
func (m *mockProtocolDownloader) GetHash() string                 { return m.hash }
func (m *mockProtocolDownloader) GetFileName() string             { return m.fileName }
func (m *mockProtocolDownloader) GetDownloadDirectory() string    { return m.downloadDir }
func (m *mockProtocolDownloader) GetSavePath() string             { return m.savePath }
func (m *mockProtocolDownloader) GetContentLength() ContentLength { return m.contentLength }

// Test 2: DownloadError.Error() returns "protocol op: cause" format
func TestDownloadError_ErrorFormat(t *testing.T) {
	cause := errors.New("connection refused")
	de := NewPermanentError("http", "connect", cause)
	want := "http connect: connection refused"
	if de.Error() != want {
		t.Errorf("Error(): want %q, got %q", want, de.Error())
	}
}

// Test 3: DownloadError.Unwrap() returns the cause error
func TestDownloadError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	de := NewPermanentError("http", "probe", cause)
	if !errors.Is(de, cause) {
		t.Errorf("Unwrap: errors.Is should find cause via Unwrap chain")
	}
	var unwrapped *DownloadError
	if errors.As(de, &unwrapped) {
		if unwrapped.Unwrap() != cause {
			t.Errorf("Unwrap(): want cause, got %v", unwrapped.Unwrap())
		}
	} else {
		t.Errorf("errors.As should find *DownloadError")
	}
}

// Test 4: DownloadError.IsTransient() — permanent
func TestDownloadError_IsTransient_Permanent(t *testing.T) {
	de := NewPermanentError("http", "download", errors.New("404 not found"))
	if de.IsTransient() {
		t.Errorf("IsTransient: permanent error should return false")
	}
}

// Test 5: DownloadError.IsTransient() — transient
func TestDownloadError_IsTransient_Transient(t *testing.T) {
	de := NewTransientError("http", "connect", errors.New("timeout"))
	if !de.IsTransient() {
		t.Errorf("IsTransient: transient error should return true")
	}
}

// Test 6: errors.As works for wrapped DownloadError
func TestDownloadError_ErrorsAs(t *testing.T) {
	cause := errors.New("network timeout")
	de := NewTransientError("http", "download", cause)
	wrapped := fmt.Errorf("outer: %w", de)

	var target *DownloadError
	if !errors.As(wrapped, &target) {
		t.Errorf("errors.As should unwrap to *DownloadError")
	}
	if target.Protocol != "http" {
		t.Errorf("Protocol: want http, got %s", target.Protocol)
	}
	if target.Op != "download" {
		t.Errorf("Op: want download, got %s", target.Op)
	}
	if !target.IsTransient() {
		t.Errorf("IsTransient: should be true")
	}
}
