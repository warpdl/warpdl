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
	print(goja.FunctionCall{Arguments: []goja.Value{runtime.ToValue("hi")}})
	throw(runtime, "boom")
}

func TestInputWithCallback(t *testing.T) {
	runtime := goja.New()
	if _, err := runtime.RunString("function cb(v){ return v + '!'; }"); err != nil {
		t.Fatalf("RunString: %v", err)
	}

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("answer\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	fn := input(runtime)
	out := fn(goja.FunctionCall{Arguments: []goja.Value{runtime.ToValue("Q? "), runtime.ToValue("cb")}})
	if out.String() != "answer!" {
		t.Fatalf("unexpected callback output: %s", out.String())
	}
}

func TestInputWithoutCallback(t *testing.T) {
	runtime := goja.New()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("plain\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	fn := input(runtime)
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
	if v := req(goja.FunctionCall{Arguments: []goja.Value{rt.ToValue("missing.js")}}); v != nil {
		t.Fatalf("expected nil for missing module")
	}
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
