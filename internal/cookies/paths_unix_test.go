//go:build unix

package cookies

import (
	"path/filepath"
	"runtime"
	"testing"
)

// TestGetBrowserCookiePathsForHome_Firefox verifies that Firefox profile ini
// paths are returned at the correct OS-specific locations.
func TestGetBrowserCookiePathsForHome_Firefox(t *testing.T) {
	home := "/fake/home"
	specs := getBrowserCookiePathsForHome(home)

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
		t.Errorf("Firefox should use ProfilesIniPaths, not CookiePaths; got %v", ff.CookiePaths)
	}

	isDarwin := runtime.GOOS == "darwin"
	if isDarwin {
		expected := filepath.Join(home, "Library", "Application Support", "Firefox", "profiles.ini")
		if ff.ProfilesIniPaths[0] != expected {
			t.Errorf("macOS Firefox profiles.ini: want %q, got %q", expected, ff.ProfilesIniPaths[0])
		}
	} else {
		expected := filepath.Join(home, ".mozilla", "firefox", "profiles.ini")
		if ff.ProfilesIniPaths[0] != expected {
			t.Errorf("Linux Firefox profiles.ini primary: want %q, got %q", expected, ff.ProfilesIniPaths[0])
		}
		// Snap path should be the second candidate on Linux
		if len(ff.ProfilesIniPaths) < 2 {
			t.Fatal("Linux Firefox should have at least 2 ProfilesIniPaths (standard + snap)")
		}
		expectedSnap := filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox", "profiles.ini")
		if ff.ProfilesIniPaths[1] != expectedSnap {
			t.Errorf("Linux Firefox snap profiles.ini: want %q, got %q", expectedSnap, ff.ProfilesIniPaths[1])
		}
	}
}

// TestGetBrowserCookiePathsForHome_LibreWolf verifies LibreWolf profile ini paths.
func TestGetBrowserCookiePathsForHome_LibreWolf(t *testing.T) {
	home := "/fake/home"
	specs := getBrowserCookiePathsForHome(home)

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
	if len(lw.ProfilesIniPaths) == 0 {
		t.Fatal("LibreWolf ProfilesIniPaths is empty")
	}

	isDarwin := runtime.GOOS == "darwin"
	if isDarwin {
		expected := filepath.Join(home, "Library", "Application Support", "librewolf", "profiles.ini")
		if lw.ProfilesIniPaths[0] != expected {
			t.Errorf("macOS LibreWolf profiles.ini: want %q, got %q", expected, lw.ProfilesIniPaths[0])
		}
	} else {
		expected := filepath.Join(home, ".librewolf", "profiles.ini")
		if lw.ProfilesIniPaths[0] != expected {
			t.Errorf("Linux LibreWolf profiles.ini: want %q, got %q", expected, lw.ProfilesIniPaths[0])
		}
	}
}

// TestGetBrowserCookiePathsForHome_Chrome verifies Chrome cookie paths include
// both Network/Cookies and Cookies candidates.
func TestGetBrowserCookiePathsForHome_Chrome(t *testing.T) {
	home := "/fake/home"
	specs := getBrowserCookiePathsForHome(home)

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
	if len(ch.ProfilesIniPaths) != 0 {
		t.Errorf("Chrome should not have ProfilesIniPaths; got %v", ch.ProfilesIniPaths)
	}

	isDarwin := runtime.GOOS == "darwin"
	if isDarwin {
		base := filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default")
		wantNetwork := filepath.Join(base, "Network", "Cookies")
		wantDirect := filepath.Join(base, "Cookies")
		if ch.CookiePaths[0] != wantNetwork {
			t.Errorf("macOS Chrome primary: want %q, got %q", wantNetwork, ch.CookiePaths[0])
		}
		if ch.CookiePaths[1] != wantDirect {
			t.Errorf("macOS Chrome fallback: want %q, got %q", wantDirect, ch.CookiePaths[1])
		}
	} else {
		base := filepath.Join(home, ".config", "google-chrome", "Default")
		wantNetwork := filepath.Join(base, "Network", "Cookies")
		wantDirect := filepath.Join(base, "Cookies")
		if ch.CookiePaths[0] != wantNetwork {
			t.Errorf("Linux Chrome primary: want %q, got %q", wantNetwork, ch.CookiePaths[0])
		}
		if ch.CookiePaths[1] != wantDirect {
			t.Errorf("Linux Chrome fallback: want %q, got %q", wantDirect, ch.CookiePaths[1])
		}
	}
}

