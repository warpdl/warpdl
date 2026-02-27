package warplib

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

// ---- Mock FTP Server Infrastructure ----

// testFTPDriver implements ftpserver.MainDriver for testing.
type testFTPDriver struct {
	fs afero.Fs
}

func (d *testFTPDriver) GetSettings() (*ftpserver.Settings, error) {
	s := &ftpserver.Settings{
		// Use random port by creating a listener first; we set ListenAddr to ":0"
		// The actual address is obtained after starting.
		ListenAddr:  ":0",
		IdleTimeout: 30,
	}
	return s, nil
}

func (d *testFTPDriver) ClientConnected(_ ftpserver.ClientContext) (string, error) {
	return "Welcome to test FTP server", nil
}

func (d *testFTPDriver) ClientDisconnected(_ ftpserver.ClientContext) {}

func (d *testFTPDriver) AuthUser(_ ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if user == "anonymous" && pass == "anonymous" {
		return afero.NewBasePathFs(d.fs, "/"), nil
	}
	if user == "testuser" && pass == "testpass" {
		return afero.NewBasePathFs(d.fs, "/"), nil
	}
	return nil, fmt.Errorf("invalid credentials")
}

func (d *testFTPDriver) GetTLSConfig() (*tls.Config, error) {
	return nil, nil
}

// startMockFTPServer starts a mock FTP server with pre-populated test files.
// Returns the server address (host:port) and a cleanup function.
func startMockFTPServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()

	memFs := afero.NewMemMapFs()

	// Create test files
	testContent := bytes.Repeat([]byte{0xAB}, 1024)
	if err := afero.WriteFile(memFs, "/pub/testfile.bin", testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	largeContent := bytes.Repeat([]byte{0xCD}, 65536)
	if err := afero.WriteFile(memFs, "/pub/largefile.bin", largeContent, 0644); err != nil {
		t.Fatalf("failed to create large test file: %v", err)
	}

	driver := &testFTPDriver{
		fs: memFs,
	}

	// Create a listener on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	// Use the wrapper driver that provides a pre-created listener
	driverWithListener := &testFTPDriverWithListener{
		testFTPDriver: driver,
		listener:      listener,
	}

	server := ftpserver.NewFtpServer(driverWithListener)

	go func() {
		if err := server.ListenAndServe(); err != nil {
			// Server stopped - this is expected during cleanup
		}
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	addr = listener.Addr().String()
	cleanup = func() {
		server.Stop()
	}
	return
}

// testFTPDriverWithListener wraps testFTPDriver to provide a pre-created listener.
type testFTPDriverWithListener struct {
	*testFTPDriver
	listener net.Listener
}

func (d *testFTPDriverWithListener) GetSettings() (*ftpserver.Settings, error) {
	s := &ftpserver.Settings{
		Listener:    d.listener,
		IdleTimeout: 30,
	}
	return s, nil
}

// ---- Test Cases ----

// Batch 1: Factory + Auth + Capabilities (no mock server needed for most)

func TestFTPFactory(t *testing.T) {
	t.Run("creates downloader with correct fields", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://localhost:2121/pub/file.iso", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ftpd := pd.(*ftpProtocolDownloader)
		if ftpd.host != "localhost:2121" {
			t.Errorf("host = %q, want %q", ftpd.host, "localhost:2121")
		}
		if ftpd.ftpPath != "/pub/file.iso" {
			t.Errorf("ftpPath = %q, want %q", ftpd.ftpPath, "/pub/file.iso")
		}
		if ftpd.fileName != "file.iso" {
			t.Errorf("fileName = %q, want %q", ftpd.fileName, "file.iso")
		}
	})

	t.Run("rejects empty path", func(t *testing.T) {
		_, err := newFTPProtocolDownloader("ftp://host/", nil)
		if err == nil {
			t.Fatal("expected error for root path")
		}
	})

	t.Run("rejects no path", func(t *testing.T) {
		_, err := newFTPProtocolDownloader("ftp://host", nil)
		if err == nil {
			t.Fatal("expected error for empty path")
		}
	})

	t.Run("uses port 2121 when specified", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://host:2121/file.iso", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ftpd := pd.(*ftpProtocolDownloader)
		if ftpd.host != "host:2121" {
			t.Errorf("host = %q, want %q", ftpd.host, "host:2121")
		}
	})

	t.Run("defaults to port 21", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://host/file.iso", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ftpd := pd.(*ftpProtocolDownloader)
		if ftpd.host != "host:21" {
			t.Errorf("host = %q, want %q", ftpd.host, "host:21")
		}
	})

	t.Run("uses explicit FileName from opts", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://host/pub/file.iso", &DownloaderOpts{
			FileName: "custom.iso",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if pd.GetFileName() != "custom.iso" {
			t.Errorf("GetFileName() = %q, want %q", pd.GetFileName(), "custom.iso")
		}
	})
}

