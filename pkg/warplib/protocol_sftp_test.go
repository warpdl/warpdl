package warplib

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ============================================================
// Mock SFTP Server infrastructure
// ============================================================

type mockSFTPOption func(*ssh.ServerConfig)

func withPasswordAuth(user, pass string) mockSFTPOption {
	return func(config *ssh.ServerConfig) {
		config.PasswordCallback = func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(password) == pass {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid credentials")
		}
	}
}

func withPublicKeyAuth(authorizedKey ssh.PublicKey) mockSFTPOption {
	return func(config *ssh.ServerConfig) {
		config.PublicKeyCallback = func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authorizedKey.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unknown public key")
		}
	}
}

// testFileEntry represents a file served by the mock SFTP server.
// path is relative to the filesystem root (e.g., "pub/testfile.bin").
type testFileEntry struct {
	path    string
	content []byte
}

// mockSFTPResult holds the result of starting a mock SFTP server.
type mockSFTPResult struct {
	addr    string
	hostKey ssh.PublicKey
	fsRoot  string
	cleanup func()
}

// remotePath returns the absolute path on the mock server's filesystem
// for a given relative file entry path.
func (m *mockSFTPResult) remotePath(relPath string) string {
	return filepath.Join(m.fsRoot, relPath)
}

// sftpURL constructs an sftp:// URL for the mock server with the given
// credentials and relative file path.
func (m *mockSFTPResult) sftpURL(user, pass, relPath string) string {
	absPath := m.remotePath(relPath)
	if pass != "" {
		return fmt.Sprintf("sftp://%s:%s@%s%s", user, pass, m.addr, absPath)
	}
	return fmt.Sprintf("sftp://%s@%s%s", user, m.addr, absPath)
}

// startMockSFTPServer starts an in-process SSH server with SFTP subsystem.
func startMockSFTPServer(t *testing.T, files []testFileEntry, opts ...mockSFTPOption) *mockSFTPResult {
	t.Helper()

	// Generate host key
	hostPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivKey)
	if err != nil {
		t.Fatalf("create host signer: %v", err)
	}

	config := &ssh.ServerConfig{}
	for _, opt := range opts {
		opt(config)
	}
	config.AddHostKey(hostSigner)

	// Listen on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	// Create temp filesystem for SFTP
	fsRoot := t.TempDir()
	for _, f := range files {
		absPath := filepath.Join(fsRoot, f.path)
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdirall %s: %v", dir, err)
		}
		if err := os.WriteFile(absPath, f.content, 0644); err != nil {
			t.Fatalf("write %s: %v", absPath, err)
		}
	}

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // listener closed
			}
			go handleSSHConnection(conn, config)
		}
	}()

	result := &mockSFTPResult{
		addr:    listener.Addr().String(),
		hostKey: hostSigner.PublicKey(),
		fsRoot:  fsRoot,
		cleanup: func() { listener.Close() },
	}

	t.Cleanup(result.cleanup)
	return result
}

func handleSSHConnection(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go func() {
			for req := range requests {
				if req.Type == "subsystem" && string(req.Payload[4:]) == "sftp" {
					req.Reply(true, nil)

					server, err := sftp.NewServer(channel)
					if err != nil {
						channel.Close()
						return
					}
					server.Serve()
					server.Close()
					return
				}
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}()
	}
}

// ============================================================
// Test helper: set up known_hosts for a mock server
// ============================================================

// setupTestKnownHosts creates a temp known_hosts file, seeds it with the
// mock server's host key, and sets KnownHostsPath to the temp file.
// Returns a cleanup function that restores the original KnownHostsPath.
func setupTestKnownHosts(t *testing.T, mock *mockSFTPResult) func() {
	t.Helper()
	tmpDir := t.TempDir()
	khFile := filepath.Join(tmpDir, "known_hosts")
	savedKH := KnownHostsPath
	KnownHostsPath = khFile

	callback := newTOFUHostKeyCallback(khFile)
	addr := fakeAddr{network: "tcp", address: mock.addr}
	if err := callback(mock.addr, addr, mock.hostKey); err != nil {
		t.Fatalf("seed known_hosts: %v", err)
	}

	return func() { KnownHostsPath = savedKH }
}

// writeTestSSHKey generates a test SSH key pair and writes the private key to a file.
// Returns the public key and the path to the private key file.
func writeTestSSHKey(t *testing.T, dir string) (ssh.PublicKey, string) {
	t.Helper()
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	keyPath := filepath.Join(dir, "id_test_ecdsa")
	pemData := pem.EncodeToMemory(pemBlock)
	if err := os.WriteFile(keyPath, pemData, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return signer.PublicKey(), keyPath
}

// ============================================================
// Tests
// ============================================================

func TestSFTPFactory(t *testing.T) {
	t.Run("valid URL with credentials and port", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://testuser:testpass@localhost:2222/pub/file.iso", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if d.host != "localhost:2222" {
			t.Errorf("host = %q, want %q", d.host, "localhost:2222")
		}
		if d.remotePath != "/pub/file.iso" {
			t.Errorf("remotePath = %q, want %q", d.remotePath, "/pub/file.iso")
		}
		if d.fileName != "file.iso" {
			t.Errorf("fileName = %q, want %q", d.fileName, "file.iso")
		}
		if d.user != "testuser" {
			t.Errorf("user = %q, want %q", d.user, "testuser")
		}
		if d.password != "testpass" {
			t.Errorf("password = %q, want %q", d.password, "testpass")
		}
	})

	t.Run("empty path returns error", func(t *testing.T) {
		_, err := newSFTPProtocolDownloader("sftp://host/", nil)
		if err == nil {
			t.Fatal("expected error for root path")
		}
		var de *DownloadError
		if !errors.As(err, &de) {
			t.Fatalf("expected DownloadError, got %T", err)
		}
	})

	t.Run("no path returns error", func(t *testing.T) {
		_, err := newSFTPProtocolDownloader("sftp://host", nil)
		if err == nil {
			t.Fatal("expected error for no path")
		}
	})

	t.Run("user without password falls back to key auth", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://myuser@host/file.iso", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if d.user != "myuser" {
			t.Errorf("user = %q, want %q", d.user, "myuser")
		}
		if d.password != "" {
			t.Errorf("password should be empty, got %q", d.password)
		}
	})

	t.Run("explicit FileName from opts overrides URL", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://user:pass@host/pub/original.tar.gz", &DownloaderOpts{
			FileName: "custom.tar.gz",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if d.fileName != "custom.tar.gz" {
			t.Errorf("fileName = %q, want %q", d.fileName, "custom.tar.gz")
		}
	})
}

