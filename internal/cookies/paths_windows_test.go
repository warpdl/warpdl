//go:build windows

package cookies

import (
	"path/filepath"
	"testing"
)

// TestGetBrowserCookiePathsForEnv_Firefox verifies Firefox uses APPDATA for profiles.ini.
func TestGetBrowserCookiePathsForEnv_Firefox(t *testing.T) {
	localAppData := `C:\Users\user\AppData\Local`
	appData := `C:\Users\user\AppData\Roaming`

	specs := getBrowserCookiePathsForEnv(localAppData, appData)

	var ff *browserSpec
	for i := range specs {
		if specs[i].Name == "Firefox" {
			ff = &specs[i]
			break
		}
	}
	if ff == nil {
		t.Fatal("Firefox browserSpec not found")
	}
	if len(ff.ProfilesIniPaths) == 0 {
		t.Fatal("Firefox ProfilesIniPaths is empty")
	}
	if len(ff.CookiePaths) != 0 {
		t.Errorf("Firefox should use ProfilesIniPaths only; got CookiePaths %v", ff.CookiePaths)
	}

	expected := filepath.Join(appData, "Mozilla", "Firefox", "profiles.ini")
	if ff.ProfilesIniPaths[0] != expected {
		t.Errorf("Windows Firefox profiles.ini: want %q, got %q", expected, ff.ProfilesIniPaths[0])
	}
}

// TestGetBrowserCookiePathsForEnv_LibreWolf verifies LibreWolf uses APPDATA.
func TestGetBrowserCookiePathsForEnv_LibreWolf(t *testing.T) {
	localAppData := `C:\Users\user\AppData\Local`
	appData := `C:\Users\user\AppData\Roaming`

	specs := getBrowserCookiePathsForEnv(localAppData, appData)

	var lw *browserSpec
	for i := range specs {
		if specs[i].Name == "LibreWolf" {
			lw = &specs[i]
			break
		}
	}
	if lw == nil {
		t.Fatal("LibreWolf browserSpec not found")
	}

	expected := filepath.Join(appData, "LibreWolf", "profiles.ini")
	if lw.ProfilesIniPaths[0] != expected {
		t.Errorf("Windows LibreWolf profiles.ini: want %q, got %q", expected, lw.ProfilesIniPaths[0])
	}
}

// TestGetBrowserCookiePathsForEnv_Chrome verifies Chrome uses LOCALAPPDATA.
func TestGetBrowserCookiePathsForEnv_Chrome(t *testing.T) {
	localAppData := `C:\Users\user\AppData\Local`
	appData := `C:\Users\user\AppData\Roaming`

	specs := getBrowserCookiePathsForEnv(localAppData, appData)

	var ch *browserSpec
	for i := range specs {
		if specs[i].Name == "Chrome" {
			ch = &specs[i]
			break
		}
	}
	if ch == nil {
		t.Fatal("Chrome browserSpec not found")
	}
	if len(ch.CookiePaths) < 2 {
		t.Fatalf("Chrome should have at least 2 CookiePaths, got %d", len(ch.CookiePaths))
	}

	base := filepath.Join(localAppData, "Google", "Chrome", "User Data", "Default")
	wantNetwork := filepath.Join(base, "Network", "Cookies")
	wantDirect := filepath.Join(base, "Cookies")

	if ch.CookiePaths[0] != wantNetwork {
		t.Errorf("Windows Chrome primary: want %q, got %q", wantNetwork, ch.CookiePaths[0])
	}
	if ch.CookiePaths[1] != wantDirect {
		t.Errorf("Windows Chrome fallback: want %q, got %q", wantDirect, ch.CookiePaths[1])
	}
}

// TestGetBrowserCookiePathsForEnv_Chromium verifies Chromium uses LOCALAPPDATA.
func TestGetBrowserCookiePathsForEnv_Chromium(t *testing.T) {
	localAppData := `C:\Users\user\AppData\Local`
	appData := `C:\Users\user\AppData\Roaming`

	specs := getBrowserCookiePathsForEnv(localAppData, appData)

	var cr *browserSpec
	for i := range specs {
		if specs[i].Name == "Chromium" {
			cr = &specs[i]
			break
		}
	}
	if cr == nil {
		t.Fatal("Chromium browserSpec not found")
	}

	base := filepath.Join(localAppData, "Chromium", "User Data", "Default")
	wantNetwork := filepath.Join(base, "Network", "Cookies")
	if cr.CookiePaths[0] != wantNetwork {
		t.Errorf("Windows Chromium primary: want %q, got %q", wantNetwork, cr.CookiePaths[0])
	}
}

// TestGetBrowserCookiePathsForEnv_Edge verifies Edge uses LOCALAPPDATA.
func TestGetBrowserCookiePathsForEnv_Edge(t *testing.T) {
	localAppData := `C:\Users\user\AppData\Local`
	appData := `C:\Users\user\AppData\Roaming`

	specs := getBrowserCookiePathsForEnv(localAppData, appData)

	var ed *browserSpec
	for i := range specs {
		if specs[i].Name == "Edge" {
			ed = &specs[i]
			break
		}
	}
	if ed == nil {
		t.Fatal("Edge browserSpec not found")
	}

	base := filepath.Join(localAppData, "Microsoft", "Edge", "User Data", "Default")
	wantNetwork := filepath.Join(base, "Network", "Cookies")
	if ed.CookiePaths[0] != wantNetwork {
		t.Errorf("Windows Edge primary: want %q, got %q", wantNetwork, ed.CookiePaths[0])
	}
}

// TestGetBrowserCookiePathsForEnv_Brave verifies Brave uses LOCALAPPDATA.
func TestGetBrowserCookiePathsForEnv_Brave(t *testing.T) {
	localAppData := `C:\Users\user\AppData\Local`
	appData := `C:\Users\user\AppData\Roaming`

	specs := getBrowserCookiePathsForEnv(localAppData, appData)

	var br *browserSpec
	for i := range specs {
		if specs[i].Name == "Brave" {
			br = &specs[i]
			break
		}
	}
	if br == nil {
		t.Fatal("Brave browserSpec not found")
	}

	base := filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "User Data", "Default")
	wantNetwork := filepath.Join(base, "Network", "Cookies")
	if br.CookiePaths[0] != wantNetwork {
		t.Errorf("Windows Brave primary: want %q, got %q", wantNetwork, br.CookiePaths[0])
	}
}

// TestGetBrowserCookiePathsForEnv_PriorityOrder verifies priority order on Windows.
func TestGetBrowserCookiePathsForEnv_PriorityOrder(t *testing.T) {
	localAppData := `C:\Users\user\AppData\Local`
	appData := `C:\Users\user\AppData\Roaming`

	specs := getBrowserCookiePathsForEnv(localAppData, appData)

	wantOrder := []string{"Firefox", "LibreWolf", "Chrome", "Chromium", "Edge", "Brave"}
	if len(specs) < len(wantOrder) {
		t.Fatalf("expected at least %d specs, got %d", len(wantOrder), len(specs))
	}
	for i, name := range wantOrder {
		if specs[i].Name != name {
			t.Errorf("priority[%d]: want %q, got %q", i, name, specs[i].Name)
		}
	}
}