func TestFTPAnonymousLogin(t *testing.T) {
	t.Run("no userinfo defaults to anonymous", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://host/file", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ftpd := pd.(*ftpProtocolDownloader)
		if ftpd.user != "anonymous" {
			t.Errorf("user = %q, want %q", ftpd.user, "anonymous")
		}
		if ftpd.password != "anonymous" {
			t.Errorf("password = %q, want %q", ftpd.password, "anonymous")
		}
	})

	t.Run("probe with anonymous credentials against mock server", func(t *testing.T) {
		addr, cleanup := startMockFTPServer(t)
		defer cleanup()

		pd, err := newFTPProtocolDownloader(fmt.Sprintf("ftp://%s/pub/testfile.bin", addr), nil)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}

		probe, err := pd.Probe(context.Background())
		if err != nil {
			t.Fatalf("Probe error: %v", err)
		}
		if probe.ContentLength != 1024 {
			t.Errorf("ContentLength = %d, want 1024", probe.ContentLength)
		}
	})
}

func TestFTPCredentialAuth(t *testing.T) {
	t.Run("extracts credentials from URL", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://testuser:testpass@host/file", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ftpd := pd.(*ftpProtocolDownloader)
		if ftpd.user != "testuser" {
			t.Errorf("user = %q, want %q", ftpd.user, "testuser")
		}
		if ftpd.password != "testpass" {
			t.Errorf("password = %q, want %q", ftpd.password, "testpass")
		}
	})

	t.Run("probe with credentials against mock server", func(t *testing.T) {
		addr, cleanup := startMockFTPServer(t)
		defer cleanup()

		pd, err := newFTPProtocolDownloader(
			fmt.Sprintf("ftp://testuser:testpass@%s/pub/testfile.bin", addr), nil)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}

		probe, err := pd.Probe(context.Background())
		if err != nil {
			t.Fatalf("Probe error: %v", err)
		}
		if probe.ContentLength != 1024 {
			t.Errorf("ContentLength = %d, want 1024", probe.ContentLength)
		}
	})
}

func TestStripURLCredentials(t *testing.T) {
	t.Run("strips user:pass from URL", func(t *testing.T) {
		got := StripURLCredentials("ftp://user:pass@host:21/path/file.iso")
		if strings.Contains(got, "user") || strings.Contains(got, "pass") || strings.Contains(got, "@") {
			t.Errorf("StripURLCredentials did not strip credentials: %q", got)
		}
		if !strings.Contains(got, "ftp://host:21/path/file.iso") {
			t.Errorf("unexpected result: %q", got)
		}
	})

	t.Run("no-op when no credentials", func(t *testing.T) {
		got := StripURLCredentials("ftp://host/path")
		if got != "ftp://host/path" {
			t.Errorf("StripURLCredentials(%q) = %q, want %q", "ftp://host/path", got, "ftp://host/path")
		}
	})
}

