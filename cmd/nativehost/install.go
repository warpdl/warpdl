package nativehost

import (
	"fmt"
	"os"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/nativehost"
)

func install(c *cli.Context) error {
	chromeID := c.String("chrome-extension-id")
	firefoxID := c.String("firefox-extension-id")
	browser := c.String("browser")

	// Validate required extension IDs
	if chromeID == "" && firefoxID == "" {
		return cli.NewExitError("at least one extension ID is required (--chrome-extension-id or --firefox-extension-id)", 1)
	}

	// Get the path to the warpdl binary
	hostPath, err := os.Executable()
	if err != nil {
		return cli.NewExitError(fmt.Sprintf("failed to get executable path: %v", err), 1)
	}

	installer := &nativehost.ManifestInstaller{
		HostPath:           hostPath,
		ChromeExtensionID:  chromeID,
		FirefoxExtensionID: firefoxID,
	}

	installed := []string{}
	errors := []string{}

	installChrome := func(b nativehost.Browser) {
		if chromeID == "" {
			return
		}
		path, err := installer.InstallChrome(b)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", b, err))
		} else {
			installed = append(installed, fmt.Sprintf("%s: %s", b, path))
		}
	}

	installFirefox := func() {
		if firefoxID == "" {
			return
		}
		path, err := installer.InstallFirefox()
		if err != nil {
			errors = append(errors, fmt.Sprintf("firefox: %v", err))
		} else {
			installed = append(installed, fmt.Sprintf("firefox: %s", path))
		}
	}

	switch browser {
	case "all":
		installChrome(nativehost.BrowserChrome)
		installChrome(nativehost.BrowserChromium)
		installChrome(nativehost.BrowserEdge)
		installChrome(nativehost.BrowserBrave)
		installFirefox()
	case "chrome":
		installChrome(nativehost.BrowserChrome)
	case "chromium":
		installChrome(nativehost.BrowserChromium)
	case "edge":
		installChrome(nativehost.BrowserEdge)
	case "brave":
		installChrome(nativehost.BrowserBrave)
	case "firefox":
		installFirefox()
	default:
		return cli.NewExitError(fmt.Sprintf("unknown browser: %s", browser), 1)
	}

	if len(installed) > 0 {
		fmt.Println("Installed manifests:")
		for _, m := range installed {
			fmt.Printf("  %s\n", m)
		}
	}

	if len(errors) > 0 {
		fmt.Println("\nErrors:")
		for _, e := range errors {
			fmt.Printf("  %s\n", e)
		}
		if len(installed) == 0 {
			return cli.NewExitError("installation failed", 1)
		}
	}

	return nil
}
