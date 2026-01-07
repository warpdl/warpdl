package nativehost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// HostName is the native messaging host identifier.
// This must match the "name" field in the manifest and be used by browser extensions.
const HostName = "com.warpdl.host"

// Browser represents a supported browser for native messaging.
type Browser string

const (
	BrowserChrome   Browser = "chrome"
	BrowserFirefox  Browser = "firefox"
	BrowserChromium Browser = "chromium"
	BrowserEdge     Browser = "edge"
	BrowserBrave    Browser = "brave"
)

// SupportedBrowsers returns all browsers that support native messaging.
func SupportedBrowsers() []Browser {
	return []Browser{BrowserChrome, BrowserFirefox, BrowserChromium, BrowserEdge, BrowserBrave}
}

// ChromeManifest represents Chrome/Chromium native messaging host manifest.
type ChromeManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

// FirefoxManifest represents Firefox native messaging host manifest.
type FirefoxManifest struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Path              string   `json:"path"`
	Type              string   `json:"type"`
	AllowedExtensions []string `json:"allowed_extensions"`
}

// GenerateChromeManifest creates a Chrome/Chromium native messaging manifest.
func GenerateChromeManifest(hostPath, extensionID string) []byte {
	m := ChromeManifest{
		Name:           HostName,
		Description:    "WarpDL Download Manager Native Host",
		Path:           hostPath,
		Type:           "stdio",
		AllowedOrigins: []string{"chrome-extension://" + extensionID + "/"},
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return b
}

// GenerateFirefoxManifest creates a Firefox native messaging manifest.
func GenerateFirefoxManifest(hostPath, extensionID string) []byte {
	m := FirefoxManifest{
		Name:              HostName,
		Description:       "WarpDL Download Manager Native Host",
		Path:              hostPath,
		Type:              "stdio",
		AllowedExtensions: []string{extensionID},
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return b
}

// getManifestPath returns the manifest file path for a given browser and platform.
func getManifestPath(browser Browser, platform, homeDir string) string {
	manifestFile := HostName + ".json"

	switch platform {
	case "darwin":
		appSupport := filepath.Join(homeDir, "Library", "Application Support")
		switch browser {
		case BrowserChrome:
			return filepath.Join(appSupport, "Google", "Chrome", "NativeMessagingHosts", manifestFile)
		case BrowserChromium:
			return filepath.Join(appSupport, "Chromium", "NativeMessagingHosts", manifestFile)
		case BrowserFirefox:
			return filepath.Join(appSupport, "Mozilla", "NativeMessagingHosts", manifestFile)
		case BrowserEdge:
			return filepath.Join(appSupport, "Microsoft Edge", "NativeMessagingHosts", manifestFile)
		case BrowserBrave:
			return filepath.Join(appSupport, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts", manifestFile)
		}
	case "linux":
		switch browser {
		case BrowserChrome:
			return filepath.Join(homeDir, ".config", "google-chrome", "NativeMessagingHosts", manifestFile)
		case BrowserChromium:
			return filepath.Join(homeDir, ".config", "chromium", "NativeMessagingHosts", manifestFile)
		case BrowserFirefox:
			return filepath.Join(homeDir, ".mozilla", "native-messaging-hosts", manifestFile)
		case BrowserEdge:
			return filepath.Join(homeDir, ".config", "microsoft-edge", "NativeMessagingHosts", manifestFile)
		case BrowserBrave:
			return filepath.Join(homeDir, ".config", "BraveSoftware", "Brave-Browser", "NativeMessagingHosts", manifestFile)
		}
	case "windows":
		// Windows uses registry, but manifest file path for reference
		// Actual installation requires registry keys
		return filepath.Join(homeDir, "AppData", "Local", string(browser), "NativeMessagingHosts", manifestFile)
	}
	return ""
}

// ManifestInstaller handles installation and removal of native messaging manifests.
type ManifestInstaller struct {
	HostPath           string
	ChromeExtensionID  string
	FirefoxExtensionID string
	BaseDir            string // Override for testing; empty uses real home dir
}

// Validate checks that all required fields are set.
func (m *ManifestInstaller) Validate() error {
	if m.HostPath == "" {
		return errors.New("host path is required")
	}
	if m.ChromeExtensionID == "" {
		return errors.New("chrome extension ID is required")
	}
	return nil
}

// getHomeDir returns the home directory, using BaseDir override if set.
func (m *ManifestInstaller) getHomeDir() string {
	if m.BaseDir != "" {
		return m.BaseDir
	}
	home, _ := os.UserHomeDir()
	return home
}

// InstallChrome installs a manifest for Chrome-based browsers.
func (m *ManifestInstaller) InstallChrome(browser Browser) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}

	homeDir := m.getHomeDir()
	platform := detectPlatform()
	manifestPath := getManifestPath(browser, platform, homeDir)

	if manifestPath == "" {
		return "", fmt.Errorf("unsupported browser/platform: %s/%s", browser, platform)
	}

	// Create directory if needed
	dir := filepath.Dir(manifestPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create manifest directory: %w", err)
	}

	// Generate and write manifest
	manifest := GenerateChromeManifest(m.HostPath, m.ChromeExtensionID)
	if err := os.WriteFile(manifestPath, manifest, 0644); err != nil {
		return "", fmt.Errorf("failed to write manifest: %w", err)
	}

	return manifestPath, nil
}

// InstallFirefox installs a manifest for Firefox.
func (m *ManifestInstaller) InstallFirefox() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	if m.FirefoxExtensionID == "" {
		return "", errors.New("firefox extension ID is required")
	}

	homeDir := m.getHomeDir()
	platform := detectPlatform()
	manifestPath := getManifestPath(BrowserFirefox, platform, homeDir)

	if manifestPath == "" {
		return "", fmt.Errorf("unsupported platform for Firefox: %s", platform)
	}

	// Create directory if needed
	dir := filepath.Dir(manifestPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create manifest directory: %w", err)
	}

	// Generate and write manifest
	manifest := GenerateFirefoxManifest(m.HostPath, m.FirefoxExtensionID)
	if err := os.WriteFile(manifestPath, manifest, 0644); err != nil {
		return "", fmt.Errorf("failed to write manifest: %w", err)
	}

	return manifestPath, nil
}

// UninstallManifest removes a manifest file.
func UninstallManifest(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // Already gone
	}
	return os.Remove(path)
}

// detectPlatform returns the current OS platform.
func detectPlatform() string {
	// This will be overridden in platform-specific files
	return detectPlatformImpl()
}
