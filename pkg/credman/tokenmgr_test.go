package credman

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

func newTestTokenMgr(t *testing.T) *TokenManager {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	p := filepath.Join(t.TempDir(), "tokens.gob")
	tm, err := NewTokenManager(p, key)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	return tm
}

func TestTokenMgrSetGetDelete(t *testing.T) {
	tm := newTestTokenMgr(t)
	k := types.TokenKey{PluginID: "gdrive", Account: "default"}

	if _, err := tm.Get(k); err == nil {
		t.Fatal("expected Get on empty store to error")
	}
	tok := &types.OAuth2Token{
		AccessToken:  "ACCESS",
		RefreshToken: "REFRESH",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       []string{"drive.readonly"},
	}
	if err := tm.Set(k, tok); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := tm.Get(k)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "ACCESS" || got.RefreshToken != "REFRESH" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if err := tm.Delete(k); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := tm.Get(k); err == nil {
		t.Fatal("post-delete Get should error")
	}
}

func TestTokenMgrPersistAcrossReopen(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	p := filepath.Join(t.TempDir(), "tokens.gob")

	tm, err := NewTokenManager(p, key)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	k := types.TokenKey{PluginID: "p", Account: "default"}
	_ = tm.Set(k, &types.OAuth2Token{AccessToken: "A", ExpiresAt: time.Now().Add(time.Hour)})
	_ = tm.Close()

	tm2, err := NewTokenManager(p, key)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer tm2.Close()
	got, err := tm2.Get(k)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.AccessToken != "A" {
		t.Fatalf("lost value across reopen, got %q", got.AccessToken)
	}
}

func TestTokenMgrList(t *testing.T) {
	tm := newTestTokenMgr(t)
	keys := []types.TokenKey{
		{PluginID: "a", Account: "default"},
		{PluginID: "a", Account: "work"},
		{PluginID: "b", Account: "default"},
	}
	for _, k := range keys {
		_ = tm.Set(k, &types.OAuth2Token{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)})
	}
	got := tm.List()
	if len(got) != 3 {
		t.Fatalf("List len=%d, want 3", len(got))
	}
}

func TestTokenMgrConcurrentAccess(t *testing.T) {
	tm := newTestTokenMgr(t)
	plugins := []string{"a", "b", "c", "d"}
	accounts := []string{"default", "work", "personal"}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k := types.TokenKey{
				PluginID: plugins[i%len(plugins)],
				Account:  accounts[i%len(accounts)],
			}
			tok := &types.OAuth2Token{
				AccessToken: "v",
				ExpiresAt:   time.Now().Add(time.Hour),
			}
			switch i % 4 {
			case 0:
				_ = tm.Set(k, tok)
			case 1:
				_, _ = tm.Get(k)
			case 2:
				_ = tm.List()
			case 3:
				_ = tm.Delete(k)
			}
		}(i)
	}
	wg.Wait()
}

func TestTokenMgrCloseRace(t *testing.T) {
	tm := newTestTokenMgr(t)
	k := types.TokenKey{PluginID: "p"}
	_ = tm.Set(k, &types.OAuth2Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)})

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tm.Close()
		}()
	}
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = tm.Get(k)
		}()
	}
	wg.Wait()
}

// TestTokenMgrLoadErrorClosesFile verifies that a GOB decode failure in
// load() releases the file handle before returning — regression guard for
// the pre-fix leak. Mirrors TestCookieManagerLoadErrorClosesFile.
func TestTokenMgrLoadErrorClosesFile(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	p := filepath.Join(t.TempDir(), "tokens.gob")
	// Write garbage so GOB decode fails.
	if err := os.WriteFile(p, []byte("not a valid gob"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := NewTokenManager(p, key)
	if err == nil {
		t.Fatal("expected decode error")
	}
	// File should be removable immediately — it would not be on Windows
	// if the handle had leaked. Also serves as a smoke test elsewhere.
	if err := os.Remove(p); err != nil {
		t.Fatalf("file not removable, likely leaked handle: %v", err)
	}
}

// TestTokenMgrSetRollsBackOnSaveFailure verifies that a failed save() in
// Set() leaves the in-memory map consistent with the on-disk state (i.e.
// the previous value, not the new one). Regression guard for I2.
func TestTokenMgrSetRollsBackOnSaveFailure(t *testing.T) {
	tm := newTestTokenMgr(t)
	k := types.TokenKey{PluginID: "p"}
	// Seed a value.
	orig := &types.OAuth2Token{AccessToken: "ORIG", ExpiresAt: time.Now().Add(time.Hour)}
	if err := tm.Set(k, orig); err != nil {
		t.Fatal(err)
	}
	// Break the store: close the file behind the manager's back so the
	// next save() fails.
	_ = tm.f.Close()
	tm.f = nil

	if err := tm.Set(k, &types.OAuth2Token{AccessToken: "NEW", ExpiresAt: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("expected save error")
	}
	// Repair: reopen the file so Get can re-decrypt.
	f, err := os.OpenFile(tm.filePath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	tm.f = f
	got, err := tm.Get(k)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "ORIG" {
		t.Fatalf("rollback lost: %+v", got)
	}
}