func TestFTPCapabilities(t *testing.T) {
	pd, err := newFTPProtocolDownloader("ftp://host/file.bin", nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	caps := pd.Capabilities()
	if caps.SupportsParallel != false {
		t.Error("expected SupportsParallel=false")
	}
	if caps.SupportsResume != true {
		t.Error("expected SupportsResume=true")
	}
	if pd.GetMaxConnections() != 1 {
		t.Errorf("GetMaxConnections() = %d, want 1", pd.GetMaxConnections())
	}
	if pd.GetMaxParts() != 1 {
		t.Errorf("GetMaxParts() = %d, want 1", pd.GetMaxParts())
	}
}

// Batch 2: Probe + Download + Progress (mock server required)

func TestFTPProbe(t *testing.T) {
	addr, cleanup := startMockFTPServer(t)
	defer cleanup()

	pd, err := newFTPProtocolDownloader(fmt.Sprintf("ftp://%s/pub/testfile.bin", addr), nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	probe, err := pd.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe error: %v", err)
	}

	if probe.ContentLength != 1024 {
		t.Errorf("ContentLength = %d, want 1024", probe.ContentLength)
	}
	if probe.FileName != "testfile.bin" {
		t.Errorf("FileName = %q, want %q", probe.FileName, "testfile.bin")
	}
	if !probe.Resumable {
		t.Error("expected Resumable=true")
	}
	if probe.Checksums != nil {
		t.Error("expected Checksums=nil for FTP")
	}
}

func TestFTPProbeRequired(t *testing.T) {
	pd, err := newFTPProtocolDownloader("ftp://host/file.bin", nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	t.Run("Download without Probe returns ErrProbeRequired", func(t *testing.T) {
		err := pd.Download(context.Background(), nil)
		if err != ErrProbeRequired {
			t.Errorf("Download() = %v, want ErrProbeRequired", err)
		}
	})

	t.Run("Resume without Probe returns ErrProbeRequired", func(t *testing.T) {
		err := pd.Resume(context.Background(), nil, nil)
		if err != ErrProbeRequired {
			t.Errorf("Resume() = %v, want ErrProbeRequired", err)
		}
	})
}

func TestFTPDownloadIntegration(t *testing.T) {
	addr, cleanup := startMockFTPServer(t)
	defer cleanup()

	dlDir := t.TempDir()
	pd, err := newFTPProtocolDownloader(
		fmt.Sprintf("ftp://%s/pub/testfile.bin", addr),
		&DownloaderOpts{DownloadDirectory: dlDir},
	)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	probe, err := pd.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe error: %v", err)
	}

	// Track handler calls
	var spawnCalls int32
	var completeCalls int32
	var totalProgress int64

	handlers := &Handlers{
		SpawnPartHandler: func(hash string, ioff, foff int64) {
			atomic.AddInt32(&spawnCalls, 1)
			if ioff != 0 {
				t.Errorf("SpawnPartHandler ioff = %d, want 0", ioff)
			}
			if foff != probe.ContentLength-1 {
				t.Errorf("SpawnPartHandler foff = %d, want %d", foff, probe.ContentLength-1)
			}
		},
		DownloadProgressHandler: func(hash string, nread int) {
			atomic.AddInt64(&totalProgress, int64(nread))
		},
		DownloadCompleteHandler: func(hash string, tread int64) {
			atomic.AddInt32(&completeCalls, 1)
			if hash != MAIN_HASH {
				t.Errorf("DownloadCompleteHandler hash = %q, want %q", hash, MAIN_HASH)
			}
		},
	}

	err = pd.Download(context.Background(), handlers)
	if err != nil {
		t.Fatalf("Download error: %v", err)
	}

	// Verify file content
	content, err := os.ReadFile(filepath.Join(dlDir, "testfile.bin"))
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}

	expected := bytes.Repeat([]byte{0xAB}, 1024)
	if !bytes.Equal(content, expected) {
		t.Errorf("downloaded content mismatch: got %d bytes, want %d bytes", len(content), len(expected))
	}

	// Verify handler calls
	if atomic.LoadInt32(&spawnCalls) != 1 {
		t.Errorf("SpawnPartHandler called %d times, want 1", spawnCalls)
	}
	if atomic.LoadInt32(&completeCalls) != 1 {
		t.Errorf("DownloadCompleteHandler called %d times, want 1", completeCalls)
	}
	if atomic.LoadInt64(&totalProgress) != 1024 {
		t.Errorf("total progress = %d, want 1024", totalProgress)
	}
}

