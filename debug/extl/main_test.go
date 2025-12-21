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

func TestMainHelp(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"debug/extl", "help"}
	defer func() { os.Args = oldArgs }()
	main()
}
