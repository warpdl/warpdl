package warplib

import (
	"bytes"
	"encoding/gob"
	"os"
	"testing"
)

// TestSSHKeyPathGOBRoundTrip verifies that Item.SSHKeyPath survives GOB encode/decode.
func TestSSHKeyPathGOBRoundTrip(t *testing.T) {
	original := &Item{
		Hash:       "test-gob-ssh-key",
		Name:       "file.bin",
		Url:        "sftp://host/file.bin",
		SSHKeyPath: "/custom/test/key",
		Protocol:   ProtoSFTP,
		Parts:      make(map[int64]*ItemPart),
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(original); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded Item
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.SSHKeyPath != "/custom/test/key" {
		t.Fatalf("expected SSHKeyPath '/custom/test/key', got %q", decoded.SSHKeyPath)
	}
	if decoded.Protocol != ProtoSFTP {
		t.Fatalf("expected ProtoSFTP, got %v", decoded.Protocol)
	}
}

// TestSSHKeyPathGOBBackwardCompat verifies that the pre-Phase-2 fixture
// decodes correctly with SSHKeyPath as zero value (empty string).
func TestSSHKeyPathGOBBackwardCompat(t *testing.T) {
	data, err := os.ReadFile("testdata/pre_phase2_userdata.warp")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var md ManagerData
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&md); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for hash, item := range md.Items {
		if item.SSHKeyPath != "" {
			t.Errorf("item %s: expected empty SSHKeyPath, got %q", hash, item.SSHKeyPath)
		}
	}
}

// TestSFTPResumePreservesCustomSSHKey verifies that when an SFTP download
// is added with a custom SSH key path, the key path is persisted in the Item
// and threaded through to NewDownloader on resume.
func TestSFTPResumePreservesCustomSSHKey(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	// Track what SSHKeyPath was passed to NewDownloader
	var capturedSSHKeyPath string
	mockFactory := func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
		capturedSSHKeyPath = opts.SSHKeyPath
		return &mockProtocolDownloader{
			hash:        "sftp-resume-test-hash",
			fileName:    "file.bin",
			downloadDir: base,
			probeResult: ProbeResult{FileName: "file.bin", ContentLength: 1024, Resumable: true},
		}, nil
	}

	router := NewSchemeRouter(nil)
	router.Register("sftp", mockFactory)
	m.SetSchemeRouter(router)

	// Create a mock protocol downloader for the initial add
	pd := &mockProtocolDownloader{
		hash:        "sftp-resume-test-hash",
		fileName:    "file.bin",
		downloadDir: base,
		probeResult: ProbeResult{FileName: "file.bin", ContentLength: 1024, Resumable: true},
	}

	// Add the download with SSHKeyPath
	err = m.AddProtocolDownload(pd, ProbeResult{
		FileName:      "file.bin",
		ContentLength: 1024,
		Resumable:     true,
	}, "sftp://host/file.bin", ProtoSFTP, &Handlers{}, &AddDownloadOpts{
		AbsoluteLocation: base,
		SSHKeyPath:       "/custom/key",
	})
	if err != nil {
		t.Fatalf("AddProtocolDownload: %v", err)
	}

	// Verify the stored item has SSHKeyPath
	item := m.GetItem("sftp-resume-test-hash")
	if item == nil {
		t.Fatal("item not found")
	}
	if item.SSHKeyPath != "/custom/key" {
		t.Fatalf("expected SSHKeyPath '/custom/key', got %q", item.SSHKeyPath)
	}

	// Stop the download to allow resume
	item.setDAlloc(nil)

	// Create dest file so resume integrity check passes
	os.MkdirAll(item.AbsoluteLocation, 0755)
	destFile := item.GetAbsolutePath()
	os.WriteFile(destFile, []byte("partial"), 0644)
	item.Downloaded = 7
	m.UpdateItem(item)

	// Resume the download
	_, err = m.ResumeDownload(nil, "sftp-resume-test-hash", &ResumeDownloadOpts{})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}

	// Verify the factory received the SSHKeyPath
	if capturedSSHKeyPath != "/custom/key" {
		t.Fatalf("expected NewDownloader to receive SSHKeyPath '/custom/key', got %q", capturedSSHKeyPath)
	}
}

// TestSFTPResumeDefaultKeyWhenNone verifies that when SSHKeyPath is empty,
// the resume path passes empty string (triggering default key fallback).
func TestSFTPResumeDefaultKeyWhenNone(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	var capturedSSHKeyPath string
	mockFactory := func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
		capturedSSHKeyPath = opts.SSHKeyPath
		return &mockProtocolDownloader{
			hash:        "sftp-default-key-hash",
			fileName:    "file.bin",
			downloadDir: base,
			probeResult: ProbeResult{FileName: "file.bin", ContentLength: 1024, Resumable: true},
		}, nil
	}

	router := NewSchemeRouter(nil)
	router.Register("sftp", mockFactory)
	m.SetSchemeRouter(router)

	pd := &mockProtocolDownloader{
		hash:        "sftp-default-key-hash",
		fileName:    "file.bin",
		downloadDir: base,
		probeResult: ProbeResult{FileName: "file.bin", ContentLength: 1024, Resumable: true},
	}

	// Add with empty SSHKeyPath
	err = m.AddProtocolDownload(pd, ProbeResult{
		FileName:      "file.bin",
		ContentLength: 1024,
		Resumable:     true,
	}, "sftp://host/file.bin", ProtoSFTP, &Handlers{}, &AddDownloadOpts{
		AbsoluteLocation: base,
	})
	if err != nil {
		t.Fatalf("AddProtocolDownload: %v", err)
	}

	item := m.GetItem("sftp-default-key-hash")
	if item == nil {
		t.Fatal("item not found")
	}
	if item.SSHKeyPath != "" {
		t.Fatalf("expected empty SSHKeyPath, got %q", item.SSHKeyPath)
	}

	item.setDAlloc(nil)
	os.MkdirAll(item.AbsoluteLocation, 0755)
	destFile := item.GetAbsolutePath()
	os.WriteFile(destFile, []byte("partial"), 0644)
	item.Downloaded = 7
	m.UpdateItem(item)

	_, err = m.ResumeDownload(nil, "sftp-default-key-hash", &ResumeDownloadOpts{})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}

	if capturedSSHKeyPath != "" {
		t.Fatalf("expected empty SSHKeyPath on resume, got %q", capturedSSHKeyPath)
	}
}
