package warplib

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// generateTestHostKey creates an ECDSA host key for testing.
func generateTestHostKey(t *testing.T) (ssh.PublicKey, ssh.Signer) {
	t.Helper()
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	return signer.PublicKey(), signer
}

// fakeAddr implements net.Addr for testing.
type fakeAddr struct {
	network string
	address string
}

func (a fakeAddr) Network() string { return a.network }
func (a fakeAddr) String() string  { return a.address }

func TestTOFUUnknownHost(t *testing.T) {
	dir := t.TempDir()
	khFile := filepath.Join(dir, "known_hosts")

	pubKey, _ := generateTestHostKey(t)
	callback := newTOFUHostKeyCallback(khFile)

	addr := fakeAddr{network: "tcp", address: "192.168.1.1:22"}

	// First connection to unknown host should auto-accept
	err := callback("192.168.1.1:22", addr, pubKey)
	if err != nil {
		t.Fatalf("unknown host should be auto-accepted, got error: %v", err)
	}

	// Verify file was created and contains the host
	data, err := os.ReadFile(khFile)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("known_hosts file should not be empty after TOFU accept")
	}
}

func TestTOFUKnownHostMatch(t *testing.T) {
	dir := t.TempDir()
	khFile := filepath.Join(dir, "known_hosts")

	pubKey, _ := generateTestHostKey(t)
	callback := newTOFUHostKeyCallback(khFile)

	addr := fakeAddr{network: "tcp", address: "192.168.1.1:22"}

	// First connection — auto-accept
	err := callback("192.168.1.1:22", addr, pubKey)
	if err != nil {
		t.Fatalf("first connection failed: %v", err)
	}

	// Second connection with same key — should pass
	err = callback("192.168.1.1:22", addr, pubKey)
	if err != nil {
		t.Fatalf("known host with matching key should pass, got error: %v", err)
	}
}

func TestTOFUHostKeyMismatch(t *testing.T) {
	dir := t.TempDir()
	khFile := filepath.Join(dir, "known_hosts")

	pubKey1, _ := generateTestHostKey(t)
	pubKey2, _ := generateTestHostKey(t)
	callback := newTOFUHostKeyCallback(khFile)

	addr := fakeAddr{network: "tcp", address: "192.168.1.1:22"}

	// First connection with key1 — auto-accept
	err := callback("192.168.1.1:22", addr, pubKey1)
	if err != nil {
		t.Fatalf("first connection failed: %v", err)
	}

	// Second connection with DIFFERENT key — should reject with MITM warning
	err = callback("192.168.1.1:22", addr, pubKey2)
	if err == nil {
		t.Fatal("changed host key should be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "host key changed") {
		t.Errorf("error should contain MITM warning, got: %v", err)
	}
	if !strings.Contains(err.Error(), "WARNING") {
		t.Errorf("error should contain WARNING, got: %v", err)
	}
}

func TestTOFUConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	khFile := filepath.Join(dir, "known_hosts")

	callback := newTOFUHostKeyCallback(khFile)

	// Launch concurrent TOFU appends for different hosts
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pubKey, _ := generateTestHostKey(t)
			host := net.JoinHostPort("192.168.1."+string(rune('0'+idx)), "22")
			addr := fakeAddr{network: "tcp", address: host}
			if err := callback(host, addr, pubKey); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent TOFU append error: %v", err)
	}

	// Verify file is not corrupted — should be readable
	data, err := os.ReadFile(khFile)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	// Each host should have one line
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 10 {
		t.Errorf("expected at least 10 lines, got %d", len(lines))
	}
}

func TestKnownHostsNormalize(t *testing.T) {
	dir := t.TempDir()

	t.Run("port 22 normalized to hostname only", func(t *testing.T) {
		khFile := filepath.Join(dir, "kh_port22")
		pubKey, _ := generateTestHostKey(t)
		callback := newTOFUHostKeyCallback(khFile)

		addr := fakeAddr{network: "tcp", address: "example.com:22"}
		err := callback("example.com:22", addr, pubKey)
		if err != nil {
			t.Fatalf("TOFU accept failed: %v", err)
		}

		data, err := os.ReadFile(khFile)
		if err != nil {
			t.Fatalf("read known_hosts: %v", err)
		}
		content := string(data)
		// Port 22 should normalize to just "example.com" (not "[example.com]:22")
		if strings.Contains(content, "[example.com]:22") {
			t.Error("port 22 should not appear as [example.com]:22")
		}
		if !strings.Contains(content, "example.com") {
			t.Error("should contain example.com")
		}
	})

	t.Run("non-22 port uses bracketed format", func(t *testing.T) {
		khFile := filepath.Join(dir, "kh_port2222")
		pubKey, _ := generateTestHostKey(t)
		callback := newTOFUHostKeyCallback(khFile)

		addr := fakeAddr{network: "tcp", address: "example.com:2222"}
		err := callback("[example.com]:2222", addr, pubKey)
		if err != nil {
			t.Fatalf("TOFU accept failed: %v", err)
		}

		data, err := os.ReadFile(khFile)
		if err != nil {
			t.Fatalf("read known_hosts: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "[example.com]:2222") {
			t.Errorf("non-22 port should use bracketed format, got: %s", content)
		}
	})
}

func TestTOFUReReadAfterAppend(t *testing.T) {
	dir := t.TempDir()
	khFile := filepath.Join(dir, "known_hosts")

	pubKey, _ := generateTestHostKey(t)
	callback := newTOFUHostKeyCallback(khFile)

	addr := fakeAddr{network: "tcp", address: "myhost.local:22"}

	// First connection — TOFU accept
	err := callback("myhost.local:22", addr, pubKey)
	if err != nil {
		t.Fatalf("first connection failed: %v", err)
	}

	// Second connection — should succeed via re-read (not cached)
	err = callback("myhost.local:22", addr, pubKey)
	if err != nil {
		t.Fatalf("subsequent connection should succeed after TOFU, got: %v", err)
	}
}

func TestKnownHostsDirCreation(t *testing.T) {
	dir := t.TempDir()
	// Use a nested path that doesn't exist yet
	khFile := filepath.Join(dir, "deep", "nested", "known_hosts")

	pubKey, _ := generateTestHostKey(t)
	callback := newTOFUHostKeyCallback(khFile)

	addr := fakeAddr{network: "tcp", address: "newhost.local:22"}

	err := callback("newhost.local:22", addr, pubKey)
	if err != nil {
		t.Fatalf("should auto-create directory, got: %v", err)
	}

	if _, err := os.Stat(khFile); os.IsNotExist(err) {
		t.Fatal("known_hosts file should have been created")
	}
}
