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

// Test 1: ProtocolDownloader interface exists; mock satisfies it
func TestProtocolDownloader_InterfaceExists(t *testing.T) {
	var pd ProtocolDownloader = &mockProtocolDownloader{
		hash:          "abc123",
		fileName:      "test.zip",
		downloadDir:   "/tmp",
		savePath:      "/tmp/test.zip",
		contentLength: ContentLength(1024),
		maxConn:       4,
		maxParts:      8,
	}

	if pd.GetHash() != "abc123" {
		t.Errorf("GetHash: want abc123, got %s", pd.GetHash())
	}
	if pd.GetFileName() != "test.zip" {
		t.Errorf("GetFileName: want test.zip, got %s", pd.GetFileName())
	}
	if pd.GetDownloadDirectory() != "/tmp" {
		t.Errorf("GetDownloadDirectory: want /tmp, got %s", pd.GetDownloadDirectory())
	}
	if pd.GetSavePath() != "/tmp/test.zip" {
		t.Errorf("GetSavePath: want /tmp/test.zip, got %s", pd.GetSavePath())
	}
	if pd.GetContentLength() != ContentLength(1024) {
		t.Errorf("GetContentLength: want 1024, got %v", pd.GetContentLength())
	}
	if pd.GetMaxConnections() != 4 {
		t.Errorf("GetMaxConnections: want 4, got %d", pd.GetMaxConnections())
	}
	if pd.GetMaxParts() != 8 {
		t.Errorf("GetMaxParts: want 8, got %d", pd.GetMaxParts())
	}
}

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

// Test 7: DownloadCapabilities defaults to zero values
func TestDownloadCapabilities_Defaults(t *testing.T) {
	var caps DownloadCapabilities
	if caps.SupportsParallel {
		t.Errorf("SupportsParallel: zero value should be false")
	}
	if caps.SupportsResume {
		t.Errorf("SupportsResume: zero value should be false")
	}
}

// Test 8: ProbeResult defaults
func TestProbeResult_Defaults(t *testing.T) {
	var pr ProbeResult
	if pr.ContentLength != 0 {
		t.Errorf("ContentLength: zero value should be 0 (unknown indicated by -1 when set)")
	}
	if pr.FileName != "" {
		t.Errorf("FileName: zero value should be empty")
	}
	if pr.Resumable {
		t.Errorf("Resumable: zero value should be false")
	}
}

// Test 9: Calling Download() without Probe() returns ErrProbeRequired
func TestProtocolDownloader_DownloadWithoutProbe(t *testing.T) {
	pd := &mockProtocolDownloader{probed: false}
	err := pd.Download(context.Background(), &Handlers{})
	if !errors.Is(err, ErrProbeRequired) {
		t.Errorf("Download without Probe: want ErrProbeRequired, got %v", err)
	}
}

// Test 10: Calling Resume() without Probe() returns ErrProbeRequired
func TestProtocolDownloader_ResumeWithoutProbe(t *testing.T) {
	pd := &mockProtocolDownloader{probed: false}
	err := pd.Resume(context.Background(), map[int64]*ItemPart{}, &Handlers{})
	if !errors.Is(err, ErrProbeRequired) {
		t.Errorf("Resume without Probe: want ErrProbeRequired, got %v", err)
	}
}

// Test 11 & 12: DownloadCapabilities returned by Capabilities()
func TestProtocolDownloader_Capabilities(t *testing.T) {
	pd := &mockProtocolDownloader{
		capabilities: DownloadCapabilities{
			SupportsParallel: true,
			SupportsResume:   true,
		},
	}
	caps := pd.Capabilities()
	if !caps.SupportsParallel {
		t.Errorf("SupportsParallel: want true, got false")
	}
	if !caps.SupportsResume {
		t.Errorf("SupportsResume: want true, got false")
	}
}
