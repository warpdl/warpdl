package cookies

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// browserSpec describes a browser's cookie database candidate paths.
type browserSpec struct {
	// Name is the human-readable browser name (e.g., "Firefox").
	Name string
	// CookiePaths contains direct cookie file candidates for Chromium-family
	// browsers. The first path that exists on disk is used.
	CookiePaths []string
	// ProfilesIniPaths contains candidate paths to Firefox-style profiles.ini
	// files. Empty for Chromium-family browsers.
	ProfilesIniPaths []string
}

// parseProfilesIni parses a Firefox-style profiles.ini file and returns the
// absolute path to the default profile directory.
//
// Priority:
//  1. [Install*] section Default= key — used by modern Firefox
//  2. [Profile*] section with Default=1 — fallback for older profiles
//
// Returns an empty string (no error) if the file does not exist, cannot be
// read, or contains no identifiable default profile.
func parseProfilesIni(iniPath string) string {
	f, err := os.Open(iniPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	iniDir := filepath.Dir(iniPath)

	var installDefault string
	var profileDefault string
	var inInstallSection bool
	var inProfileSection bool
	var currentPath string
	var currentIsDefault bool

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			// Flush previous Profile section if it had Default=1.
			if inProfileSection && currentIsDefault && profileDefault == "" {
				profileDefault = currentPath
			}
			sectionName := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			inInstallSection = strings.HasPrefix(sectionName, "Install")
			inProfileSection = strings.HasPrefix(sectionName, "Profile")
			currentPath = ""
			currentIsDefault = false
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if inInstallSection && key == "Default" && installDefault == "" {
			installDefault = filepath.Join(iniDir, filepath.FromSlash(val))
		}
		if inProfileSection {
			if key == "Path" {
				currentPath = filepath.Join(iniDir, filepath.FromSlash(val))
			}
			if key == "Default" && val == "1" {
				currentIsDefault = true
			}
		}
	}
	// Flush the last section.
	if inProfileSection && currentIsDefault && profileDefault == "" {
		profileDefault = currentPath
	}

	if installDefault != "" {
		return installDefault
	}
	return profileDefault
}

// detectWithSpecs scans the given browser specs in order and returns cookies
// from the first valid cookie store found for the given domain.
// This function exists as a testable seam; production code calls DetectBrowserCookies.
func detectWithSpecs(domain string, specs []browserSpec) ([]Cookie, *CookieSource, error) {
	for _, spec := range specs {
		if len(spec.ProfilesIniPaths) > 0 {
			// Firefox-family: resolve profile via profiles.ini.
			for _, iniPath := range spec.ProfilesIniPaths {
				profileDir := parseProfilesIni(iniPath)
				if profileDir == "" {
					continue
				}
				cookiePath := filepath.Join(profileDir, "cookies.sqlite")
				if _, err := os.Stat(cookiePath); err != nil {
					continue
				}
				imported, source, err := ImportCookies(cookiePath, domain)
				if err != nil {
					continue
				}
				source.Browser = spec.Name
				return imported, source, nil
			}
		} else {
			// Chromium-family: check direct cookie file paths.
			for _, cookiePath := range spec.CookiePaths {
				if _, err := os.Stat(cookiePath); err != nil {
					continue
				}
				imported, source, err := ImportCookies(cookiePath, domain)
				if err != nil {
					continue
				}
				source.Browser = spec.Name
				return imported, source, nil
			}
		}
	}
	return nil, nil, fmt.Errorf(
		"no supported browser cookie store found (tried Firefox, LibreWolf, Chrome, Chromium, Edge, Brave)",
	)
}

// DetectBrowserCookies scans known browser cookie stores in priority order and
// returns cookies for the given domain from the first available store.
//
// Priority: Firefox > LibreWolf > Chrome > Chromium > Edge > Brave.
//
// Returns an error if no supported browser cookie store is found.
func DetectBrowserCookies(domain string) ([]Cookie, *CookieSource, error) {
	return detectWithSpecs(domain, getBrowserCookiePaths())
}