func TestFTPProgressTracking(t *testing.T) {
	addr, cleanup := startMockFTPServer(t)
	defer cleanup()

	dlDir := t.TempDir()
	pd, err := newFTPProtocolDownloader(
		fmt.Sprintf("ftp://%s/pub/largefile.bin", addr),
		&DownloaderOpts{DownloadDirectory: dlDir},
	)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = pd.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe error: %v", err)
	}

	var progressCalls int32
	handlers := &Handlers{
		DownloadProgressHandler: func(hash string, nread int) {
			atomic.AddInt32(&progressCalls, 1)
		},
	}

	err = pd.Download(context.Background(), handlers)
	if err != nil {
		t.Fatalf("Download error: %v", err)
	}

	if atomic.LoadInt32(&progressCalls) == 0 {
		t.Error("DownloadProgressHandler was never called")
	}
}

func TestFTPNilHandlerSafety(t *testing.T) {
	addr, cleanup := startMockFTPServer(t)
	defer cleanup()

	t.Run("nil handlers does not panic", func(t *testing.T) {
		dlDir := t.TempDir()
		pd, err := newFTPProtocolDownloader(
			fmt.Sprintf("ftp://%s/pub/testfile.bin", addr),
			&DownloaderOpts{DownloadDirectory: dlDir},
		)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		if _, err := pd.Probe(context.Background()); err != nil {
			t.Fatalf("Probe error: %v", err)
		}

		// Should not panic with nil handlers
		err = pd.Download(context.Background(), nil)
		if err != nil {
			t.Fatalf("Download with nil handlers error: %v", err)
		}

		// Verify file was written
		content, err := os.ReadFile(filepath.Join(dlDir, "testfile.bin"))
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		if len(content) != 1024 {
			t.Errorf("file size = %d, want 1024", len(content))
		}
	})

	t.Run("partial handlers does not panic", func(t *testing.T) {
		dlDir := t.TempDir()
		pd, err := newFTPProtocolDownloader(
			fmt.Sprintf("ftp://%s/pub/testfile.bin", addr),
			&DownloaderOpts{DownloadDirectory: dlDir},
		)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		if _, err := pd.Probe(context.Background()); err != nil {
			t.Fatalf("Probe error: %v", err)
		}

		// Only DownloadProgressHandler set, others nil
		handlers := &Handlers{
			DownloadProgressHandler: func(hash string, nread int) {},
		}
		err = pd.Download(context.Background(), handlers)
		if err != nil {
			t.Fatalf("Download with partial handlers error: %v", err)
		}
	})
}

// Batch 3: Error classification + SchemeRouter + Manager

func TestClassifyFTPError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		result := classifyFTPError("ftp", "test", nil)
		if result != nil {
			t.Errorf("classifyFTPError(nil) = %v, want nil", result)
		}
	})

	t.Run("4xx textproto error is transient", func(t *testing.T) {
		err := &textproto.Error{Code: 421, Msg: "Service not available"}
		result := classifyFTPError("ftp", "test", err)
		if result == nil {
			t.Fatal("expected non-nil error")
		}
		if !result.IsTransient() {
			t.Error("expected 4xx to be transient")
		}
	})

	t.Run("5xx textproto error is permanent", func(t *testing.T) {
		err := &textproto.Error{Code: 530, Msg: "Not logged in"}
		result := classifyFTPError("ftp", "test", err)
		if result == nil {
			t.Fatal("expected non-nil error")
		}
		if result.IsTransient() {
			t.Error("expected 5xx to be permanent")
		}
	})

	t.Run("net.Error is transient", func(t *testing.T) {
		err := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: fmt.Errorf("connection refused"),
		}
		result := classifyFTPError("ftp", "test", err)
		if result == nil {
			t.Fatal("expected non-nil error")
		}
		if !result.IsTransient() {
			t.Error("expected net.Error to be transient")
		}
	})
}

