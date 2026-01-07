package nativehost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestChromeManifest verifies Chrome manifest generation
func TestChromeManifest(t *testing.T) {
	hostPath := "/usr/local/bin/warpdl"
	extensionID := "abcdefghijklmnopqrstuvwxyzabcdef"

	manifest := GenerateChromeManifest(hostPath, extensionID)

	var m ChromeManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("Failed to unmarshal manifest: %v", err)
	}

	if m.Name != HostName {
		t.Errorf("Name = %s, want %s", m.Name, HostName)
	}
	if m.Description == "" {
		t.Error("Description should not be empty")
	}
	if m.Path != hostPath {
		t.Errorf("Path = %s, want %s", m.Path, hostPath)
	}
	if m.Type != "stdio" {
		t.Errorf("Type = %s, want stdio", m.Type)
	}
	if len(m.AllowedOrigins) != 1 {
		t.Errorf("AllowedOrigins length = %d, want 1", len(m.AllowedOrigins))
	}
	expectedOrigin := "chrome-extension://" + extensionID + "/"
	if m.AllowedOrigins[0] != expectedOrigin {
		t.Errorf("AllowedOrigins[0] = %s, want %s", m.AllowedOrigins[0], expectedOrigin)
	}
}

// TestFirefoxManifest verifies Firefox manifest generation
func TestFirefoxManifest(t *testing.T) {
	hostPath := "/usr/local/bin/warpdl"
	extensionID := "warpdl@example.com"

	manifest := GenerateFirefoxManifest(hostPath, extensionID)

	var m FirefoxManifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("Failed to unmarshal manifest: %v", err)
	}

	if m.Name != HostName {
		t.Errorf("Name = %s, want %s", m.Name, HostName)
	}
	if m.Description == "" {
		t.Error("Description should not be empty")
	}
	if m.Path != hostPath {
		t.Errorf("Path = %s, want %s", m.Path, hostPath)
	}
	if m.Type != "stdio" {
		t.Errorf("Type = %s, want stdio", m.Type)
	}
	if len(m.AllowedExtensions) != 1 {
		t.Errorf("AllowedExtensions length = %d, want 1", len(m.AllowedExtensions))
	}
	if m.AllowedExtensions[0] != extensionID {
		t.Errorf("AllowedExtensions[0] = %s, want %s", m.AllowedExtensions[0], extensionID)
	}
}

// TestManifestPaths verifies correct manifest paths for different browsers and platforms
func TestManifestPaths(t *testing.T) {
	tests := []struct {
		browser  Browser
		platform string
		contains string
	}{
		{BrowserChrome, "darwin", "Google/Chrome/NativeMessagingHosts"},
		{BrowserChrome, "linux", ".config/google-chrome/NativeMessagingHosts"},
		{BrowserFirefox, "darwin", "Mozilla/NativeMessagingHosts"},
		{BrowserFirefox, "linux", ".mozilla/native-messaging-hosts"},
		{BrowserChromium, "darwin", "Chromium/NativeMessagingHosts"},
		{BrowserChromium, "linux", ".config/chromium/NativeMessagingHosts"},
		{BrowserEdge, "darwin", "Microsoft Edge/NativeMessagingHosts"},
		{BrowserEdge, "linux", ".config/microsoft-edge/NativeMessagingHosts"},
		{BrowserBrave, "darwin", "BraveSoftware/Brave-Browser/NativeMessagingHosts"},
		{BrowserBrave, "linux", ".config/BraveSoftware/Brave-Browser/NativeMessagingHosts"},
	}

	for _, tt := range tests {
		t.Run(string(tt.browser)+"_"+tt.platform, func(t *testing.T) {
			path := getManifestPath(tt.browser, tt.platform, "/home/testuser")
			if !strings.Contains(path, tt.contains) {
				t.Errorf("Path %s should contain %s", path, tt.contains)
			}
		})
	}
}

// TestInstallManifest verifies manifest installation to filesystem
func TestInstallManifest(t *testing.T) {
	// Skip on Windows as paths differ significantly
	if runtime.GOOS == "windows" {
		t.Skip("Skipping manifest file test on Windows")
	}

	tmpDir := t.TempDir()
	hostPath := filepath.Join(tmpDir, "warpdl")
	extensionID := "testextension"

	// Create a mock host binary
	if err := os.WriteFile(hostPath, []byte("#!/bin/sh\necho test"), 0755); err != nil {
		t.Fatalf("Failed to create mock host: %v", err)
	}

	installer := &ManifestInstaller{
		HostPath:           hostPath,
		ChromeExtensionID:  extensionID,
		FirefoxExtensionID: extensionID + "@example.com",
		BaseDir:            tmpDir,
	}

	// Install Chrome manifest
	chromePath, err := installer.InstallChrome(BrowserChrome)
	if err != nil {
		t.Fatalf("InstallChrome failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(chromePath); os.IsNotExist(err) {
		t.Errorf("Chrome manifest not created at %s", chromePath)
	}

	// Verify content
	content, err := os.ReadFile(chromePath)
	if err != nil {
		t.Fatalf("Failed to read manifest: %v", err)
	}

	var m ChromeManifest
	if err := json.Unmarshal(content, &m); err != nil {
		t.Fatalf("Failed to parse manifest: %v", err)
	}
	if m.Path != hostPath {
		t.Errorf("Manifest path = %s, want %s", m.Path, hostPath)
	}
}

// TestUninstallManifest verifies manifest removal
func TestUninstallManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping manifest file test on Windows")
	}

	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "test_manifest.json")

	// Create a test manifest file
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatalf("Failed to create test manifest: %v", err)
	}

	// Uninstall
	if err := UninstallManifest(manifestPath); err != nil {
		t.Fatalf("UninstallManifest failed: %v", err)
	}

	// Verify removal
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Error("Manifest should have been removed")
	}
}

// TestUninstallManifestNotExists verifies uninstall handles missing files gracefully
func TestUninstallManifestNotExists(t *testing.T) {
	err := UninstallManifest("/nonexistent/path/manifest.json")
	// Should not error on missing file
	if err != nil {
		t.Errorf("UninstallManifest should not error on missing file: %v", err)
	}
}

// TestSupportedBrowsers verifies all expected browsers are supported
func TestSupportedBrowsers(t *testing.T) {
	browsers := SupportedBrowsers()
	expected := []Browser{BrowserChrome, BrowserFirefox, BrowserChromium, BrowserEdge, BrowserBrave}

	if len(browsers) != len(expected) {
		t.Errorf("SupportedBrowsers() returned %d browsers, want %d", len(browsers), len(expected))
	}

	for _, b := range expected {
		found := false
		for _, sb := range browsers {
			if sb == b {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Browser %s not in SupportedBrowsers()", b)
		}
	}
}

// TestManifestInstallerValidation verifies installer validation
func TestManifestInstallerValidation(t *testing.T) {
	tests := []struct {
		name      string
		installer *ManifestInstaller
		wantErr   bool
	}{
		{
			name: "valid installer",
			installer: &ManifestInstaller{
				HostPath:           "/usr/bin/warpdl",
				ChromeExtensionID:  "abcdef",
				FirefoxExtensionID: "warpdl@example.com",
			},
			wantErr: false,
		},
		{
			name: "missing host path",
			installer: &ManifestInstaller{
				ChromeExtensionID:  "abcdef",
				FirefoxExtensionID: "warpdl@example.com",
			},
			wantErr: true,
		},
		{
			name: "missing chrome extension id",
			installer: &ManifestInstaller{
				HostPath:           "/usr/bin/warpdl",
				FirefoxExtensionID: "warpdl@example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.installer.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