func TestSFTPPasswordAuth(t *testing.T) {
	t.Run("extracts credentials from URL", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://testuser:testpass@host/file", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if d.user != "testuser" {
			t.Errorf("user = %q, want %q", d.user, "testuser")
		}
		if d.password != "testpass" {
			t.Errorf("password = %q, want %q", d.password, "testpass")
		}
	})

	t.Run("password auth probe against mock server", func(t *testing.T) {
		testContent := bytes.Repeat([]byte{0xAB}, 1024)
		mock := startMockSFTPServer(t,
			[]testFileEntry{{path: "pub/testfile.bin", content: testContent}},
			withPasswordAuth("sftpuser", "sftppass"),
		)
		restoreKH := setupTestKnownHosts(t, mock)
		defer restoreKH()

		dlDir := t.TempDir()
		rawURL := mock.sftpURL("sftpuser", "sftppass", "pub/testfile.bin")
		pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
			DownloadDirectory: dlDir,
		})
		if err != nil {
			t.Fatalf("factory: %v", err)
		}

		d := pd.(*sftpProtocolDownloader)
		result, err := d.Probe(context.Background())
		if err != nil {
			t.Fatalf("probe: %v", err)
		}

		if result.ContentLength != 1024 {
			t.Errorf("ContentLength = %d, want 1024", result.ContentLength)
		}
	})

	t.Run("full download with password auth", func(t *testing.T) {
		testContent := bytes.Repeat([]byte{0xCD}, 512)
		mock := startMockSFTPServer(t,
			[]testFileEntry{{path: "pub/download.bin", content: testContent}},
			withPasswordAuth("dluser", "dlpass"),
		)
		restoreKH := setupTestKnownHosts(t, mock)
		defer restoreKH()

		dlDir := t.TempDir()
		rawURL := mock.sftpURL("dluser", "dlpass", "pub/download.bin")
		pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
			DownloadDirectory: dlDir,
		})
		if err != nil {
			t.Fatalf("factory: %v", err)
		}

		d := pd.(*sftpProtocolDownloader)
		if _, err := d.Probe(context.Background()); err != nil {
			t.Fatalf("probe: %v", err)
		}

		if err := d.Download(context.Background(), nil); err != nil {
			t.Fatalf("download: %v", err)
		}

		gotBytes, err := os.ReadFile(d.savePath)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if !bytes.Equal(gotBytes, testContent) {
			t.Error("downloaded content does not match expected")
		}
	})
}

func TestSFTPKeyAuth(t *testing.T) {
	t.Run("buildAuthMethods returns error with no auth", func(t *testing.T) {
		methods, err := buildAuthMethods("", "/nonexistent/key")
		if err == nil {
			t.Fatal("expected error when no auth available")
		}
		if methods != nil {
			t.Error("methods should be nil on error")
		}
		if !strings.Contains(err.Error(), "no authentication method") {
			t.Errorf("error should mention 'no authentication method', got: %v", err)
		}
	})

	t.Run("key auth with generated key against mock server", func(t *testing.T) {
		tmpDir := t.TempDir()
		clientPubKey, clientKeyPath := writeTestSSHKey(t, tmpDir)

		testContent := bytes.Repeat([]byte{0xEF}, 256)
		mock := startMockSFTPServer(t,
			[]testFileEntry{{path: "data/keyfile.bin", content: testContent}},
			withPublicKeyAuth(clientPubKey),
		)
		restoreKH := setupTestKnownHosts(t, mock)
		defer restoreKH()

		dlDir := t.TempDir()
		rawURL := mock.sftpURL("keyuser", "", "data/keyfile.bin")
		pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
			DownloadDirectory: dlDir,
			SSHKeyPath:        clientKeyPath,
		})
		if err != nil {
			t.Fatalf("factory: %v", err)
		}

		d := pd.(*sftpProtocolDownloader)
		if _, err := d.Probe(context.Background()); err != nil {
			t.Fatalf("probe: %v", err)
		}

		if err := d.Download(context.Background(), nil); err != nil {
			t.Fatalf("download: %v", err)
		}

		gotBytes, err := os.ReadFile(d.savePath)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if !bytes.Equal(gotBytes, testContent) {
			t.Error("downloaded content does not match expected")
		}
	})
}