func TestFTPSchemeRouting(t *testing.T) {
	router := NewSchemeRouter(nil)

	t.Run("ftp scheme registered", func(t *testing.T) {
		schemes := SupportedSchemes(router)
		found := false
		for _, s := range schemes {
			if s == "ftp" {
				found = true
				break
			}
		}
		if !found {
			t.Error("ftp scheme not found in SupportedSchemes")
		}
	})

	t.Run("ftps scheme registered", func(t *testing.T) {
		schemes := SupportedSchemes(router)
		found := false
		for _, s := range schemes {
			if s == "ftps" {
				found = true
				break
			}
		}
		if !found {
			t.Error("ftps scheme not found in SupportedSchemes")
		}
	})

	t.Run("dispatches ftp URL to FTP factory", func(t *testing.T) {
		pd, err := router.NewDownloader("ftp://localhost:2121/pub/file.iso", nil)
		if err != nil {
			t.Fatalf("NewDownloader error: %v", err)
		}
		if _, ok := pd.(*ftpProtocolDownloader); !ok {
			t.Errorf("expected *ftpProtocolDownloader, got %T", pd)
		}
	})

	t.Run("dispatches uppercase FTP via case normalization", func(t *testing.T) {
		pd, err := router.NewDownloader("FTP://localhost:2121/pub/file.iso", nil)
		if err != nil {
			t.Fatalf("NewDownloader error: %v", err)
		}
		if _, ok := pd.(*ftpProtocolDownloader); !ok {
			t.Errorf("expected *ftpProtocolDownloader, got %T", pd)
		}
	})
}

