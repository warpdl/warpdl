package extl

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func writePluginDir(t *testing.T, entry string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"name":"t","version":"0","matches":["^x"],"entrypoint":"main.js"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte(entry), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestExtractStringReturn(t *testing.T) {
	dir := writePluginDir(t, `function extract(url){ return "https://resolved/"+url; }`)
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	res, err := m.Extract("abc")
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://resolved/abc" {
		t.Fatalf("URL=%q", res.URL)
	}
	if len(res.Headers) != 0 {
		t.Fatalf("unexpected headers: %v", res.Headers)
	}
}

func TestExtractObjectReturn(t *testing.T) {
	js := `
function extract(u) {
  return {
    url: "https://resolved/"+u,
    headers: {"Authorization": "Bearer XYZ", "X-Custom": "1"}
  };
}`
	dir := writePluginDir(t, js)
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	res, err := m.Extract("z")
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://resolved/z" {
		t.Fatalf("URL=%q", res.URL)
	}
	if res.Headers["Authorization"] != "Bearer XYZ" {
		t.Fatalf("missing auth header: %v", res.Headers)
	}
	if res.Headers["X-Custom"] != "1" {
		t.Fatalf("missing custom header: %v", res.Headers)
	}
}

func TestExtractInvalidReturnTypeErrors(t *testing.T) {
	dir := writePluginDir(t, `function extract(){ return 42; }`)
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Extract("u"); err == nil {
		t.Fatal("expected error for non-string non-object return")
	}
}

func TestExtractObjectMissingUrlErrors(t *testing.T) {
	dir := writePluginDir(t, `function extract(){ return {headers: {a:"b"}}; }`)
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Extract("u"); err == nil {
		t.Fatal("expected error for object without url")
	}
}

func TestExtractObjectNonStringHeaderErrors(t *testing.T) {
	dir := writePluginDir(t, `function extract(){ return {url: "x", headers: {a: 42}}; }`)
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Extract("u"); err == nil {
		t.Fatal("expected error for non-string header value")
	}
}

func TestExtractObjectEndSentinel(t *testing.T) {
	dir := writePluginDir(t, `function extract(){ return {url: "end"}; }`)
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Extract("u"); err != ErrInteractionEnded {
		t.Fatalf("expected ErrInteractionEnded, got %v", err)
	}
}

func TestEngineLoadsAuthProvider(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
		"name":"p","version":"0","matches":["^x"],"entrypoint":"main.js",
		"auth":{"type":"oauth2","client_id":"c","scopes":["a"],
		        "authorize_url":"https://example.com/a",
		        "token_url":"https://example.com/t"}
	}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte(`function extract(u){ return "x:"+typeof getAccessToken; }`), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Auth == nil {
		t.Fatal("manifest auth block not parsed")
	}
	if m.Auth.PKCEMethod != "S256" {
		t.Fatalf("PKCEMethod default not applied: %q", m.Auth.PKCEMethod)
	}
}
