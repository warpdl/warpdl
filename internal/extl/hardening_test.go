package extl

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func TestModuleExtractTimeoutAndRuntimeRecovery(t *testing.T) {
	dir := writePluginDir(t, `
var first = true;
function extract(url) {
	if (first) {
		first = false;
		while (true) {}
	}
	return url;
}`)
	module, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := module.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	module.runtime.executionTimeout = 25 * time.Millisecond

	if _, err := module.Extract("first"); !errors.Is(err, ErrExecutionTimeout) {
		t.Fatalf("Extract error = %v, want ErrExecutionTimeout", err)
	}
	result, err := module.Extract("second")
	if err != nil {
		t.Fatalf("runtime did not recover after interrupt: %v", err)
	}
	if result.URL != "second" {
		t.Fatalf("URL = %q, want second", result.URL)
	}
}

func TestModuleInputHonorsExecutionTimeout(t *testing.T) {
	dir := writePluginDir(t, `
	function extract() {
		return input("blocked input: ");
	}`)
	module, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := module.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	module.runtime.executionTimeout = 25 * time.Millisecond

	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdin = reader
	defer func() {
		os.Stdin = oldStdin
		_ = writer.Close()
		_ = reader.Close()
	}()

	result := make(chan error, 1)
	go func() {
		_, extractErr := module.Extract("unused")
		result <- extractErr
	}()
	select {
	case extractErr := <-result:
		if !errors.Is(extractErr, ErrExecutionTimeout) {
			t.Fatalf("Extract error = %v, want ErrExecutionTimeout", extractErr)
		}
	case <-time.After(time.Second):
		t.Fatal("input kept native extension execution blocked past its deadline")
	}
}

func TestExtensionHTTPRequestHasDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	runtime := goja.New()
	callback := requestCallback(runtime, &http.Client{}, 25*time.Millisecond)
	request := runtime.ToValue(Request{Method: http.MethodGet, URL: server.URL})
	start := time.Now()
	assertPanics(t, func() {
		callback(goja.FunctionCall{Arguments: []goja.Value{request}})
	})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request deadline took %v", elapsed)
	}
}

func TestExtensionHTTPRequestHonorsExecutionDeadline(t *testing.T) {
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
		close(requestCanceled)
	}))
	defer server.Close()

	dir := writePluginDir(t, `
var first = true;
function extract(url) {
	if (first) {
		first = false;
		request({method: "GET", url: url, headers: {}, body: ""});
	}
	return url;
}`)
	module, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := module.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	module.runtime.executionTimeout = 25 * time.Millisecond

	start := time.Now()
	if _, err := module.Extract(server.URL); !errors.Is(err, ErrExecutionTimeout) {
		t.Fatalf("Extract error = %v, want ErrExecutionTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("request exceeded execution timeout: %v", elapsed)
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("server did not observe request cancellation")
	}

	result, err := module.Extract("recovered")
	if err != nil {
		t.Fatalf("runtime did not recover after request timeout: %v", err)
	}
	if result.URL != "recovered" {
		t.Fatalf("URL = %q, want recovered", result.URL)
	}
}

func TestModuleExtractSerializesConcurrentCalls(t *testing.T) {
	dir := writePluginDir(t, `
var counter = 0;
function extract() {
	var next = counter + 1;
	for (var i = 0; i < 1000; i++) {}
	counter = next;
	return String(counter);
}`)
	module, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := module.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	const calls = 40
	results := make(chan string, calls)
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := module.Extract("unused")
			if err != nil {
				errs <- err
				return
			}
			results <- result.URL
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("Extract: %v", err)
	}

	seen := make(map[int]bool, calls)
	for result := range results {
		value, err := strconv.Atoi(result)
		if err != nil {
			t.Fatalf("Atoi(%q): %v", result, err)
		}
		seen[value] = true
	}
	for value := 1; value <= calls; value++ {
		if !seen[value] {
			t.Errorf("missing serialized result %d", value)
		}
	}
}

func TestModuleExtractSerializesConcurrentObjectExports(t *testing.T) {
	dir := writePluginDir(t, `
var exported = 0;
function extract() {
	return {
		get url() {
			var next = exported + 1;
			for (var i = 0; i < 1000; i++) {}
			exported = next;
			return String(next);
		},
		headers: {}
	};
}`)
	module, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := module.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	const calls = 40
	results := make(chan string, calls)
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := module.Extract("unused")
			if err != nil {
				errs <- err
				return
			}
			results <- result.URL
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("Extract: %v", err)
	}

	seen := make(map[int]bool, calls)
	for result := range results {
		value, err := strconv.Atoi(result)
		if err != nil {
			t.Fatalf("Atoi(%q): %v", result, err)
		}
		seen[value] = true
	}
	for value := 1; value <= calls; value++ {
		if !seen[value] {
			t.Errorf("missing serialized object export %d", value)
		}
	}
}