func TestSFTPCustomKeyPath(t *testing.T) {
	t.Run("opts.SSHKeyPath is used for auth", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://user@host/file.iso", &DownloaderOpts{
			SSHKeyPath: "/custom/path/id_rsa",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if d.sshKeyPath != "/custom/path/id_rsa" {
			t.Errorf("sshKeyPath = %q, want %q", d.sshKeyPath, "/custom/path/id_rsa")
		}
	})

	t.Run("resolveSSHKeyPaths with explicit key returns that path only", func(t *testing.T) {
		paths := resolveSSHKeyPaths("/explicit/key")
		if len(paths) != 1 || paths[0] != "/explicit/key" {
			t.Errorf("resolveSSHKeyPaths with explicit = %v, want [/explicit/key]", paths)
		}
	})
}

func TestSFTPCapabilities(t *testing.T) {
	pd, err := newSFTPProtocolDownloader("sftp://user:pass@host/file.iso", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := pd.Capabilities()
	if caps.SupportsParallel {
		t.Error("SupportsParallel should be false")
	}
	if !caps.SupportsResume {
		t.Error("SupportsResume should be true")
	}
	if pd.GetMaxConnections() != 1 {
		t.Errorf("GetMaxConnections = %d, want 1", pd.GetMaxConnections())
	}
	if pd.GetMaxParts() != 1 {
		t.Errorf("GetMaxParts = %d, want 1", pd.GetMaxParts())
	}
}

func TestSFTPProbe(t *testing.T) {
	testContent := bytes.Repeat([]byte{0xBB}, 2048)
	mock := startMockSFTPServer(t,
		[]testFileEntry{{path: "docs/report.pdf", content: testContent}},
		withPasswordAuth("probeuser", "probepass"),
	)
	restoreKH := setupTestKnownHosts(t, mock)
	defer restoreKH()

	rawURL := mock.sftpURL("probeuser", "probepass", "docs/report.pdf")
	pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
		DownloadDirectory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	result, err := pd.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}

	if result.ContentLength != 2048 {
		t.Errorf("ContentLength = %d, want 2048", result.ContentLength)
	}
	if result.FileName != "report.pdf" {
		t.Errorf("FileName = %q, want %q", result.FileName, "report.pdf")
	}
	if !result.Resumable {
		t.Error("Resumable should be true")
	}
	if result.Checksums != nil {
		t.Error("Checksums should be nil")
	}
}

func TestSFTPDownloadIntegration(t *testing.T) {
	testContent := bytes.Repeat([]byte{0xAA}, 4096)
	mock := startMockSFTPServer(t,
		[]testFileEntry{{path: "files/big.bin", content: testContent}},
		withPasswordAuth("intuser", "intpass"),
	)
	restoreKH := setupTestKnownHosts(t, mock)
	defer restoreKH()

	dlDir := t.TempDir()
	rawURL := mock.sftpURL("intuser", "intpass", "files/big.bin")
	pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
		DownloadDirectory: dlDir,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	d := pd.(*sftpProtocolDownloader)

	// Probe first
	if _, err := d.Probe(context.Background()); err != nil {
		t.Fatalf("probe: %v", err)
	}

	// Track handlers
	var spawnCalls int32
	var progressBytes int32
	var completeCalls int32

	handlers := &Handlers{
		SpawnPartHandler: func(hash string, ioff, foff int64) {
			atomic.AddInt32(&spawnCalls, 1)
			if ioff != 0 {
				t.Errorf("SpawnPartHandler ioff = %d, want 0", ioff)
			}
			if foff != 4095 {
				t.Errorf("SpawnPartHandler foff = %d, want 4095", foff)
			}
		},
		DownloadProgressHandler: func(hash string, nread int) {
			atomic.AddInt32(&progressBytes, int32(nread))
		},
		DownloadCompleteHandler: func(hash string, tread int64) {
			atomic.AddInt32(&completeCalls, 1)
			if hash != MAIN_HASH {
				t.Errorf("DownloadCompleteHandler hash = %q, want %q", hash, MAIN_HASH)
			}
		},
	}

	if err := d.Download(context.Background(), handlers); err != nil {
		t.Fatalf("download: %v", err)
	}

	gotBytes, err := os.ReadFile(d.savePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !bytes.Equal(gotBytes, testContent) {
		t.Error("downloaded content mismatch")
	}

	if atomic.LoadInt32(&spawnCalls) != 1 {
		t.Errorf("SpawnPartHandler called %d times, want 1", spawnCalls)
	}
	if atomic.LoadInt32(&completeCalls) != 1 {
		t.Errorf("DownloadCompleteHandler called %d times, want 1", completeCalls)
	}
	if atomic.LoadInt32(&progressBytes) == 0 {
		t.Error("DownloadProgressHandler was never called")
	}
}

func TestSFTPPortParsing(t *testing.T) {
	t.Run("custom port in URL", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://user@host:2222/file.iso", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if d.host != "host:2222" {
			t.Errorf("host = %q, want %q", d.host, "host:2222")
		}
	})

	t.Run("default port 22 when unspecified", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://user:pass@host/file.iso", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if d.host != "host:22" {
			t.Errorf("host = %q, want %q", d.host, "host:22")
		}
	})
}

