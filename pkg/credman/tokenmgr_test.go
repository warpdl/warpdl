package credman

import (
	"crypto/rand"
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
	k := types.TokenKey{PluginID: "p", Account: "default"}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = tm.Set(k, &types.OAuth2Token{AccessToken: "v", ExpiresAt: time.Now().Add(time.Hour)})
			_, _ = tm.Get(k)
		}(i)
	}
	wg.Wait()
}
