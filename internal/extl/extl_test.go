package extl

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dop251/goja"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read error")
}

func writeTestModule(t *testing.T, dir string) string {
	t.Helper()
	modDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := map[string]any{
		"name":        "TestExt",
		"version":     "1.0",
		"description": "desc",
		"matches":     []string{".*"},
		"entrypoint":  "main.js",
		"assets":      []string{"asset.txt"},
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(modDir, "manifest.json"), b, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	main := `function extract(url) { return url + "?ext=1"; }
`
	if err := os.WriteFile(filepath.Join(modDir, "main.js"), []byte(main), 0644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "extra.js"), []byte("module.exports = {};\n"), 0644); err != nil {
		t.Fatalf("WriteFile extra: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "asset.txt"), []byte("asset"), 0644); err != nil {
		t.Fatalf("WriteFile asset: %v", err)
	}
	return modDir
}

func writeModuleWithMain(t *testing.T, dir, entrypoint, main string) string {
	t.Helper()
	modDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if entrypoint == "" {
		entrypoint = "main.js"
	}
	manifest := map[string]any{
		"name":        "TestExt",
		"version":     "1.0",
		"description": "desc",
		"matches":     []string{".*"},
		"entrypoint":  entrypoint,
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(modDir, "manifest.json"), b, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if main != "" {
		if err := os.WriteFile(filepath.Join(modDir, entrypoint), []byte(main), 0644); err != nil {
			t.Fatalf("WriteFile main: %v", err)
		}
	}
	return modDir
}

func TestOpenModuleInvalid(t *testing.T) {
	tmp := t.TempDir()
	if _, err := OpenModule(log.New(io.Discard, "", 0), tmp); err == nil {
		t.Fatalf("expected error for missing manifest")
	}
}

func TestModuleLoadExtract(t *testing.T) {
	modDir := writeTestModule(t, t.TempDir())
	m, err := OpenModule(log.New(io.Discard, "", 0), modDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.runtime.imported = []string{"extra.js"}
	url, err := m.Extract("http://example.com")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if url != "http://example.com?ext=1" {
		t.Fatalf("unexpected url: %s", url)
	}
}

func TestEngineModuleLifecycle(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	eng, err := NewEngine(log.New(io.Discard, "", 0), nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()
	modDir := writeTestModule(t, t.TempDir())
	mod, err := eng.AddModule(modDir)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}
	if mod.ModuleId == "" {
		t.Fatalf("expected module id")
	}
	if got := eng.GetModule(mod.ModuleId); got == nil {
		t.Fatalf("expected module to be retrievable")
	}
	if err := eng.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	extURL, err := eng.Extract("http://example.com")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if extURL == "http://example.com" {
		t.Fatalf("expected extract to modify url")
	}
	mods := eng.ListModules(true)
	if len(mods) == 0 {
		t.Fatalf("expected modules to be listed")
	}
	if _, err := eng.DeactiveModule(mod.ModuleId); err != nil {
		t.Fatalf("DeactiveModule: %v", err)
	}
	if _, err := eng.ActivateModule(mod.ModuleId); err != nil {
		t.Fatalf("ActivateModule: %v", err)
	}
	if _, err := eng.DeleteModule(mod.ModuleId); err != nil {
		t.Fatalf("DeleteModule: %v", err)
	}
}

func TestGetModuleNotFound(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	eng, err := NewEngine(log.New(io.Discard, "", 0), nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	if got := eng.GetModule("nonexistent"); got != nil {
		t.Fatalf("expected nil for missing module, got %+v", got)
	}
}

func TestActivateModuleNotFound(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	eng, err := NewEngine(log.New(io.Discard, "", 0), nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	if _, err := eng.ActivateModule("nonexistent"); err != ErrModuleNotFound {
		t.Fatalf("expected ErrModuleNotFound, got %v", err)
	}
}

func TestDeactivateModuleNotFound(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	eng, err := NewEngine(log.New(io.Discard, "", 0), nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	if _, err := eng.DeactiveModule("nonexistent"); err != ErrModuleNotFound {
		t.Fatalf("expected ErrModuleNotFound, got %v", err)
	}
}

func TestDeleteModuleNotFound(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	eng, err := NewEngine(log.New(io.Discard, "", 0), nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()

	if _, err := eng.DeleteModule("nonexistent"); err != ErrModuleNotFound {
		t.Fatalf("expected ErrModuleNotFound, got %v", err)
	}
}

func TestMigrateModule(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	modDir := writeTestModule(t, t.TempDir())
	m, err := OpenModule(log.New(io.Discard, "", 0), modDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.runtime.imported = []string{"extra.js"}
	if err := migrateModule(m, "", MODULE_STORE); err != nil {
		t.Fatalf("migrateModule: %v", err)
	}
	if m.ModuleId == "" {
		t.Fatalf("expected ModuleId to be set")
	}
	assetPath := filepath.Join(MODULE_STORE, m.ModuleId, "asset.txt")
	if _, err := os.Stat(assetPath); err != nil {
		t.Fatalf("expected asset to be migrated: %v", err)
	}
	extraPath := filepath.Join(MODULE_STORE, m.ModuleId, "extra.js")
	if _, err := os.Stat(extraPath); err != nil {
		t.Fatalf("expected extra module to be migrated: %v", err)
	}
}

func TestHeaderMethods(t *testing.T) {
	h := Header{std: http.Header{}, runtime: goja.New()}
	h.Set("X-Test", "one")
	if !h.Has("X-Test") {
		t.Fatalf("expected header to exist")
	}
	h.Append("X-Test", "two")
	if got := h.Get("X-Test"); got == "" {
		t.Fatalf("expected appended header")
	}
	h.std.Add("Set-Cookie", "a=1")
	if len(h.GetSetCookies()) != 1 {
		t.Fatalf("expected set-cookie")
	}
	if len(h.Keys()) == 0 || len(h.Values()) == 0 {
		t.Fatalf("expected keys and values")
	}
	entries := h.Entries()
	if len(entries) == 0 {
		t.Fatalf("expected entries")
	}
	count := 0
	h.ForEach(func(call goja.FunctionCall) goja.Value {
		count++
		return nil
	})
	if count == 0 {
		t.Fatalf("expected foreach callback")
	}
	h.Delete("X-Test")
	if h.Has("X-Test") {
		t.Fatalf("expected delete")
	}
}

func TestRequestCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "1")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	runtime := goja.New()
	client := &http.Client{}
	cb := _requestCallback(runtime, client)
	val := runtime.ToValue(Request{Method: http.MethodGet, URL: srv.URL})
	respVal := cb(goja.FunctionCall{Arguments: []goja.Value{val}})
	var out struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}
	if err := runtime.ExportTo(respVal, &out); err != nil {
		t.Fatalf("ExportTo: %v", err)
	}
	if out.StatusCode != http.StatusOK || out.Body != "ok" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestModuleLoadErrors(t *testing.T) {
	base := t.TempDir()
	modDir := writeModuleWithMain(t, base, "missing.js", "")
	m, err := OpenModule(log.New(io.Discard, "", 0), modDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := m.Load(); err != ErrEntrypointNotFound {
		t.Fatalf("expected ErrEntrypointNotFound, got %v", err)
	}

	modDir = writeModuleWithMain(t, t.TempDir(), "main.js", "function nope() {}\n")
	m, err = OpenModule(log.New(io.Discard, "", 0), modDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := m.Load(); err != ErrExtractNotDefined {
		t.Fatalf("expected ErrExtractNotDefined, got %v", err)
	}
}

func TestModuleExtractErrors(t *testing.T) {
	modDir := writeModuleWithMain(t, t.TempDir(), "main.js", "function extract(url){ return 1; }\n")
	m, err := OpenModule(log.New(io.Discard, "", 0), modDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := m.Extract("http://example.com"); err != ErrInvalidReturnType {
		t.Fatalf("expected ErrInvalidReturnType, got %v", err)
	}

	modDir = writeModuleWithMain(t, t.TempDir(), "main.js", "function extract(url){ return \"end\"; }\n")
	m, err = OpenModule(log.New(io.Discard, "", 0), modDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := m.Extract("http://example.com"); err != ErrInteractionEnded {
		t.Fatalf("expected ErrInteractionEnded, got %v", err)
	}
}

func TestRequestCallbackErrors(t *testing.T) {
	runtime := goja.New()
	cb := _requestCallback(runtime, &http.Client{})
	cb(goja.FunctionCall{})
	val := runtime.ToValue(Request{Method: "GET", URL: "://bad"})
	cb(goja.FunctionCall{Arguments: []goja.Value{val}})
}

func TestSetEngineStoreInvalid(t *testing.T) {
	if err := SetEngineStore(""); err == nil {
		t.Fatalf("expected error for invalid store path")
	}
}

func TestNewEngineLoadsModules(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	modID := "mod1"
	modDir := filepath.Join(MODULE_STORE, modID)
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := map[string]any{
		"name":        "TestExt",
		"version":     "1.0",
		"description": "desc",
		"matches":     []string{".*"},
		"entrypoint":  "main.js",
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(modDir, "manifest.json"), b, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	main := "function extract(url){ return url; }\n"
	if err := os.WriteFile(filepath.Join(modDir, "main.js"), []byte(main), 0644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	state := map[string]LoadedModuleState{
		"dummy": {ModuleId: modID, Name: "TestExt", IsActivated: true},
	}
	engFile := filepath.Join(ENGINE_STORE, "module_engine.json")
	engJSON, _ := json.Marshal(map[string]any{"loaded_modules": state})
	if err := os.WriteFile(engFile, engJSON, 0644); err != nil {
		t.Fatalf("WriteFile engine: %v", err)
	}
	eng, err := NewEngine(log.New(io.Discard, "", 0), nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer eng.Close()
	if eng.GetModule(modID) == nil {
		t.Fatalf("expected module to be loaded")
	}
}

func TestNewEngineInvalidJSON(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	engFile := filepath.Join(ENGINE_STORE, "module_engine.json")
	if err := os.WriteFile(engFile, []byte("{bad-json"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := NewEngine(log.New(io.Discard, "", 0), nil, false); err == nil {
		t.Fatalf("expected error for invalid engine json")
	}
}

func TestEngineSaveError(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "module_engine.json")
	if err := os.WriteFile(filePath, []byte("{}"), 0444); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	eng := &Engine{
		f:   f,
		enc: json.NewEncoder(f),
	}
	if err := eng.Save(); err == nil {
		t.Fatalf("expected save error for read-only file")
	}
}

func TestModuleMigratorHardMissingFile(t *testing.T) {
	base := t.TempDir()
	migrator := moduleMigrator{
		initialBasePath: base,
		finalBasePath:   filepath.Join(t.TempDir(), "out"),
	}
	if err := migrator.moduleMigratorHard("missing.txt"); err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestMigrateModuleMissingImportedFile(t *testing.T) {
	base := t.TempDir()
	if err := SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	modDir := writeModuleWithMain(t, t.TempDir(), "main.js", "function extract(url){ return url; }\n")
	m, err := OpenModule(log.New(io.Discard, "", 0), modDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	m.runtime.imported = []string{"missing.js"}
	hash := "missing-import"
	err = migrateModule(m, hash, MODULE_STORE)
	if err == nil {
		t.Fatalf("expected migrateModule error for missing imported file")
	}
	if _, statErr := os.Stat(filepath.Join(MODULE_STORE, hash)); !os.IsNotExist(statErr) {
		t.Fatalf("expected target directory to be cleaned up")
	}
}

func TestRequestCallbackErrorsAdditional(t *testing.T) {
	runtime := goja.New()
	clientErr := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("client error")
		}),
	}
	cb := _requestCallback(runtime, clientErr)
	val := runtime.ToValue(Request{Method: http.MethodGet, URL: "http://example.com"})
	_ = cb(goja.FunctionCall{Arguments: []goja.Value{val}})

	readerErr := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(errReader{}),
				Header:     make(http.Header),
			}, nil
		}),
	}
	cb = _requestCallback(runtime, readerErr)
	_ = cb(goja.FunctionCall{Arguments: []goja.Value{val}})

	cb = _requestCallback(runtime, &http.Client{})
	_ = cb(goja.FunctionCall{Arguments: []goja.Value{runtime.ToValue("bad")}})
}

// TestEngineFilePermissions verifies that engine file is created with secure permissions
func TestEngineFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission tests not applicable on Windows")
	}
	tmpDir := t.TempDir()
	originalStore := ENGINE_STORE
	ENGINE_STORE = tmpDir
	defer func() { ENGINE_STORE = originalStore }()

	logger := log.New(io.Discard, "", 0)
	engine, err := NewEngine(logger, nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	enginePath := filepath.Join(tmpDir, "module_engine.json")
	info, err := os.Stat(enginePath)
	if err != nil {
		t.Fatalf("stat engine file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0644 {
		t.Errorf("expected engine file permissions 0644, got %#o", perm)
	}
}
