//go:build windows

package cookies

import (
	"os"
	"path/filepath"
)

// getBrowserCookiePathsForEnv returns browser specs using the given environment
// variable values. This is the testable variant; getBrowserCookiePaths calls it
// with real values from os.Getenv.
func getBrowserCookiePathsForEnv(localAppData, appData string) []browserSpec {
	var specs []browserSpec

	// Firefox — uses APPDATA (Roaming)
	specs = append(specs, browserSpec{
		Name: "Firefox",
		ProfilesIniPaths: []string{
			filepath.Join(appData, "Mozilla", "Firefox", "profiles.ini"),
		},
	})

	// LibreWolf — uses APPDATA (Roaming)
	specs = append(specs, browserSpec{
		Name: "LibreWolf",
		ProfilesIniPaths: []string{
			filepath.Join(appData, "LibreWolf", "profiles.ini"),
		},
	})

	// Chrome — uses LOCALAPPDATA
	chromeBase := filepath.Join(localAppData, "Google", "Chrome", "User Data", "Default")
	specs = append(specs, browserSpec{
		Name: "Chrome",
		CookiePaths: []string{
			filepath.Join(chromeBase, "Network", "Cookies"),
			filepath.Join(chromeBase, "Cookies"),
		},
	})

	// Chromium — uses LOCALAPPDATA
	chromiumBase := filepath.Join(localAppData, "Chromium", "User Data", "Default")
	specs = append(specs, browserSpec{
		Name: "Chromium",
		CookiePaths: []string{
			filepath.Join(chromiumBase, "Network", "Cookies"),
			filepath.Join(chromiumBase, "Cookies"),
		},
	})

	// Edge — uses LOCALAPPDATA
	edgeBase := filepath.Join(localAppData, "Microsoft", "Edge", "User Data", "Default")
	specs = append(specs, browserSpec{
		Name: "Edge",
		CookiePaths: []string{
			filepath.Join(edgeBase, "Network", "Cookies"),
			filepath.Join(edgeBase, "Cookies"),
		},
	})

	// Brave — uses LOCALAPPDATA
	braveBase := filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "User Data", "Default")
	specs = append(specs, browserSpec{
		Name: "Brave",
		CookiePaths: []string{
			filepath.Join(braveBase, "Network", "Cookies"),
			filepath.Join(braveBase, "Cookies"),
		},
	})

	return specs
}

// getBrowserCookiePaths returns browser specs using real Windows environment variables.
func getBrowserCookiePaths() []browserSpec {
	return getBrowserCookiePathsForEnv(
		os.Getenv("LOCALAPPDATA"),
		os.Getenv("APPDATA"),
	)
}
