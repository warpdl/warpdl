package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/warpdl/warpdl/internal/extl"
)

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

func writeModuleAt(t *testing.T, dir, main string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte(main), 0644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
}

func TestRunHelp(t *testing.T) {
	if err := run([]string{}); err != nil {
		t.Fatalf("run help: %v", err)
	}
}

func TestRunExtractMissing(t *testing.T) {
	if err := run([]string{"extract"}); err == nil {
		t.Fatalf("expected error for missing url")
	}
}

func TestRunExtract(t *testing.T) {
	base := t.TempDir()
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	if err := run([]string{"extract", "http://example.com"}); err != nil {
		t.Fatalf("run extract: %v", err)
	}
}

func TestRunLoadMissing(t *testing.T) {
	if err := run([]string{"load"}); err == nil {
		t.Fatalf("expected error for missing path")
	}
}

func TestRunNotImplemented(t *testing.T) {
	base := t.TempDir()
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	if err := run([]string{"list"}); err != nil {
		t.Fatalf("run list: %v", err)
	}
}

func TestRunEngineError(t *testing.T) {
	oldEngineStore := extl.DEBUG_ENGINE_STORE
	oldModuleStore := extl.DEBUG_MODULE_STORE
	extl.DEBUG_ENGINE_STORE = filepath.Join(t.TempDir(), "missing")
	extl.DEBUG_MODULE_STORE = filepath.Join(extl.DEBUG_ENGINE_STORE, "extstore")
	defer func() {
		extl.DEBUG_ENGINE_STORE = oldEngineStore
		extl.DEBUG_MODULE_STORE = oldModuleStore
	}()

	if err := run([]string{"extract", "http://example.com"}); err == nil {
		t.Fatalf("expected error for invalid engine store")
	}
}

func TestRunExtractError(t *testing.T) {
	base := t.TempDir()
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	modID := "mod1"
	modPath := filepath.Join(extl.DEBUG_MODULE_STORE, modID)
	writeModuleAt(t, modPath, "function extract(url){ return \"end\"; }\n")

	state := map[string]extl.LoadedModuleState{
		"dummy": {ModuleId: modID, Name: "TestExt", IsActivated: true},
	}
	engFile := filepath.Join(extl.DEBUG_ENGINE_STORE, "module_engine.json")
	engJSON, _ := json.Marshal(map[string]any{"loaded_modules": state})
	if err := os.WriteFile(engFile, engJSON, 0644); err != nil {
		t.Fatalf("WriteFile engine: %v", err)
	}

	if err := run([]string{"extract", "http://example.com"}); err == nil {
		t.Fatalf("expected error for extract interaction end")
	}
}

func TestRunLoad(t *testing.T) {
	base := t.TempDir()
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	modDir := writeTestModule(t, t.TempDir())
	if err := run([]string{"load", modDir}); err != nil {
		t.Fatalf("run load: %v", err)
	}
}

func TestRunLoadError(t *testing.T) {
	base := t.TempDir()
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	if err := run([]string{"load", filepath.Join(base, "missing")}); err == nil {
		t.Fatalf("expected error for missing module path")
	}
}

func TestMainHelp(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"debug/extl", "help"}
	defer func() { os.Args = oldArgs }()
	main()
}