func TestAddProtocolDownload(t *testing.T) {
	addr, cleanup := startMockFTPServer(t)
	defer cleanup()

	m := newTestManager(t)
	defer m.Close()

	dlDir := t.TempDir()
	pd, err := newFTPProtocolDownloader(
		fmt.Sprintf("ftp://testuser:testpass@%s/pub/testfile.bin", addr),
		&DownloaderOpts{DownloadDirectory: dlDir},
	)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	probe, err := pd.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe error: %v", err)
	}

	cleanURL := StripURLCredentials(fmt.Sprintf("ftp://testuser:testpass@%s/pub/testfile.bin", addr))
	handlers := &Handlers{}

	err = m.AddProtocolDownload(pd, probe, cleanURL, ProtoFTP, handlers, &AddDownloadOpts{
		AbsoluteLocation: dlDir,
	})
	if err != nil {
		t.Fatalf("AddProtocolDownload error: %v", err)
	}

	item := m.GetItem(pd.GetHash())
	if item == nil {
		t.Fatal("expected item to exist after AddProtocolDownload")
	}

	t.Run("item has correct Hash", func(t *testing.T) {
		if item.Hash != pd.GetHash() {
			t.Errorf("item.Hash = %q, want %q", item.Hash, pd.GetHash())
		}
	})

	t.Run("item has correct Name", func(t *testing.T) {
		if item.Name != "testfile.bin" {
			t.Errorf("item.Name = %q, want %q", item.Name, "testfile.bin")
		}
	})

	t.Run("item URL has no credentials", func(t *testing.T) {
		if strings.Contains(item.Url, "@") {
			t.Errorf("item.Url contains credentials: %q", item.Url)
		}
		if strings.Contains(item.Url, "testpass") {
			t.Errorf("item.Url contains password: %q", item.Url)
		}
	})

	t.Run("item has correct TotalSize", func(t *testing.T) {
		if item.TotalSize != ContentLength(1024) {
			t.Errorf("item.TotalSize = %d, want 1024", item.TotalSize)
		}
	})

	t.Run("item has correct Protocol", func(t *testing.T) {
		if item.Protocol != ProtoFTP {
			t.Errorf("item.Protocol = %d, want ProtoFTP (%d)", item.Protocol, ProtoFTP)
		}
	})

	t.Run("item is Resumable", func(t *testing.T) {
		if !item.Resumable {
			t.Error("expected item.Resumable=true")
		}
	})

	// Test patchProtocolHandlers integration
	t.Run("patchProtocolHandlers wraps SpawnPartHandler", func(t *testing.T) {
		// Create a fresh manager and add with tracking handlers
		m2 := newTestManager(t)
		defer m2.Close()

		pd2, err := newFTPProtocolDownloader(
			fmt.Sprintf("ftp://%s/pub/testfile.bin", addr),
			&DownloaderOpts{DownloadDirectory: dlDir},
		)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		probe2, err := pd2.Probe(context.Background())
		if err != nil {
			t.Fatalf("Probe error: %v", err)
		}

		var spawnCalled bool
		h := &Handlers{
			SpawnPartHandler: func(hash string, ioff, foff int64) {
				spawnCalled = true
			},
		}

		cleanURL2 := StripURLCredentials(fmt.Sprintf("ftp://%s/pub/testfile.bin", addr))
		err = m2.AddProtocolDownload(pd2, probe2, cleanURL2, ProtoFTP, h, &AddDownloadOpts{
			AbsoluteLocation: dlDir,
		})
		if err != nil {
			t.Fatalf("AddProtocolDownload error: %v", err)
		}

		// Trigger SpawnPartHandler through the wrapped handler
		h.SpawnPartHandler(pd2.GetHash(), 0, 1023)
		if !spawnCalled {
			t.Error("original SpawnPartHandler was not called")
		}

		// Verify item.Parts was updated
		item2 := m2.GetItem(pd2.GetHash())
		if item2 == nil {
			t.Fatal("item not found")
		}
		if len(item2.Parts) == 0 {
			t.Error("item.Parts was not updated by wrapped SpawnPartHandler")
		}
	})

	t.Run("patchProtocolHandlers wraps DownloadProgressHandler", func(t *testing.T) {
		m3 := newTestManager(t)
		defer m3.Close()

		pd3, err := newFTPProtocolDownloader(
			fmt.Sprintf("ftp://%s/pub/testfile.bin", addr),
			&DownloaderOpts{DownloadDirectory: dlDir},
		)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		probe3, err := pd3.Probe(context.Background())
		if err != nil {
			t.Fatalf("Probe error: %v", err)
		}

		h := &Handlers{}
		cleanURL3 := StripURLCredentials(fmt.Sprintf("ftp://%s/pub/testfile.bin", addr))
		err = m3.AddProtocolDownload(pd3, probe3, cleanURL3, ProtoFTP, h, &AddDownloadOpts{
			AbsoluteLocation: dlDir,
		})
		if err != nil {
			t.Fatalf("AddProtocolDownload error: %v", err)
		}

		// Trigger progress handler
		h.DownloadProgressHandler(pd3.GetHash(), 100)

		item3 := m3.GetItem(pd3.GetHash())
		if item3.Downloaded != 100 {
			t.Errorf("item.Downloaded = %d, want 100 after progress handler", item3.Downloaded)
		}
	})

	t.Run("patchProtocolHandlers wraps DownloadCompleteHandler with MAIN_HASH gate", func(t *testing.T) {
		m4 := newTestManager(t)
		defer m4.Close()

		pd4, err := newFTPProtocolDownloader(
			fmt.Sprintf("ftp://%s/pub/testfile.bin", addr),
			&DownloaderOpts{DownloadDirectory: dlDir},
		)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		probe4, err := pd4.Probe(context.Background())
		if err != nil {
			t.Fatalf("Probe error: %v", err)
		}

		var completeCalled bool
		h := &Handlers{
			DownloadCompleteHandler: func(hash string, tread int64) {
				completeCalled = true
			},
		}
		cleanURL4 := StripURLCredentials(fmt.Sprintf("ftp://%s/pub/testfile.bin", addr))
		err = m4.AddProtocolDownload(pd4, probe4, cleanURL4, ProtoFTP, h, &AddDownloadOpts{
			AbsoluteLocation: dlDir,
		})
		if err != nil {
			t.Fatalf("AddProtocolDownload error: %v", err)
		}

		// Non-MAIN_HASH should be ignored
		h.DownloadCompleteHandler("some-part-hash", 1024)
		if completeCalled {
			t.Error("DownloadCompleteHandler should not fire for non-MAIN_HASH")
		}

		// MAIN_HASH should fire
		h.DownloadCompleteHandler(MAIN_HASH, 1024)
		if !completeCalled {
			t.Error("DownloadCompleteHandler was not called for MAIN_HASH")
		}

		item4 := m4.GetItem(pd4.GetHash())
		if item4.Parts != nil {
			t.Error("item.Parts should be nil after DownloadCompleteHandler(MAIN_HASH)")
		}
		if item4.Downloaded != item4.TotalSize {
			t.Errorf("item.Downloaded = %d, want %d", item4.Downloaded, item4.TotalSize)
		}
	})
}

