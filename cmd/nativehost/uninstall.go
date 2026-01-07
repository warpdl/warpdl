package nativehost

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/nativehost"
)

func uninstall(c *cli.Context) error {
	browser := c.String("browser")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return cli.NewExitError(fmt.Sprintf("failed to get home directory: %v", err), 1)
	}

	removed := []string{}
	errors := []string{}

	manifestFile := nativehost.HostName + ".json"

	uninstallBrowser := func(b nativehost.Browser) {
		var path string
		switch b {
		case nativehost.BrowserChrome:
			path = filepath.Join(homeDir, "Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts", manifestFile)
		case nativehost.BrowserChromium:
			path = filepath.Join(homeDir, "Library", "Application Support", "Chromium", "NativeMessagingHosts", manifestFile)
		case nativehost.BrowserFirefox:
			path = filepath.Join(homeDir, "Library", "Application Support", "Mozilla", "NativeMessagingHosts", manifestFile)
		case nativehost.BrowserEdge:
			path = filepath.Join(homeDir, "Library", "Application Support", "Microsoft Edge", "NativeMessagingHosts", manifestFile)
		case nativehost.BrowserBrave:
			path = filepath.Join(homeDir, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "NativeMessagingHosts", manifestFile)
		}
		// Try macOS path first, then Linux
		if _, err := os.Stat(path); os.IsNotExist(err) {
			switch b {
			case nativehost.BrowserChrome:
				path = filepath.Join(homeDir, ".config", "google-chrome", "NativeMessagingHosts", manifestFile)
			case nativehost.BrowserChromium:
				path = filepath.Join(homeDir, ".config", "chromium", "NativeMessagingHosts", manifestFile)
			case nativehost.BrowserFirefox:
				path = filepath.Join(homeDir, ".mozilla", "native-messaging-hosts", manifestFile)
			case nativehost.BrowserEdge:
				path = filepath.Join(homeDir, ".config", "microsoft-edge", "NativeMessagingHosts", manifestFile)
			case nativehost.BrowserBrave:
				path = filepath.Join(homeDir, ".config", "BraveSoftware", "Brave-Browser", "NativeMessagingHosts", manifestFile)
			}
		}

		if err := nativehost.UninstallManifest(path); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", b, err))
		} else if _, err := os.Stat(path); os.IsNotExist(err) {
			removed = append(removed, fmt.Sprintf("%s: removed (or was not installed)", b))
		}
	}

	switch browser {
	case "all":
		for _, b := range nativehost.SupportedBrowsers() {
			uninstallBrowser(b)
		}
	case "chrome":
		uninstallBrowser(nativehost.BrowserChrome)
	case "chromium":
		uninstallBrowser(nativehost.BrowserChromium)
	case "edge":
		uninstallBrowser(nativehost.BrowserEdge)
	case "brave":
		uninstallBrowser(nativehost.BrowserBrave)
	case "firefox":
		uninstallBrowser(nativehost.BrowserFirefox)
	default:
		return cli.NewExitError(fmt.Sprintf("unknown browser: %s", browser), 1)
	}

	if len(removed) > 0 {
		fmt.Println("Uninstalled manifests:")
		for _, m := range removed {
			fmt.Printf("  %s\n", m)
		}
	}

	if len(errors) > 0 {
		fmt.Println("\nErrors:")
		for _, e := range errors {
			fmt.Printf("  %s\n", e)
		}
	}

	return nil
}
