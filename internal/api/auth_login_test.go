package api

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// writeAuthExtension writes a manifest.json that includes an auth block
// so the engine attaches an OAuth2Provider on AddModule.
func writeAuthExtension(t *testing.T, dir string) string {
	t.Helper()
	modDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := map[string]any{
		"name":        "AuthExt",
		"version":     "1.0",
		"description": "authed",
		"matches":     []string{".*"},
		"entrypoint":  "main.js",
		"auth": map[string]any{
			"type":          "oauth2",
			"client_id":     "test-client",
			"scopes":        []string{"read", "write"},
			"authorize_url": "https://example.invalid/authorize",
			"token_url":     "https://example.invalid/token",
			"pkce":          "S256",
		},
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(modDir, "manifest.json"), b, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	main := "function extract(url) { return url; }\n"
	if err := os.WriteFile(filepath.Join(modDir, "main.js"), []byte(main), 0644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	return modDir
}

// newAuthTestApi builds an Api backed by an Engine that has both
// TokenManager + FlowRegistry wired, so OAuth2 providers actually
// attach on AddModule. This differs from newTestApi which passes nil.
func newAuthTestApi(t *testing.T) (*Api, *server.Pool, func()) {
	t.Helper()
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	// TokenManager needs a 32-byte key and a storage path.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	tm, err := credman.NewTokenManager(filepath.Join(base, "tokens.gob"), key)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	flows := auth.NewFlowRegistry(time.Minute)

	eng, err := extl.NewEngine(log.New(io.Discard, "", 0), nil, tm, flows, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	api, err := NewApi(log.New(io.Discard, "", 0), m, &http.Client{}, eng, nil, nil, "test", "abc123", "test")
	if err != nil {
		t.Fatalf("NewApi: %v", err)
	}
	pool := server.NewPool(log.New(io.Discard, "", 0))
	cleanup := func() {
		_ = m.Close()
		_ = eng.Close()
		_ = tm.Close()
		flows.Shutdown()
		if runtime.GOOS == "windows" {
			time.Sleep(250 * time.Millisecond)
		}
	}
	return api, pool, cleanup
}

func TestAuthLoginHandler_PKCE_Success(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	params := common.AuthLoginParams{
		PluginID:    ext.ModuleId,
		Account:     "",
		Scopes:      []string{"read"},
		RedirectURI: "http://127.0.0.1:54321/callback",
	}
	body, _ := json.Marshal(params)

	tag, msg, err := api.authLoginHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("authLoginHandler: %v", err)
	}
	if tag != common.UPDATE_AUTH_REQUIRED {
		t.Fatalf("expected tag UPDATE_AUTH_REQUIRED, got %q", tag)
	}
	res, ok := msg.(*common.AuthLoginResult)
	if !ok {
		t.Fatalf("expected *AuthLoginResult, got %T", msg)
	}
	if res.FlowID == "" {
		t.Fatalf("expected FlowID to be populated")
	}
	if res.AuthorizeURL == "" {
		t.Fatalf("expected AuthorizeURL to be populated")
	}
	if res.ExpiresAt == 0 {
		t.Fatalf("expected ExpiresAt to be populated")
	}

	u, err := url.Parse(res.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse AuthorizeURL: %v", err)
	}
	q := u.Query()
	if got := q.Get("response_type"); got != "code" {
		t.Errorf("response_type = %q, want code", got)
	}
	if got := q.Get("client_id"); got != "test-client" {
		t.Errorf("client_id = %q, want test-client", got)
	}
	if got := q.Get("state"); got == "" {
		t.Errorf("state missing")
	}
	if got := q.Get("code_challenge"); got == "" {
		t.Errorf("code_challenge missing")
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if got := q.Get("redirect_uri"); got != params.RedirectURI {
		t.Errorf("redirect_uri = %q, want %q", got, params.RedirectURI)
	}
	if got := q.Get("scope"); got != "read" {
		t.Errorf("scope = %q, want 'read'", got)
	}

	// Flow must be registered in the registry so Await can observe it.
	prov, ok := api.elEngine.GetModule(ext.ModuleId).Provider().(*auth.OAuth2Provider)
	if !ok {
		t.Fatalf("provider not *OAuth2Provider")
	}
	f := prov.FlowRegistry().Get(res.FlowID)
	if f == nil {
		t.Fatalf("flow not registered")
	}
	if f.CodeVerifier == "" {
		t.Errorf("expected CodeVerifier stored on flow")
	}
	if f.State == "" || f.State != q.Get("state") {
		t.Errorf("state mismatch: flow=%q url=%q", f.State, q.Get("state"))
	}
}

func TestAuthLoginHandler_PKCE_DefaultsScopesFromConfig(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	// Omit Scopes; authorize URL should include the manifest-declared
	// defaults ("read write").
	params := common.AuthLoginParams{
		PluginID:    ext.ModuleId,
		RedirectURI: "http://127.0.0.1:54321/callback",
	}
	body, _ := json.Marshal(params)

	_, msg, err := api.authLoginHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("authLoginHandler: %v", err)
	}
	res := msg.(*common.AuthLoginResult)
	u, _ := url.Parse(res.AuthorizeURL)
	if got := u.Query().Get("scope"); got != "read write" {
		t.Errorf("scope = %q, want 'read write'", got)
	}
}

func TestAuthLoginHandler_Device_Stub(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	params := common.AuthLoginParams{
		PluginID: ext.ModuleId,
		Flow:     "device",
	}
	body, _ := json.Marshal(params)
	tag, msg, err := api.authLoginHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("authLoginHandler: %v", err)
	}
	if tag != common.UPDATE_AUTH_REQUIRED {
		t.Fatalf("expected tag UPDATE_AUTH_REQUIRED")
	}
	res, ok := msg.(*common.AuthLoginResult)
	if !ok {
		t.Fatalf("expected *AuthLoginResult, got %T", msg)
	}
	if res.FlowID == "" {
		t.Fatalf("expected FlowID")
	}
	// Task 13 stub: device handler does not produce a user_code or
	// verification URL; Task 14's polling goroutine will.
	if res.UserCode != "" {
		t.Errorf("expected no UserCode in stub, got %q", res.UserCode)
	}
	if res.VerificationURL != "" {
		t.Errorf("expected no VerificationURL in stub, got %q", res.VerificationURL)
	}
	if res.AuthorizeURL != "" {
		t.Errorf("expected no AuthorizeURL in device flow, got %q", res.AuthorizeURL)
	}
}

func TestAuthLoginHandler_MissingPluginID(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.AuthLoginParams{})
	_, _, err := api.authLoginHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for missing plugin_id")
	}
}

func TestAuthLoginHandler_UnknownPlugin(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.AuthLoginParams{PluginID: "does-not-exist"})
	_, _, err := api.authLoginHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for unknown plugin")
	}
}

func TestAuthLoginHandler_NoAuthBlock(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	// writeTestExtension writes a manifest with NO auth block.
	path := writeTestExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	body, _ := json.Marshal(common.AuthLoginParams{PluginID: ext.ModuleId})
	_, _, err = api.authLoginHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for plugin with no auth block")
	}
}

func TestAuthLoginHandler_BadJSON(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	_, _, err := api.authLoginHandler(nil, pool, []byte("{"))
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}