// FTP Resume tests

func TestFTPResume(t *testing.T) {
	addr, cleanup := startMockFTPServer(t)
	defer cleanup()

	t.Run("resume from offset completes file", func(t *testing.T) {
		dlDir := t.TempDir()
		pd, err := newFTPProtocolDownloader(
			fmt.Sprintf("ftp://%s/pub/testfile.bin", addr),
			&DownloaderOpts{DownloadDirectory: dlDir},
		)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		_, err = pd.Probe(context.Background())
		if err != nil {
			t.Fatalf("Probe error: %v", err)
		}

		// Create partial file (first 512 bytes)
		expected := bytes.Repeat([]byte{0xAB}, 1024)
		partialPath := filepath.Join(dlDir, "testfile.bin")
		if err := os.WriteFile(partialPath, expected[:512], DefaultFileMode); err != nil {
			t.Fatalf("failed to create partial file: %v", err)
		}

		parts := map[int64]*ItemPart{
			0: {Hash: "part0", FinalOffset: 1023, Compiled: false},
		}

		var progressTotal int64
		var completeCalled bool
		handlers := &Handlers{
			DownloadProgressHandler: func(hash string, nread int) {
				atomic.AddInt64(&progressTotal, int64(nread))
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				completeCalled = true
			},
		}

		err = pd.Resume(context.Background(), parts, handlers)
		if err != nil {
			t.Fatalf("Resume error: %v", err)
		}

		// Verify completed file
		content, err := os.ReadFile(partialPath)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}
		if !bytes.Equal(content, expected) {
			t.Errorf("file content mismatch after resume: got %d bytes", len(content))
		}

		if !completeCalled {
			t.Error("DownloadCompleteHandler was not called")
		}
		if progressTotal != 512 {
			t.Errorf("progress total = %d, want 512 (remaining bytes)", progressTotal)
		}
	})

	t.Run("resume with all parts compiled returns nil", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://host/file.bin", nil)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		// Manually set probed to true
		ftpd := pd.(*ftpProtocolDownloader)
		ftpd.probed = true
		ftpd.fileSize = 1024

		parts := map[int64]*ItemPart{
			0: {Hash: "part0", FinalOffset: 1023, Compiled: true},
		}

		err = pd.Resume(context.Background(), parts, nil)
		if err != nil {
			t.Errorf("Resume with all compiled parts = %v, want nil", err)
		}
	})

	t.Run("resume without probe returns ErrProbeRequired", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://host/file.bin", nil)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}

		err = pd.Resume(context.Background(), nil, nil)
		if err != ErrProbeRequired {
			t.Errorf("Resume without Probe = %v, want ErrProbeRequired", err)
		}
	})
}

// FTPS TLS test