func TestClassifySFTPError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		result := classifySFTPError("sftp", "op", nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("os.ErrNotExist is permanent", func(t *testing.T) {
		result := classifySFTPError("sftp", "op", os.ErrNotExist)
		if result == nil {
			t.Fatal("expected non-nil error")
		}
		if result.IsTransient() {
			t.Error("os.ErrNotExist should be permanent, not transient")
		}
	})

	t.Run("net.Error is transient", func(t *testing.T) {
		netErr := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connection refused")}
		result := classifySFTPError("sftp", "op", netErr)
		if result == nil {
			t.Fatal("expected non-nil error")
		}
		if !result.IsTransient() {
			t.Error("net.Error should be transient")
		}
	})

	t.Run("ssh.ExitError is permanent", func(t *testing.T) {
		exitErr := &ssh.ExitError{Waitmsg: ssh.Waitmsg{}}
		result := classifySFTPError("sftp", "op", exitErr)
		if result == nil {
			t.Fatal("expected non-nil error")
		}
		if result.IsTransient() {
			t.Error("ssh.ExitError should be permanent, not transient")
		}
	})

	t.Run("unknown error defaults to permanent", func(t *testing.T) {
		result := classifySFTPError("sftp", "op", fmt.Errorf("something weird"))
		if result == nil {
			t.Fatal("expected non-nil error")
		}
		if result.IsTransient() {
			t.Error("unknown error should default to permanent")
		}
	})
}

func TestSFTPNilHandlerSafety(t *testing.T) {
	testContent := bytes.Repeat([]byte{0xCC}, 256)
	mock := startMockSFTPServer(t,
		[]testFileEntry{{path: "safe/test.bin", content: testContent}},
		withPasswordAuth("safeuser", "safepass"),
	)
	restoreKH := setupTestKnownHosts(t, mock)
	defer restoreKH()

	t.Run("nil Handlers does not panic", func(t *testing.T) {
		dlDir := t.TempDir()
		rawURL := mock.sftpURL("safeuser", "safepass", "safe/test.bin")
		pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
			DownloadDirectory: dlDir,
		})
		if err != nil {
			t.Fatalf("factory: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if _, err := d.Probe(context.Background()); err != nil {
			t.Fatalf("probe: %v", err)
		}

		if err := d.Download(context.Background(), nil); err != nil {
			t.Fatalf("download with nil handlers: %v", err)
		}

		gotBytes, err := os.ReadFile(d.savePath)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if !bytes.Equal(gotBytes, testContent) {
			t.Error("content mismatch")
		}
	})

	t.Run("partial Handlers does not panic", func(t *testing.T) {
		dlDir := t.TempDir()
		rawURL := mock.sftpURL("safeuser", "safepass", "safe/test.bin")
		pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
			DownloadDirectory: dlDir,
		})
		if err != nil {
			t.Fatalf("factory: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if _, err := d.Probe(context.Background()); err != nil {
			t.Fatalf("probe: %v", err)
		}

		handlers := &Handlers{
			DownloadProgressHandler: func(hash string, nread int) {},
		}
		if err := d.Download(context.Background(), handlers); err != nil {
			t.Fatalf("download with partial handlers: %v", err)
		}
	})
}

func TestSFTPProbeRequired(t *testing.T) {
	t.Run("Download without Probe returns error", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://user:pass@host/file.iso", nil)
		if err != nil {
			t.Fatalf("factory: %v", err)
		}
		err = pd.Download(context.Background(), nil)
		if !errors.Is(err, ErrProbeRequired) {
			t.Errorf("expected ErrProbeRequired, got: %v", err)
		}
	})

	t.Run("Resume without Probe returns error", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://user:pass@host/file.iso", nil)
		if err != nil {
			t.Fatalf("factory: %v", err)
		}
		err = pd.Resume(context.Background(), nil, nil)
		if !errors.Is(err, ErrProbeRequired) {
			t.Errorf("expected ErrProbeRequired, got: %v", err)
		}
	})
}

func TestSFTPAuthEdgeCases(t *testing.T) {
	t.Run("passphrase-protected key returns clear error", func(t *testing.T) {
		privKey, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		encryptedPEM, err := ssh.MarshalPrivateKeyWithPassphrase(privKey, "", []byte("mypassphrase"))
		if err != nil {
			t.Fatalf("marshal encrypted key: %v", err)
		}

		keyFile := filepath.Join(t.TempDir(), "encrypted_key")
		pemData := pem.EncodeToMemory(encryptedPEM)
		if err := os.WriteFile(keyFile, pemData, 0600); err != nil {
			t.Fatalf("write key: %v", err)
		}

		_, err = buildAuthMethods("", keyFile)
		if err == nil {
			t.Fatal("expected error for passphrase-protected key")
		}
		if !strings.Contains(err.Error(), "passphrase-protected") {
			t.Errorf("error should mention passphrase-protected, got: %v", err)
		}
	})

	t.Run("no password and no key returns clear error", func(t *testing.T) {
		_, err := buildAuthMethods("", "/nonexistent/key")
		if err == nil {
			t.Fatal("expected error when no auth available")
		}
		if !strings.Contains(err.Error(), "no authentication method") {
			t.Errorf("error should mention 'no authentication method', got: %v", err)
		}
	})
}

