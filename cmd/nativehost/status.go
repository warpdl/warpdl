package nativehost

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/nativehost"
)

func status(c *cli.Context) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return cli.NewExitError(fmt.Sprintf("failed to get home directory: %v", err), 1)
	}

	manifestFile := nativehost.HostName + ".json"

	fmt.Println("Native Messaging Host Status")
	fmt.Println("============================")
	fmt.Printf("Host Name: %s\n\n", nativehost.HostName)

	checkBrowser := func(name string, paths []string) {
		for _, path := range paths {
			fullPath := filepath.Join(path, manifestFile)
			if _, err := os.Stat(fullPath); err == nil {
				fmt.Printf("%s: Installed\n", name)
				fmt.Printf("  Path: %s\n", fullPath)
				return
			}
		}
		fmt.Printf("%s: Not installed\n", name)
	}

	// macOS paths
	macAppSupport := filepath.Join(homeDir, "Library", "Application Support")
	// Linux paths
	linuxConfig := filepath.Join(homeDir, ".config")

	checkBrowser("Chrome", []string{
		filepath.Join(macAppSupport, "Google", "Chrome", "NativeMessagingHosts"),
		filepath.Join(linuxConfig, "google-chrome", "NativeMessagingHosts"),
	})

	checkBrowser("Chromium", []string{
		filepath.Join(macAppSupport, "Chromium", "NativeMessagingHosts"),
		filepath.Join(linuxConfig, "chromium", "NativeMessagingHosts"),
	})

	checkBrowser("Firefox", []string{
		filepath.Join(macAppSupport, "Mozilla", "NativeMessagingHosts"),
		filepath.Join(homeDir, ".mozilla", "native-messaging-hosts"),
	})

	checkBrowser("Edge", []string{
		filepath.Join(macAppSupport, "Microsoft Edge", "NativeMessagingHosts"),
		filepath.Join(linuxConfig, "microsoft-edge", "NativeMessagingHosts"),
	})

	checkBrowser("Brave", []string{
		filepath.Join(macAppSupport, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"),
		filepath.Join(linuxConfig, "BraveSoftware", "Brave-Browser", "NativeMessagingHosts"),
	})

	return nil
}