func TestFTPSExplicitTLS(t *testing.T) {
	t.Run("ftps URL sets useTLS=true", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftps://host/file.bin", nil)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		ftpd := pd.(*ftpProtocolDownloader)
		if !ftpd.useTLS {
			t.Error("expected useTLS=true for ftps:// URL")
		}
	})

	t.Run("ftp URL sets useTLS=false", func(t *testing.T) {
		pd, err := newFTPProtocolDownloader("ftp://host/file.bin", nil)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		ftpd := pd.(*ftpProtocolDownloader)
		if ftpd.useTLS {
			t.Error("expected useTLS=false for ftp:// URL")
		}
	})

	t.Run("SchemeRouter dispatches ftps to FTP factory with TLS", func(t *testing.T) {
		router := NewSchemeRouter(nil)
		pd, err := router.NewDownloader("ftps://host/file.bin", nil)
		if err != nil {
			t.Fatalf("NewDownloader error: %v", err)
		}
		ftpd, ok := pd.(*ftpProtocolDownloader)
		if !ok {
			t.Fatalf("expected *ftpProtocolDownloader, got %T", pd)
		}
		if !ftpd.useTLS {
			t.Error("expected useTLS=true for ftps:// via SchemeRouter")
		}
	})
}

func TestFTPStopAndClose(t *testing.T) {
	pd, err := newFTPProtocolDownloader("ftp://host/file.bin", nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	t.Run("IsStopped initially false", func(t *testing.T) {
		if pd.IsStopped() {
			t.Error("expected IsStopped=false initially")
		}
	})

	t.Run("Stop sets stopped flag", func(t *testing.T) {
		pd.Stop()
		if !pd.IsStopped() {
			t.Error("expected IsStopped=true after Stop()")
		}
	})

	t.Run("Download returns nil when stopped", func(t *testing.T) {
		// Manually set probed to skip probe check
		ftpd := pd.(*ftpProtocolDownloader)
		ftpd.probed = true
		err := pd.Download(context.Background(), nil)
		if err != nil {
			t.Errorf("Download when stopped = %v, want nil", err)
		}
	})

	t.Run("Resume returns nil when stopped", func(t *testing.T) {
		err := pd.Resume(context.Background(), nil, nil)
		if err != nil {
			t.Errorf("Resume when stopped = %v, want nil", err)
		}
	})

	t.Run("Close does not panic", func(t *testing.T) {
		err := pd.Close()
		if err != nil {
			t.Errorf("Close() = %v, want nil", err)
		}
	})
}

func TestFTPGetters(t *testing.T) {
	dlDir := t.TempDir()
	pd, err := newFTPProtocolDownloader("ftp://host/pub/file.bin", &DownloaderOpts{
		DownloadDirectory: dlDir,
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	t.Run("GetSavePath", func(t *testing.T) {
		sp := pd.GetSavePath()
		if sp == "" {
			t.Error("GetSavePath returned empty")
		}
		if !strings.Contains(sp, "file.bin") {
			t.Errorf("GetSavePath = %q, expected to contain file.bin", sp)
		}
	})

	t.Run("GetContentLength before probe is 0", func(t *testing.T) {
		cl := pd.GetContentLength()
		if cl != 0 {
			t.Errorf("GetContentLength before Probe = %d, want 0", cl)
		}
	})

	t.Run("GetContentLength after probe", func(t *testing.T) {
		addr, cleanup := startMockFTPServer(t)
		defer cleanup()

		pd2, err := newFTPProtocolDownloader(
			fmt.Sprintf("ftp://%s/pub/testfile.bin", addr),
			&DownloaderOpts{DownloadDirectory: dlDir},
		)
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		if _, err := pd2.Probe(context.Background()); err != nil {
			t.Fatalf("Probe error: %v", err)
		}
		cl := pd2.GetContentLength()
		if cl != 1024 {
			t.Errorf("GetContentLength after Probe = %d, want 1024", cl)
		}
	})

	t.Run("GetHash is non-empty", func(t *testing.T) {
		if pd.GetHash() == "" {
			t.Error("GetHash returned empty")
		}
	})

	t.Run("GetDownloadDirectory", func(t *testing.T) {
		if pd.GetDownloadDirectory() != dlDir {
			t.Errorf("GetDownloadDirectory = %q, want %q", pd.GetDownloadDirectory(), dlDir)
		}
	})
}