func TestSFTPCredentialStripping(t *testing.T) {
	t.Run("credentials stripped from sftp URL", func(t *testing.T) {
		result := StripURLCredentials("sftp://user:pass@host:22/path/file.iso")
		if strings.Contains(result, "user") || strings.Contains(result, "pass") {
			t.Errorf("credentials not stripped: %q", result)
		}
		if !strings.Contains(result, "sftp://") {
			t.Errorf("scheme should be preserved: %q", result)
		}
		if !strings.Contains(result, "host") {
			t.Errorf("host should be preserved: %q", result)
		}
	})

	t.Run("no-op for URL without credentials", func(t *testing.T) {
		result := StripURLCredentials("sftp://host/path")
		if result != "sftp://host/path" {
			t.Errorf("unexpected change: %q", result)
		}
	})

	t.Run("factory stores cleanURL without credentials", func(t *testing.T) {
		pd, err := newSFTPProtocolDownloader("sftp://user:secret@host:22/path/file.iso", nil)
		if err != nil {
			t.Fatalf("factory: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		if strings.Contains(d.cleanURL, "user") || strings.Contains(d.cleanURL, "secret") {
			t.Errorf("cleanURL should not contain credentials: %q", d.cleanURL)
		}
	})
}

func TestSFTPSchemeRouting(t *testing.T) {
	router := NewSchemeRouter(nil)

	t.Run("sftp in SupportedSchemes", func(t *testing.T) {
		schemes := SupportedSchemes(router)
		found := false
		for _, s := range schemes {
			if s == "sftp" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("sftp not in SupportedSchemes: %v", schemes)
		}
	})

	t.Run("SchemeRouter dispatches sftp URL to SFTP factory", func(t *testing.T) {
		pd, err := router.NewDownloader("sftp://user:pass@host/file.iso", nil)
		if err != nil {
			t.Fatalf("NewDownloader: %v", err)
		}
		if _, ok := pd.(*sftpProtocolDownloader); !ok {
			t.Errorf("expected *sftpProtocolDownloader, got %T", pd)
		}
	})

	t.Run("case-insensitive scheme dispatch", func(t *testing.T) {
		pd, err := router.NewDownloader("SFTP://user:pass@host/file.iso", nil)
		if err != nil {
			t.Fatalf("NewDownloader uppercase: %v", err)
		}
		if _, ok := pd.(*sftpProtocolDownloader); !ok {
			t.Errorf("expected *sftpProtocolDownloader, got %T", pd)
		}
	})
}

func TestDownloaderOptsSSHKeyPath(t *testing.T) {
	opts := &DownloaderOpts{
		SSHKeyPath: "/custom/ssh/key",
	}
	pd, err := newSFTPProtocolDownloader("sftp://user@host/file.iso", opts)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	d := pd.(*sftpProtocolDownloader)
	if d.sshKeyPath != "/custom/ssh/key" {
		t.Errorf("sshKeyPath = %q, want %q", d.sshKeyPath, "/custom/ssh/key")
	}
}

func TestSFTPStopLifecycle(t *testing.T) {
	pd, err := newSFTPProtocolDownloader("sftp://user:pass@host/file.iso", nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	if pd.IsStopped() {
		t.Error("should not be stopped initially")
	}

	pd.Stop()
	if !pd.IsStopped() {
		t.Error("should be stopped after Stop()")
	}

	if err := pd.Close(); err != nil {
		t.Errorf("Close should not error: %v", err)
	}
}

func TestSFTPGetters(t *testing.T) {
	pd, err := newSFTPProtocolDownloader("sftp://user:pass@host/docs/readme.txt", &DownloaderOpts{
		DownloadDirectory: "/tmp/downloads",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	if pd.GetFileName() != "readme.txt" {
		t.Errorf("GetFileName = %q, want %q", pd.GetFileName(), "readme.txt")
	}
	if pd.GetDownloadDirectory() != "/tmp/downloads" {
		t.Errorf("GetDownloadDirectory = %q, want %q", pd.GetDownloadDirectory(), "/tmp/downloads")
	}
	if pd.GetHash() == "" {
		t.Error("GetHash should not be empty")
	}
	if pd.GetContentLength() != 0 {
		t.Errorf("GetContentLength before Probe = %d, want 0", pd.GetContentLength())
	}
}

func TestSFTPResumeFlow(t *testing.T) {
	testContent := bytes.Repeat([]byte{0xDD}, 1024)
	mock := startMockSFTPServer(t,
		[]testFileEntry{{path: "data/resume.bin", content: testContent}},
		withPasswordAuth("resuser", "respass"),
	)
	restoreKH := setupTestKnownHosts(t, mock)
	defer restoreKH()

	t.Run("resume from partial file", func(t *testing.T) {
		dlDir := t.TempDir()
		rawURL := mock.sftpURL("resuser", "respass", "data/resume.bin")
		pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
			DownloadDirectory: dlDir,
		})
		if err != nil {
			t.Fatalf("factory: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)

		if _, err := d.Probe(context.Background()); err != nil {
			t.Fatalf("probe: %v", err)
		}

		// Write partial file (first 512 bytes)
		partialContent := testContent[:512]
		if err := os.WriteFile(d.savePath, partialContent, 0644); err != nil {
			t.Fatalf("write partial: %v", err)
		}

		var completeCalls int32
		handlers := &Handlers{
			DownloadCompleteHandler: func(hash string, tread int64) {
				atomic.AddInt32(&completeCalls, 1)
			},
		}

		parts := map[int64]*ItemPart{
			0: {Compiled: false},
		}
		if err := d.Resume(context.Background(), parts, handlers); err != nil {
			t.Fatalf("resume: %v", err)
		}

		gotBytes, err := os.ReadFile(d.savePath)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if !bytes.Equal(gotBytes, testContent) {
			t.Errorf("final content mismatch: got %d bytes, want %d bytes", len(gotBytes), len(testContent))
		}

		if atomic.LoadInt32(&completeCalls) != 1 {
			t.Errorf("completeCalls = %d, want 1", completeCalls)
		}
	})

	t.Run("resume with all parts compiled is noop", func(t *testing.T) {
		rawURL := mock.sftpURL("resuser", "respass", "data/resume.bin")
		pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
			DownloadDirectory: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("factory: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)
		d.probed = true
		d.fileSize = 1024

		parts := map[int64]*ItemPart{
			0: {Compiled: true},
		}
		if err := d.Resume(context.Background(), parts, nil); err != nil {
			t.Fatalf("resume all-compiled should return nil, got: %v", err)
		}
	})

	t.Run("resume without file starts from beginning", func(t *testing.T) {
		dlDir := t.TempDir()
		rawURL := mock.sftpURL("resuser", "respass", "data/resume.bin")
		pd, err := newSFTPProtocolDownloader(rawURL, &DownloaderOpts{
			DownloadDirectory: dlDir,
		})
		if err != nil {
			t.Fatalf("factory: %v", err)
		}
		d := pd.(*sftpProtocolDownloader)

		if _, err := d.Probe(context.Background()); err != nil {
			t.Fatalf("probe: %v", err)
		}

		parts := map[int64]*ItemPart{
			0: {Compiled: false},
		}
		if err := d.Resume(context.Background(), parts, nil); err != nil {
			t.Fatalf("resume from zero: %v", err)
		}

		gotBytes, err := os.ReadFile(d.savePath)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if !bytes.Equal(gotBytes, testContent) {
			t.Error("content mismatch when resuming from zero")
		}
	})
}

func TestSFTPResolveSSHKeyPaths(t *testing.T) {
	t.Run("explicit path returns only that path", func(t *testing.T) {
		paths := resolveSSHKeyPaths("/my/key")
		if len(paths) != 1 || paths[0] != "/my/key" {
			t.Errorf("expected [/my/key], got %v", paths)
		}
	})

	t.Run("empty path returns default locations", func(t *testing.T) {
		paths := resolveSSHKeyPaths("")
		if len(paths) != 2 {
			t.Errorf("expected 2 default paths, got %d: %v", len(paths), paths)
		}
		if len(paths) == 2 {
			if !strings.HasSuffix(paths[0], ".ssh/id_ed25519") {
				t.Errorf("first default should end with .ssh/id_ed25519, got %q", paths[0])
			}
			if !strings.HasSuffix(paths[1], ".ssh/id_rsa") {
				t.Errorf("second default should end with .ssh/id_rsa, got %q", paths[1])
			}
		}
	})
}

func TestSFTPProgressWriter(t *testing.T) {
	var totalBytes int32
	pw := &sftpProgressWriter{
		handlers: &Handlers{
			DownloadProgressHandler: func(hash string, nread int) {
				atomic.AddInt32(&totalBytes, int32(nread))
			},
		},
		hash: "test-hash",
	}

	data := make([]byte, 100)
	n, err := pw.Write(data)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if n != 100 {
		t.Errorf("n = %d, want 100", n)
	}
	if atomic.LoadInt32(&totalBytes) != 100 {
		t.Errorf("totalBytes = %d, want 100", totalBytes)
	}

	// Test with nil handlers
	pw2 := &sftpProgressWriter{handlers: nil, hash: "test"}
	n2, err2 := pw2.Write(data)
	if err2 != nil || n2 != 100 {
		t.Errorf("nil handlers Write: n=%d, err=%v", n2, err2)
	}
}

func TestSFTPBuildAuthMethodsPassword(t *testing.T) {
	methods, err := buildAuthMethods("mypassword", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 {
		t.Errorf("expected 1 auth method, got %d", len(methods))
	}
}

func TestSFTPBuildAuthMethodsKeyFile(t *testing.T) {
	dir := t.TempDir()
	_, keyPath := writeTestSSHKey(t, dir)

	methods, err := buildAuthMethods("", keyPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 {
		t.Errorf("expected 1 auth method from key file, got %d", len(methods))
	}
}

func TestSFTPWriterWithMultiWriter(t *testing.T) {
	var progressTotal int32
	var buf bytes.Buffer

	pw := &sftpProgressWriter{
		handlers: &Handlers{
			DownloadProgressHandler: func(hash string, nread int) {
				atomic.AddInt32(&progressTotal, int32(nread))
			},
		},
		hash: "mw-test",
	}

	mw := io.MultiWriter(&buf, pw)
	data := bytes.Repeat([]byte{0xFF}, 200)
	n, err := mw.Write(data)
	if err != nil {
		t.Fatalf("multiwriter write: %v", err)
	}
	if n != 200 {
		t.Errorf("n = %d, want 200", n)
	}
	if buf.Len() != 200 {
		t.Errorf("buf.Len = %d, want 200", buf.Len())
	}
	if atomic.LoadInt32(&progressTotal) != 200 {
		t.Errorf("progressTotal = %d, want 200", progressTotal)
	}
}

// ============================================================
// Manager.ResumeDownload SFTP tests (Plan 04-02)
// ============================================================

func TestResumeDownloadSFTP(t *testing.T) {
	t.Run("SFTP item dispatches through SchemeRouter", func(t *testing.T) {
		// Test that ProtoSFTP dispatches through SchemeRouter, not initDownloader.
		// Uses a non-existent host — the key assertion is that the error is NOT
		// "resume not supported for protocol" (which would mean ProtoSFTP was rejected).
		m := newTestManager(t)
		defer m.Close()
		router := NewSchemeRouter(nil)
		m.SetSchemeRouter(router)

		dlDir := t.TempDir()

		item, err := newItem(
			m.mu,
			"testfile.bin",
			"sftp://127.0.0.1:1/path/testfile.bin", // non-existent host, credentials stripped
			dlDir,
			"sftp-dispatch-hash",
			1024,
			true,
			&itemOpts{AbsoluteLocation: dlDir},
		)
		if err != nil {
			t.Fatalf("newItem error: %v", err)
		}
		item.Protocol = ProtoSFTP
		item.Parts = map[int64]*ItemPart{
			0: {Hash: "part0", FinalOffset: 1023, Compiled: false},
		}
		m.UpdateItem(item)

		// Resume — will fail to connect (port 1), but should NOT be "protocol not supported"
		_, err = m.ResumeDownload(nil, "sftp-dispatch-hash", &ResumeDownloadOpts{
			Handlers: &Handlers{},
		})
		if err == nil {
			t.Fatal("expected error for SFTP to non-existent host")
		}
		// The key assertion: it should NOT be a "protocol not supported" error
		if strings.Contains(err.Error(), "resume not supported for protocol") {
			t.Errorf("SFTP should dispatch through SchemeRouter, got protocol error: %v", err)
		}
	})

	t.Run("SFTP resume with real mock server", func(t *testing.T) {
		// End-to-end: add SFTP item with credentials, resume from stored URL with creds
		testContent := bytes.Repeat([]byte{0xAB}, 1024)
		result := startMockSFTPServer(t,
			[]testFileEntry{{path: "pub/testfile.bin", content: testContent}},
			withPasswordAuth("testuser", "testpass"),
		)
		defer result.cleanup()

		m := newTestManager(t)
		defer m.Close()
		router := NewSchemeRouter(nil)
		m.SetSchemeRouter(router)

		restore := setupTestKnownHosts(t, result)
		defer restore()

		dlDir := t.TempDir()
		sftpURL := result.sftpURL("testuser", "testpass", "pub/testfile.bin")

		// Store the URL WITH credentials for resume (simulates user re-providing creds)
		// In production, the CLI would prompt for credentials on resume.
		pd, err := newSFTPProtocolDownloader(sftpURL, &DownloaderOpts{DownloadDirectory: dlDir})
		if err != nil {
			t.Fatalf("factory error: %v", err)
		}
		probe, err := pd.Probe(context.Background())
		if err != nil {
			t.Fatalf("Probe error: %v", err)
		}

		// Store with credentials in URL (normally stripped, but for this test we keep them)
		err = m.AddProtocolDownload(pd, probe, sftpURL, ProtoSFTP, &Handlers{}, &AddDownloadOpts{
			AbsoluteLocation: dlDir,
		})
		if err != nil {
			t.Fatalf("AddProtocolDownload error: %v", err)
		}

		hash := pd.GetHash()

		// Create partial file
		partialPath := filepath.Join(dlDir, "testfile.bin")
		partialData := bytes.Repeat([]byte{0xAB}, 512)
		if err := os.WriteFile(partialPath, partialData, DefaultFileMode); err != nil {
			t.Fatalf("failed to write partial file: %v", err)
		}

		item := m.GetItem(hash)
		item.Downloaded = 512
		m.UpdateItem(item)

		// Resume
		resumedItem, err := m.ResumeDownload(nil, hash, &ResumeDownloadOpts{
			Handlers: &Handlers{},
		})
		if err != nil {
			t.Fatalf("ResumeDownload error: %v", err)
		}
		if resumedItem == nil {
			t.Fatal("ResumeDownload returned nil item")
		}

		// Verify item has a dAlloc set (ProtocolDownloader)
		dAlloc := resumedItem.getDAlloc()
		if dAlloc == nil {
			t.Fatal("item.dAlloc is nil after ResumeDownload")
		}
		// Verify it's an SFTP downloader (not HTTP adapter)
		if _, ok := dAlloc.(*sftpProtocolDownloader); !ok {
			t.Errorf("expected *sftpProtocolDownloader, got %T", dAlloc)
		}
	})

	t.Run("HTTP resume path unchanged", func(t *testing.T) {
		// This is a regression test -- HTTP items must still use initDownloader path
		m := newTestManager(t)
		defer m.Close()

		dlDir := t.TempDir()

		// Create HTTP item manually
		item, err := newItem(
			m.mu,
			"httpfile.bin",
			"http://127.0.0.1:1/httpfile.bin", // non-existent host
			dlDir,
			"http-regression-hash",
			1024,
			true,
			&itemOpts{AbsoluteLocation: dlDir},
		)
		if err != nil {
			t.Fatalf("newItem error: %v", err)
		}
		item.Protocol = ProtoHTTP
		m.UpdateItem(item)

		// Create dldata dir for integrity validation
		if err := os.MkdirAll(filepath.Join(DlDataDir, item.Hash), 0755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		// Resume HTTP -- should go through initDownloader path (will fail to connect, but
		// the error should NOT be "protocol not supported")
		_, err = m.ResumeDownload(&http.Client{}, "http-regression-hash", &ResumeDownloadOpts{
			Handlers: &Handlers{},
		})
		// We expect a connection error (not a protocol error)
		if err != nil && strings.Contains(err.Error(), "resume not supported for protocol") {
			t.Errorf("HTTP should use initDownloader path, got protocol error: %v", err)
		}
	})
}

func TestResumeDownloadSFTPIntegrityGuard(t *testing.T) {
	t.Run("SFTP skips validateDownloadIntegrity", func(t *testing.T) {
		// Verify that SFTP items pass the protocol guard even without a DlDataDir.
		// Uses a non-existent host — we only test the guard logic, not the actual connection.
		m := newTestManager(t)
		defer m.Close()
		router := NewSchemeRouter(nil)
		m.SetSchemeRouter(router)

		dlDir := t.TempDir()

		item, err := newItem(
			m.mu,
			"testfile.bin",
			"sftp://127.0.0.1:1/path/testfile.bin",
			dlDir,
			"sftp-guard-hash",
			1024,
			true,
			&itemOpts{AbsoluteLocation: dlDir},
		)
		if err != nil {
			t.Fatalf("newItem error: %v", err)
		}
		item.Protocol = ProtoSFTP
		item.Downloaded = 100
		item.Parts = map[int64]*ItemPart{
			0: {Hash: "part0", FinalOffset: 1023, Compiled: false},
		}
		m.UpdateItem(item)

		// Create partial file so the guard passes
		partialPath := filepath.Join(dlDir, "testfile.bin")
		if err := os.WriteFile(partialPath, bytes.Repeat([]byte{0xAB}, 100), DefaultFileMode); err != nil {
			t.Fatalf("failed to write partial file: %v", err)
		}

		// Resume — will fail during Probe (auth error), NOT during protocol guard
		_, err = m.ResumeDownload(nil, "sftp-guard-hash", &ResumeDownloadOpts{
			Handlers: &Handlers{},
		})
		// Error should be an auth/connection error, NOT "resume not supported" or ErrDownloadDataMissing
		if err != nil {
			if strings.Contains(err.Error(), "resume not supported") {
				t.Errorf("SFTP should pass protocol guard, got: %v", err)
			}
			if errors.Is(err, ErrDownloadDataMissing) {
				t.Errorf("SFTP should skip validateDownloadIntegrity, got ErrDownloadDataMissing: %v", err)
			}
		}
	})

	t.Run("SFTP with Downloaded>0 but missing destination returns error", func(t *testing.T) {
		m := newTestManager(t)
		defer m.Close()
		router := NewSchemeRouter(nil)
		m.SetSchemeRouter(router)

		dlDir := t.TempDir()

		// Manually create an SFTP item that claims progress but has no file
		item, err := newItem(
			m.mu,
			"missing.bin",
			"sftp://127.0.0.1:1/path/missing.bin",
			dlDir,
			"sftp-missing-hash",
			1024,
			true,
			&itemOpts{AbsoluteLocation: dlDir},
		)
		if err != nil {
			t.Fatalf("newItem error: %v", err)
		}
		item.Protocol = ProtoSFTP
		item.Downloaded = 500 // Claims progress
		item.Parts = map[int64]*ItemPart{
			0: {Hash: "part0", FinalOffset: 1023, Compiled: false},
		}
		m.UpdateItem(item)

		// No destination file exists on disk
		_, err = m.ResumeDownload(nil, "sftp-missing-hash", &ResumeDownloadOpts{
			Handlers: &Handlers{},
		})
		if err == nil {
			t.Fatal("expected error for missing destination file with Downloaded>0")
		}
		if !strings.Contains(err.Error(), "missing") {
			t.Errorf("expected 'missing' in error, got: %v", err)
		}
	})

	t.Run("SFTP with Downloaded=0 passes guard", func(t *testing.T) {
		// When Downloaded==0, the protocol guard should pass (no dest file needed).
		// The dispatch will then try to connect and fail — but the guard passed.
		m := newTestManager(t)
		defer m.Close()
		router := NewSchemeRouter(nil)
		m.SetSchemeRouter(router)

		dlDir := t.TempDir()

		item, err := newItem(
			m.mu,
			"testfile.bin",
			"sftp://127.0.0.1:1/path/testfile.bin",
			dlDir,
			"sftp-d0-hash",
			1024,
			true,
			&itemOpts{AbsoluteLocation: dlDir},
		)
		if err != nil {
			t.Fatalf("newItem error: %v", err)
		}
		item.Protocol = ProtoSFTP
		item.Downloaded = 0 // Never started
		item.Parts = map[int64]*ItemPart{
			0: {Hash: "part0", FinalOffset: 1023, Compiled: false},
		}
		m.UpdateItem(item)

		// Resume — guard should pass, then fail on connection
		_, err = m.ResumeDownload(nil, "sftp-d0-hash", &ResumeDownloadOpts{
			Handlers: &Handlers{},
		})
		// Should NOT be a "destination file missing" error — Downloaded=0 skips that check
		if err != nil && strings.Contains(err.Error(), "destination file missing") {
			t.Errorf("Downloaded=0 should not require destination file, got: %v", err)
		}
		// Should NOT be a "resume not supported" error
		if err != nil && strings.Contains(err.Error(), "resume not supported") {
			t.Errorf("SFTP should dispatch through SchemeRouter, got: %v", err)
		}
	})

	t.Run("HTTP with missing segment dir still returns ErrDownloadDataMissing", func(t *testing.T) {
		// Regression: HTTP validation must still work
		m := newTestManager(t)
		defer m.Close()

		dlDir := t.TempDir()

		item, err := newItem(
			m.mu,
			"httpfile.bin",
			"http://127.0.0.1:1/httpfile.bin",
			dlDir,
			"http-integrity-hash",
			1024,
			true,
			&itemOpts{AbsoluteLocation: dlDir},
		)
		if err != nil {
			t.Fatalf("newItem error: %v", err)
		}
		item.Protocol = ProtoHTTP
		item.Downloaded = 100
		item.Parts = map[int64]*ItemPart{
			0: {Hash: "part0", FinalOffset: 1023, Compiled: false},
		}
		m.UpdateItem(item)

		// Do NOT create DlDataDir — integrity check should fail
		_, err = m.ResumeDownload(&http.Client{}, "http-integrity-hash", &ResumeDownloadOpts{
			Handlers: &Handlers{},
		})
		if err == nil {
			t.Fatal("expected ErrDownloadDataMissing for HTTP with missing data dir")
		}
		if !errors.Is(err, ErrDownloadDataMissing) {
			t.Errorf("expected ErrDownloadDataMissing, got: %v", err)
		}
	})
}