// TestGetBrowserCookiePathsForHome_Chromium verifies Chromium cookie paths.
func TestGetBrowserCookiePathsForHome_Chromium(t *testing.T) {
	home := "/fake/home"
	specs := getBrowserCookiePathsForHome(home)

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
	if len(cr.CookiePaths) < 2 {
		t.Fatalf("Chromium should have at least 2 CookiePaths, got %d", len(cr.CookiePaths))
	}

	isDarwin := runtime.GOOS == "darwin"
	if isDarwin {
		base := filepath.Join(home, "Library", "Application Support", "Chromium", "Default")
		wantNetwork := filepath.Join(base, "Network", "Cookies")
		if cr.CookiePaths[0] != wantNetwork {
			t.Errorf("macOS Chromium primary: want %q, got %q", wantNetwork, cr.CookiePaths[0])
		}
	} else {
		base := filepath.Join(home, ".config", "chromium", "Default")
		wantNetwork := filepath.Join(base, "Network", "Cookies")
		if cr.CookiePaths[0] != wantNetwork {
			t.Errorf("Linux Chromium primary: want %q, got %q", wantNetwork, cr.CookiePaths[0])
		}
	}
}

// TestGetBrowserCookiePathsForHome_Edge verifies Edge cookie paths.
func TestGetBrowserCookiePathsForHome_Edge(t *testing.T) {
	home := "/fake/home"
	specs := getBrowserCookiePathsForHome(home)

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
	if len(ed.CookiePaths) < 2 {
		t.Fatalf("Edge should have at least 2 CookiePaths, got %d", len(ed.CookiePaths))
	}

	isDarwin := runtime.GOOS == "darwin"
	if isDarwin {
		base := filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "Default")
		wantNetwork := filepath.Join(base, "Network", "Cookies")
		if ed.CookiePaths[0] != wantNetwork {
			t.Errorf("macOS Edge primary: want %q, got %q", wantNetwork, ed.CookiePaths[0])
		}
	} else {
		base := filepath.Join(home, ".config", "microsoft-edge", "Default")
		wantNetwork := filepath.Join(base, "Network", "Cookies")
		if ed.CookiePaths[0] != wantNetwork {
			t.Errorf("Linux Edge primary: want %q, got %q", wantNetwork, ed.CookiePaths[0])
		}
	}
}

// TestGetBrowserCookiePathsForHome_Brave verifies Brave cookie paths.
func TestGetBrowserCookiePathsForHome_Brave(t *testing.T) {
	home := "/fake/home"
	specs := getBrowserCookiePathsForHome(home)

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
	if len(br.CookiePaths) < 2 {
		t.Fatalf("Brave should have at least 2 CookiePaths, got %d", len(br.CookiePaths))
	}

	isDarwin := runtime.GOOS == "darwin"
	if isDarwin {
		base := filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "Default")
		wantNetwork := filepath.Join(base, "Network", "Cookies")
		if br.CookiePaths[0] != wantNetwork {
			t.Errorf("macOS Brave primary: want %q, got %q", wantNetwork, br.CookiePaths[0])
		}
	} else {
		base := filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser", "Default")
		wantNetwork := filepath.Join(base, "Network", "Cookies")
		if br.CookiePaths[0] != wantNetwork {
			t.Errorf("Linux Brave primary: want %q, got %q", wantNetwork, br.CookiePaths[0])
		}
	}
}

// TestGetBrowserCookiePathsForHome_PriorityOrder verifies that browsers are
// returned in the required priority order: Firefox, LibreWolf, Chrome, Chromium, Edge, Brave.
func TestGetBrowserCookiePathsForHome_PriorityOrder(t *testing.T) {
	home := "/fake/home"
	specs := getBrowserCookiePathsForHome(home)

	wantOrder := []string{"Firefox", "LibreWolf", "Chrome", "Chromium", "Edge", "Brave"}
	if len(specs) < len(wantOrder) {
		t.Fatalf("expected at least %d browser specs, got %d", len(wantOrder), len(specs))
	}
	for i, name := range wantOrder {
		if specs[i].Name != name {
			t.Errorf("priority[%d]: want %q, got %q", i, name, specs[i].Name)
		}
	}
}

// TestGetBrowserCookiePathsForHome_AllHavePaths verifies every spec has at
// least one path candidate.
func TestGetBrowserCookiePathsForHome_AllHavePaths(t *testing.T) {
	home := "/fake/home"
	specs := getBrowserCookiePathsForHome(home)

	for _, s := range specs {
		hasPaths := len(s.CookiePaths) > 0 || len(s.ProfilesIniPaths) > 0
		if !hasPaths {
			t.Errorf("browser %q has neither CookiePaths nor ProfilesIniPaths", s.Name)
		}
	}
}