func TestModuleExtractGetterFailuresAreBounded(t *testing.T) {
	tests := []struct {
		name        string
		firstResult string
		wantTimeout bool
	}{
		{
			name: "infinite getter",
			firstResult: `{
				get url() { while (true) {} },
				headers: {}
			}`,
			wantTimeout: true,
		},
		{
			name: "throwing getter",
			firstResult: `{
				get url() { throw new Error("getter failed"); },
				headers: {}
			}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := writePluginDir(t, `
var first = true;
function extract(url) {
	if (first) {
		first = false;
		return `+test.firstResult+`;
	}
	return url;
}`)
			module, err := OpenModule(log.New(io.Discard, "", 0), dir)
			if err != nil {
				t.Fatalf("OpenModule: %v", err)
			}
			if err := module.Load(); err != nil {
				t.Fatalf("Load: %v", err)
			}
			module.runtime.executionTimeout = 25 * time.Millisecond

			started := time.Now()
			_, err = module.Extract("first")
			if err == nil {
				t.Fatal("getter failure returned no error")
			}
			if test.wantTimeout && !errors.Is(err, ErrExecutionTimeout) {
				t.Fatalf("Extract error = %v, want ErrExecutionTimeout", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("getter failure exceeded execution timeout: %v", elapsed)
			}

			result, err := module.Extract("recovered")
			if err != nil {
				t.Fatalf("runtime did not recover after getter failure: %v", err)
			}
			if result.URL != "recovered" {
				t.Fatalf("URL = %q, want recovered", result.URL)
			}
		})
	}
}

func TestModuleEntrypointSymbolGetterHonorsExecutionTimeout(t *testing.T) {
	runtime, err := NewRuntime(log.New(io.Discard, "", 0), t.TempDir())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	runtime.executionTimeout = 25 * time.Millisecond
	module := &Module{runtime: runtime}

	started := time.Now()
	err = module.loadEntrypoint(`
Object.defineProperty(globalThis, "extract", {
	configurable: true,
	get: function() { while (true) {} }
});`)
	if !errors.Is(err, ErrExecutionTimeout) {
		t.Fatalf("loadEntrypoint error = %v, want ErrExecutionTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("entrypoint symbol getter exceeded execution timeout: %v", elapsed)
	}

	_, err = runtime.runString(`
Object.defineProperty(globalThis, "extract", {
	configurable: true,
	value: function(url) { return url; }
});`)
	if err != nil {
		t.Fatalf("runtime did not recover after symbol getter timeout: %v", err)
	}
}

func TestAuthBindingInstallationHandlesMaliciousGlobalSetters(t *testing.T) {
	tests := []struct {
		name        string
		setter      string
		wantTimeout bool
	}{
		{
			name:        "infinite setter",
			setter:      `function() { while (true) {} }`,
			wantTimeout: true,
		},
		{
			name:   "throwing setter",
			setter: `function() { throw new Error("setter failed"); }`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := NewRuntime(log.New(io.Discard, "", 0), t.TempDir())
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}
			runtime.executionTimeout = 25 * time.Millisecond
			_, err = runtime.runString(`
Object.defineProperty(globalThis, "getAccessToken", {
	configurable: true,
	set: ` + test.setter + `
});`)
			if err != nil {
				t.Fatalf("install malicious setter: %v", err)
			}

			started := time.Now()
			err = runtime.registerAuthBindings(nil)
			if err == nil {
				t.Fatal("auth binding installation returned no error")
			}
			if test.wantTimeout && !errors.Is(err, ErrExecutionTimeout) {
				t.Fatalf("registerAuthBindings error = %v, want ErrExecutionTimeout", err)
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("auth binding installation exceeded execution timeout: %v", elapsed)
			}

			value, err := runtime.runString(`21 * 2`)
			if err != nil {
				t.Fatalf("runtime did not recover after setter failure: %v", err)
			}
			if got := value.ToInteger(); got != 42 {
				t.Fatalf("runtime recovery result = %d, want 42", got)
			}
		})
	}
}

func TestOpenModuleRejectsEscapingManifestPaths(t *testing.T) {
	tests := []struct {
		name       string
		entrypoint string
		assets     []string
	}{
		{name: "entrypoint", entrypoint: "../outside.js"},
		{name: "absolute entrypoint", entrypoint: filepath.Join(string(filepath.Separator), "outside.js")},
		{name: "asset", entrypoint: "main.js", assets: []string{"nested/../../outside.txt"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := map[string]any{
				"name":       "escape",
				"version":    "1",
				"matches":    []string{".*"},
				"entrypoint": test.entrypoint,
				"assets":     test.assets,
			}
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := OpenModule(log.New(io.Discard, "", 0), dir); !errors.Is(err, ErrPathOutsideModule) {
				t.Fatalf("OpenModule error = %v, want ErrPathOutsideModule", err)
			}
		})
	}
}

func TestRuntimeRequireConfinedToModule(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.js")
	if err := os.WriteFile(outside, []byte("module.exports = 'secret';"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runtime, err := NewRuntime(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	require := runtime.require(dir)
	assertPanics(t, func() {
		require(goja.FunctionCall{Arguments: []goja.Value{runtime.ToValue("../outside.js")}})
	})

	link := filepath.Join(dir, "link.js")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	assertPanics(t, func() {
		require(goja.FunctionCall{Arguments: []goja.Value{runtime.ToValue("link.js")}})
	})
}

func TestModuleFilesCannotEscapeThroughSymlinks(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.js")
	if err := os.WriteFile(outside, []byte(`function extract(){ return "outside"; }`), 0o600); err != nil {
		t.Fatalf("WriteFile outside: %v", err)
	}

	entryDir := t.TempDir()
	entryManifest := `{"name":"entry","version":"1","matches":[".*"],"entrypoint":"main.js"}`
	if err := os.WriteFile(filepath.Join(entryDir, "manifest.json"), []byte(entryManifest), 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(entryDir, "main.js")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	module, err := OpenModule(log.New(io.Discard, "", 0), entryDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := module.Load(); err == nil {
		t.Fatal("Load followed an entrypoint symlink outside the module")
	}

	assetDir := t.TempDir()
	assetManifest := `{"name":"asset","version":"1","matches":[".*"],"entrypoint":"main.js","assets":["asset.js"]}`
	if err := os.WriteFile(filepath.Join(assetDir, "manifest.json"), []byte(assetManifest), 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "main.js"), []byte(`function extract(url){ return url; }`), 0o600); err != nil {
		t.Fatalf("WriteFile entrypoint: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(assetDir, "asset.js")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	module, err = OpenModule(log.New(io.Discard, "", 0), assetDir)
	if err != nil {
		t.Fatalf("OpenModule: %v", err)
	}
	if err := module.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := migrateModule(module, "", t.TempDir()); err == nil {
		t.Fatal("migration followed an asset symlink outside the module")
	}
}

func TestHeaderCookieFilteringHasNoEmptyEntries(t *testing.T) {
	header := Header{std: map[string][]string{
		"X-Test":     {"value"},
		"Set-Cookie": {"secret=1"},
	}}
	if got := header.Entries(); len(got) != 1 || got[0][0] != "X-Test" {
		t.Fatalf("Entries = %#v", got)
	}
	if got := header.Keys(); len(got) != 1 || got[0] != "X-Test" {
		t.Fatalf("Keys = %#v", got)
	}
	if got := header.Values(); len(got) != 1 || got[0] != "value" {
		t.Fatalf("Values = %#v", got)
	}
	header.ForEach(func(goja.FunctionCall) goja.Value {
		t.Fatal("ForEach must not call a callback without its runtime")
		return nil
	})

	runtime := goja.New()
	header.runtime = runtime
	if err := runtime.Set("header", header); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := runtime.RunString(`
		var seen = "";
		header.ForEach(function(value, key) { seen = key + ":" + value; });
	`); err != nil {
		t.Fatalf("JavaScript ForEach: %v", err)
	}
	if got := runtime.Get("seen").String(); got != "X-Test:value" {
		t.Fatalf("seen = %q", got)
	}
}

func TestEngineOffloadUpdatesMovedModuleIndex(t *testing.T) {
	if err := SetEngineStore(t.TempDir()); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	engine, err := NewEngine(log.New(io.Discard, "", 0), nil, nil, nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = engine.Close() }()

	modules := make([]*Module, 3)
	sources := make([]string, 3)
	for i := range modules {
		dir := t.TempDir()
		sources[i] = dir
		manifest := `{"name":"module-` + strconv.Itoa(i) + `","version":"1","matches":[".*"],"entrypoint":"main.js"}`
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
			t.Fatalf("WriteFile manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte(`function extract(url){ return url; }`), 0o600); err != nil {
			t.Fatalf("WriteFile entrypoint: %v", err)
		}
		modules[i], err = engine.AddModule(dir)
		if err != nil {
			t.Fatalf("AddModule %d: %v", i, err)
		}
	}
	reloaded, err := engine.AddModule(sources[0])
	if err != nil {
		t.Fatalf("repeated AddModule: %v", err)
	}
	if len(engine.modules) != len(modules) {
		t.Fatalf("repeated AddModule created %d active modules, want %d", len(engine.modules), len(modules))
	}
	modules[0] = reloaded

	if _, err := engine.DeactiveModule(modules[1].ModuleId); err != nil {
		t.Fatalf("DeactiveModule: %v", err)
	}
	if got := engine.GetModule(modules[2].ModuleId); got != modules[2] {
		t.Fatalf("moved module index points to %p, want %p", got, modules[2])
	}
	if _, err := engine.DeactiveModule(modules[2].ModuleId); err != nil {
		t.Fatalf("deactivate moved module: %v", err)
	}
	if _, err := engine.ActivateModule(modules[2].ModuleId); err != nil {
		t.Fatalf("reactivate from module store: %v", err)
	}
	if _, err := engine.DeleteModule(modules[1].ModuleId); err != nil {
		t.Fatalf("delete deactivated module: %v", err)
	}
}

func TestEngineConcurrentReadsAndActivation(t *testing.T) {
	if err := SetEngineStore(t.TempDir()); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	engine, err := NewEngine(log.New(io.Discard, "", 0), nil, nil, nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = engine.Close() }()
	module, err := engine.AddModule(writeTestModule(t, t.TempDir()))
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	done := make(chan struct{})
	errs := make(chan error, 1)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			runEngineReader(engine, module.ModuleId, done, errs)
		}()
	}
	for range 10 {
		if _, err := engine.DeactiveModule(module.ModuleId); err != nil {
			t.Fatalf("DeactiveModule: %v", err)
		}
		if _, err := engine.ActivateModule(module.ModuleId); err != nil {
			t.Fatalf("ActivateModule: %v", err)
		}
	}
	close(done)
	readers.Wait()
	select {
	case err := <-errs:
		t.Fatalf("concurrent Extract: %v", err)
	default:
	}
}

func TestEngineCloseIsConcurrentSafeAndIdempotent(t *testing.T) {
	if err := SetEngineStore(t.TempDir()); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	engine, err := NewEngine(log.New(io.Discard, "", 0), nil, nil, nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	const saves = 20
	start := make(chan struct{})
	errs := make(chan error, saves)
	var wg sync.WaitGroup
	for range saves {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- engine.Save()
		}()
	}
	close(start)
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()
	close(errs)
	for saveErr := range errs {
		if saveErr != nil && !errors.Is(saveErr, ErrEngineClosed) {
			t.Errorf("concurrent Save error = %v", saveErr)
		}
	}

	if err := engine.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
	if err := engine.Save(); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("Save after Close = %v, want ErrEngineClosed", err)
	}
	if _, err := engine.AddModule("unused"); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("AddModule after Close = %v, want ErrEngineClosed", err)
	}
}

func TestEngineExtractDoesNotLogRawURL(t *testing.T) {
	if err := SetEngineStore(t.TempDir()); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}
	var logs bytes.Buffer
	engine, err := NewEngine(log.New(&logs, "", 0), nil, nil, nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer func() { _ = engine.Close() }()
	if _, err := engine.AddModule(writeTestModule(t, t.TempDir())); err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	const secret = "extension-url-secret"
	if _, err := engine.Extract("https://example.com/file?token=" + secret); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("extension log exposed raw URL: %s", logs.String())
	}
}

func runEngineReader(engine *Engine, moduleID string, done <-chan struct{}, errs chan<- error) {
	for {
		select {
		case <-done:
			return
		default:
			_ = engine.GetModule(moduleID)
			_ = engine.ListModules(true)
			if _, err := engine.Extract("http://example.com"); err != nil {
				select {
				case errs <- err:
				default:
				}
				return
			}
		}
	}
}
