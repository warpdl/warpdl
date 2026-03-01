//go:build unix

package cookies

import (
	"os"
	"path/filepath"
	"runtime"
)

// getBrowserCookiePathsForHome returns browser specs using the given homeDir.
// This is the testable variant; getBrowserCookiePaths calls it with the real home.
func getBrowserCookiePathsForHome(homeDir string) []browserSpec {
	isDarwin := runtime.GOOS == "darwin"

	var specs []browserSpec

	// Firefox
	var ffIniPaths []string
	if isDarwin {
		ffIniPaths = []string{
			filepath.Join(homeDir, "Library", "Application Support", "Firefox", "profiles.ini"),
		}
	} else {
		ffIniPaths = []string{
			filepath.Join(homeDir, ".mozilla", "firefox", "profiles.ini"),
			filepath.Join(homeDir, "snap", "firefox", "common", ".mozilla", "firefox", "profiles.ini"),
		}
	}
	specs = append(specs, browserSpec{Name: "Firefox", ProfilesIniPaths: ffIniPaths})

	// LibreWolf
	var lwIniPaths []string
	if isDarwin {
		lwIniPaths = []string{
			filepath.Join(homeDir, "Library", "Application Support", "librewolf", "profiles.ini"),
		}
	} else {
		lwIniPaths = []string{
			filepath.Join(homeDir, ".librewolf", "profiles.ini"),
		}
	}
	specs = append(specs, browserSpec{Name: "LibreWolf", ProfilesIniPaths: lwIniPaths})

	// Chrome
	var chromePaths []string
	if isDarwin {
		base := filepath.Join(homeDir, "Library", "Application Support", "Google", "Chrome", "Default")
		chromePaths = []string{
			filepath.Join(base, "Network", "Cookies"),
			filepath.Join(base, "Cookies"),
		}
	} else {
		base := filepath.Join(homeDir, ".config", "google-chrome", "Default")
		chromePaths = []string{
			filepath.Join(base, "Network", "Cookies"),
			filepath.Join(base, "Cookies"),
		}
	}
	specs = append(specs, browserSpec{Name: "Chrome", CookiePaths: chromePaths})

	// Chromium
	var chromiumPaths []string
	if isDarwin {
		base := filepath.Join(homeDir, "Library", "Application Support", "Chromium", "Default")
		chromiumPaths = []string{
			filepath.Join(base, "Network", "Cookies"),
			filepath.Join(base, "Cookies"),
		}
	} else {
		base := filepath.Join(homeDir, ".config", "chromium", "Default")
		chromiumPaths = []string{
			filepath.Join(base, "Network", "Cookies"),
			filepath.Join(base, "Cookies"),
		}
	}
	specs = append(specs, browserSpec{Name: "Chromium", CookiePaths: chromiumPaths})

	// Edge
	var edgePaths []string
	if isDarwin {
		base := filepath.Join(homeDir, "Library", "Application Support", "Microsoft Edge", "Default")
		edgePaths = []string{
			filepath.Join(base, "Network", "Cookies"),
			filepath.Join(base, "Cookies"),
		}
	} else {
		base := filepath.Join(homeDir, ".config", "microsoft-edge", "Default")
		edgePaths = []string{
			filepath.Join(base, "Network", "Cookies"),
			filepath.Join(base, "Cookies"),
		}
	}
	specs = append(specs, browserSpec{Name: "Edge", CookiePaths: edgePaths})

	// Brave
	var bravePaths []string
	if isDarwin {
		base := filepath.Join(homeDir, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "Default")
		bravePaths = []string{
			filepath.Join(base, "Network", "Cookies"),
			filepath.Join(base, "Cookies"),
		}
	} else {
		base := filepath.Join(homeDir, ".config", "BraveSoftware", "Brave-Browser", "Default")
		bravePaths = []string{
			filepath.Join(base, "Network", "Cookies"),
			filepath.Join(base, "Cookies"),
		}
	}
	specs = append(specs, browserSpec{Name: "Brave", CookiePaths: bravePaths})

	return specs
}

// getBrowserCookiePaths returns browser specs rooted at the real user home directory.
func getBrowserCookiePaths() []browserSpec {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return getBrowserCookiePathsForHome(homeDir)
}
