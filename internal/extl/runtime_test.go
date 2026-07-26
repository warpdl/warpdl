package extl

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func TestRuntimeHelpers(t *testing.T) {
	runtime := goja.New()
	if _, err := runtime.RunString("function foo() {}"); err != nil {
		t.Fatalf("RunString: %v", err)
	}
	val := runtime.Get("foo")
	name, ok := getFunctionName(runtime, val)
	if !ok || name != "foo" {
		t.Fatalf("expected function name foo, got %q", name)
	}
	name, ok = getFunctionName(runtime, runtime.ToValue("bar"))
	if !ok || name != "bar" {
		t.Fatalf("expected string name bar, got %q", name)
	}
	jsPrint(goja.FunctionCall{Arguments: []goja.Value{runtime.ToValue("hi")}})
	if err := runtime.Set("boom", func(goja.FunctionCall) goja.Value {
		throw(runtime, `boom'); globalThis.injected = true; //`)
		return nil
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := runtime.RunString("boom()"); err == nil {
		t.Fatal("expected thrown error")
	}
	if value := runtime.Get("injected"); value != nil && !goja.IsUndefined(value) {
		t.Fatal("error text was evaluated as JavaScript")
	}
}

func TestInputWithCallback(t *testing.T) {
	runtime := goja.New()
	if _, err := runtime.RunString("function cb(v){ return v + '!'; }"); err != nil {
		t.Fatalf("RunString: %v", err)
	}

	fn := inputWithScanner(runtime, func() (string, error) { return "answer", nil })
	out := fn(goja.FunctionCall{Arguments: []goja.Value{runtime.ToValue("Q? "), runtime.ToValue("cb")}})
	if out.String() != "answer!" {
		t.Fatalf("unexpected callback output: %s", out.String())
	}
}

func TestInputWithoutCallback(t *testing.T) {
	runtime := goja.New()
	fn := inputWithScanner(runtime, func() (string, error) { return "plain", nil })
	out := fn(goja.FunctionCall{Arguments: []goja.Value{runtime.ToValue("Q? ")}})
	if out.String() != "plain" {
		t.Fatalf("unexpected input output: %s", out.String())
	}
}

func TestRuntimeRequire(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.js"), []byte("module.exports = {};"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	rt, err := NewRuntime(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	req := rt.require(dir)
	_ = req(goja.FunctionCall{Arguments: []goja.Value{rt.ToValue("mod.js")}})
	if len(rt.imported) != 1 || rt.imported[0] != "mod.js" {
		t.Fatalf("expected module to be imported")
	}
}

func TestRuntimeRequireMissingModule(t *testing.T) {
	dir := t.TempDir()
	rt, err := NewRuntime(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	req := rt.require(dir)
	assertPanics(t, func() {
		req(goja.FunctionCall{Arguments: []goja.Value{rt.ToValue("missing.js")}})
	})
	if len(rt.imported) != 0 {
		t.Fatalf("expected no imported modules")
	}
}

func TestGetFunctionNameNonMatch(t *testing.T) {
	runtime := goja.New()
	val := runtime.ToValue(time.Now())
	if _, ok := getFunctionName(runtime, val); ok {
		t.Fatalf("expected non-function name to return false")
	}
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
