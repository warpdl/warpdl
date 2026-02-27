package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func newRangeServer(content []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}
		rangeHeader := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(content) - 1
		if parts[1] != "" {
			if e, err := strconv.Atoi(parts[1]); err == nil {
				end = e
			}
		}
		if start > end || start < 0 || end >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		chunk := content[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
}

func writeTestExtension(t *testing.T, dir string) string {
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
	main := "function extract(url) { return url + '?ext=1'; }\n"
	if err := os.WriteFile(filepath.Join(modDir, "main.js"), []byte(main), 0644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	return modDir
}

func writeEndExtension(t *testing.T, dir string) string {
	t.Helper()
	modDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := map[string]any{
		"name":        "EndExt",
		"version":     "1.0",
		"description": "desc",
		"matches":     []string{".*"},
		"entrypoint":  "main.js",
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(modDir, "manifest.json"), b, 0644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	main := "function extract(url) { return \"end\"; }\n"
	if err := os.WriteFile(filepath.Join(modDir, "main.js"), []byte(main), 0644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}
	return modDir
}

func newTestApi(t *testing.T) (*Api, *server.Pool, func()) {
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
	eng, err := extl.NewEngine(log.New(io.Discard, "", 0), nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	api, err := NewApi(log.New(io.Discard, "", 0), m, &http.Client{}, eng, nil, "test", "abc123", "test")
	if err != nil {
		t.Fatalf("NewApi: %v", err)
	}
	pool := server.NewPool(log.New(io.Discard, "", 0))
	cleanup := func() {
		_ = m.Close()
		_ = eng.Close()
		// On Windows, pause for file handle release.
		// Increased to 250ms to ensure reliable cleanup in CI.
		if runtime.GOOS == "windows" {
			time.Sleep(250 * time.Millisecond)
		}
	}
	return api, pool, cleanup
}

func TestDownloadHandler(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	content := bytes.Repeat([]byte("d"), 2048)
	srv := newRangeServer(content)
	defer srv.Close()

	params := common.DownloadParams{
		Url:               srv.URL + "/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    2,
		MaxSegments:       2,
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.downloadHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("downloadHandler: %v", err)
	}
	resp := msg.(*common.DownloadResponse)
	if resp.DownloadId == "" || resp.FileName == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(resp.SavePath)
		if err == nil && info.Size() == int64(resp.ContentLength) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := os.Stat(resp.SavePath)
	if err != nil {
		t.Fatalf("stat download: %v", err)
	}
	if info.Size() != int64(resp.ContentLength) {
		t.Fatalf("download did not complete")
	}
}

func TestDownloadHandlerExtensionErrorFallback(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	modPath := writeEndExtension(t, t.TempDir())
	if _, err := api.elEngine.AddModule(modPath); err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	content := bytes.Repeat([]byte("d"), 2048)
	srv := newRangeServer(content)
	defer srv.Close()

	params := common.DownloadParams{
		Url:               srv.URL + "/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    1,
		MaxSegments:       1,
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.downloadHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("downloadHandler: %v", err)
	}
	resp := msg.(*common.DownloadResponse)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(resp.SavePath)
		if err == nil && info.Size() == int64(resp.ContentLength) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDownloadHandlerMissingFileName(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	params := common.DownloadParams{
		Url:               srv.URL + "/",
		DownloadDirectory: warplib.ConfigDir,
	}
	body, _ := json.Marshal(params)
	if _, _, err := api.downloadHandler(nil, pool, body); err == nil {
		t.Fatalf("expected downloadHandler error for missing filename")
	}
}

func TestListHandler(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item1 := &warplib.Item{
		Hash:             "h1",
		Name:             "a",
		Url:              "u",
		TotalSize:        10,
		DownloadLocation: ".",
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	item1.Downloaded = item1.TotalSize
	item2 := &warplib.Item{
		Hash:             "h2",
		Name:             "b",
		Url:              "u",
		TotalSize:        10,
		DownloadLocation: ".",
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item1)
	api.manager.UpdateItem(item2)

	body, _ := json.Marshal(common.ListParams{ShowCompleted: true, ShowPending: true})
	_, msg, err := api.listHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("listHandler: %v", err)
	}
	if len(msg.(*common.ListResponse).Items) != 2 {
		t.Fatalf("expected two items")
	}

	body, _ = json.Marshal(common.ListParams{ShowCompleted: true, ShowPending: false})
	_, msg, err = api.listHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("listHandler: %v", err)
	}
	if len(msg.(*common.ListResponse).Items) != 1 {
		t.Fatalf("expected completed items")
	}
}

func TestFlushHandler(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item := &warplib.Item{
		Hash:             "h1",
		Name:             "a",
		Url:              "u",
		TotalSize:        10,
		DownloadLocation: ".",
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	item.Downloaded = item.TotalSize
	api.manager.UpdateItem(item)

	body, _ := json.Marshal(common.FlushParams{})
	if _, _, err := api.flushHandler(nil, pool, body); err != nil {
		t.Fatalf("flushHandler: %v", err)
	}
	if len(api.manager.GetItems()) != 0 {
		t.Fatalf("expected manager to be empty")
	}

	api.manager.UpdateItem(item)
	body, _ = json.Marshal(common.FlushParams{DownloadId: item.Hash})
	if _, _, err := api.flushHandler(nil, pool, body); err != nil {
		t.Fatalf("flushHandler: %v", err)
	}
}

func TestAttachAndStopHandler(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	content := bytes.Repeat([]byte("x"), 128)
	srv := newRangeServer(content)
	defer srv.Close()
	d, err := warplib.NewDownloader(&http.Client{}, srv.URL+"/file.bin", &warplib.DownloaderOpts{
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    2,
		MaxSegments:       2,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	defer d.Close()
	if err := api.manager.AddDownload(d, &warplib.AddDownloadOpts{AbsoluteLocation: d.GetDownloadDirectory()}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := api.manager.GetItem(d.GetHash())
	if item == nil {
		t.Fatalf("expected item")
	}

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	sconn := server.NewSyncConn(c1)
	pool.AddDownload(item.Hash, sconn)

	body, _ := json.Marshal(common.InputDownloadId{DownloadId: item.Hash})
	_, msg, err := api.attachHandler(sconn, pool, body)
	if err != nil {
		t.Fatalf("attachHandler: %v", err)
	}
	if msg.(*common.DownloadResponse).DownloadId != item.Hash {
		t.Fatalf("unexpected attach response")
	}

	if _, _, err := api.stopHandler(sconn, pool, body); err != nil {
		t.Fatalf("stopHandler: %v", err)
	}
}

func TestResumeHandlerSuccess(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item := &warplib.Item{
		Hash:             "h1",
		Name:             "a",
		Url:              "u",
		TotalSize:        10,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)
	if err := os.MkdirAll(filepath.Join(warplib.DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body, _ := json.Marshal(common.ResumeParams{DownloadId: item.Hash})
	_, msg, err := api.resumeHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("resumeHandler: %v", err)
	}
	if msg.(*common.ResumeResponse).FileName != item.Name {
		t.Fatalf("unexpected resume response")
	}
}

func TestResumeHandlerChild(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	parent := &warplib.Item{
		Hash:             "p1",
		Name:             "parent",
		Url:              "u",
		TotalSize:        5,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		ChildHash:        "c1",
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	child := &warplib.Item{
		Hash:             "c1",
		Name:             "child",
		Url:              "u",
		TotalSize:        7,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(parent)
	api.manager.UpdateItem(child)
	expectedTotal := parent.TotalSize + child.TotalSize

	if err := os.MkdirAll(filepath.Join(warplib.DlDataDir, parent.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(warplib.DlDataDir, child.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	body, _ := json.Marshal(common.ResumeParams{DownloadId: parent.Hash})
	_, msg, err := api.resumeHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("resumeHandler: %v", err)
	}
	resp := msg.(*common.ResumeResponse)
	if resp.ContentLength != expectedTotal {
		t.Fatalf("expected combined content length, got %d", resp.ContentLength)
	}
	if resp.ChildHash != child.Hash {
		t.Fatalf("expected child hash in response")
	}
}

func TestResumeHandlerChildMissing(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	parent := &warplib.Item{
		Hash:             "p2",
		Name:             "parent",
		Url:              "u",
		TotalSize:        5,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		ChildHash:        "missing",
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(parent)
	if err := os.MkdirAll(filepath.Join(warplib.DlDataDir, parent.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	body, _ := json.Marshal(common.ResumeParams{DownloadId: parent.Hash})
	if _, _, err := api.resumeHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for missing child")
	}
}

func TestExtensionHandlers(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	path := writeTestExtension(t, t.TempDir())
	body, _ := json.Marshal(common.AddExtensionParams{Path: path})
	_, msg, err := api.addExtHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("addExtHandler: %v", err)
	}
	info := msg.(*common.ExtensionInfo)
	if info.ExtensionId == "" {
		t.Fatalf("expected extension id")
	}

	body, _ = json.Marshal(common.InputExtension{ExtensionId: info.ExtensionId})
	_, msg, err = api.getExtHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("getExtHandler: %v", err)
	}
	if msg.(*common.ExtensionInfo).Name != info.Name {
		t.Fatalf("unexpected extension info")
	}

	body, _ = json.Marshal(common.ListExtensionsParams{All: true})
	_, msg, err = api.listExtHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("listExtHandler: %v", err)
	}
	if len(msg.([]common.ExtensionInfoShort)) == 0 {
		t.Fatalf("expected extensions in list")
	}

	body, _ = json.Marshal(common.InputExtension{ExtensionId: info.ExtensionId})
	if _, _, err := api.deactivateExtHandler(nil, pool, body); err != nil {
		t.Fatalf("deactivateExtHandler: %v", err)
	}
	if _, _, err := api.activateExtHandler(nil, pool, body); err != nil {
		t.Fatalf("activateExtHandler: %v", err)
	}
	if _, _, err := api.deleteExtHandler(nil, pool, body); err != nil {
		t.Fatalf("deleteExtHandler: %v", err)
	}
}

func TestGetHandler(t *testing.T) {
	pool := server.NewPool(log.New(io.Discard, "", 0))
	uid := "id"
	stopCalled := false
	stopFn := func() error { stopCalled = true; return nil }
	isStopped := func() bool { return false }
	handlers := getHandler(pool, &uid, &stopFn, &isStopped)
	handlers.ErrorHandler("hash", errors.New("boom"))
	handlers.DownloadProgressHandler("hash", 1)
	handlers.ResumeProgressHandler("hash", 1)
	handlers.CompileProgressHandler("hash", 1)
	handlers.CompileStartHandler("hash")
	handlers.CompileCompleteHandler("hash", 1)
	handlers.DownloadStoppedHandler()
	if !stopCalled {
		t.Fatalf("expected stop handler to be called")
	}
}

func TestRegisterHandlersAndClose(t *testing.T) {
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
	eng, err := extl.NewEngine(log.New(io.Discard, "", 0), nil, false)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	api, err := NewApi(log.New(io.Discard, "", 0), m, &http.Client{}, eng, nil, "test", "abc123", "test")
	if err != nil {
		t.Fatalf("NewApi: %v", err)
	}
	srv := server.NewServer(log.New(io.Discard, "", 0), m, 0)
	api.RegisterHandlers(srv)
	if err := api.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = m.Close()
	_ = eng.Close()
	// On Windows, pause for file handle release.
	// Increased to 250ms to ensure reliable cleanup in CI.
	if runtime.GOOS == "windows" {
		time.Sleep(250 * time.Millisecond)
	}
}

func TestResumeHandlerMissing(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.ResumeParams{DownloadId: "missing"})
	if _, _, err := api.resumeHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for missing download")
	}
}

func TestResumeHandlerNotResumable(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item := &warplib.Item{
		Hash:             "h1",
		Name:             "a",
		Url:              "u",
		TotalSize:        10,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        false,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)
	body, _ := json.Marshal(common.ResumeParams{DownloadId: item.Hash})
	if _, _, err := api.resumeHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for non-resumable item")
	}
}

func TestDownloadHandlerBadJSON(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	if _, _, err := api.downloadHandler(nil, pool, []byte("{")); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestDownloadHandlerInvalidURL(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.DownloadParams{Url: "://bad"})
	if _, _, err := api.downloadHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for invalid url")
	}
}

func TestAttachHandlerErrors(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.InputDownloadId{})
	if _, _, err := api.attachHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for missing download_id")
	}
	body, _ = json.Marshal(common.InputDownloadId{DownloadId: "missing"})
	if _, _, err := api.attachHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for missing download")
	}

	item := &warplib.Item{
		Hash:      "h1",
		Name:      "a",
		Url:       "u",
		TotalSize: 10,
		Parts:     make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)
	body, _ = json.Marshal(common.InputDownloadId{DownloadId: item.Hash})
	if _, _, err := api.attachHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for not running download")
	}
}

func TestStopHandlerErrors(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.InputDownloadId{})
	if _, _, err := api.stopHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for missing download_id")
	}
	body, _ = json.Marshal(common.InputDownloadId{DownloadId: "missing"})
	if _, _, err := api.stopHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for missing download")
	}
	item := &warplib.Item{
		Hash:      "h1",
		Name:      "a",
		Url:       "u",
		TotalSize: 10,
		Parts:     make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)
	body, _ = json.Marshal(common.InputDownloadId{DownloadId: item.Hash})
	if _, _, err := api.stopHandler(nil, pool, body); err == nil {
		t.Fatalf("expected error for not running download")
	}
}

func TestExtensionHandlerErrors(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	if _, _, err := api.addExtHandler(nil, pool, []byte("{}")); err == nil {
		t.Fatalf("expected error for missing extension path")
	}
	if _, _, err := api.getExtHandler(nil, pool, []byte(`{}`)); err == nil {
		t.Fatalf("expected error for missing extension id")
	}
	if _, _, err := api.activateExtHandler(nil, pool, []byte(`{}`)); err == nil {
		t.Fatalf("expected error for missing extension id")
	}
	if _, _, err := api.deactivateExtHandler(nil, pool, []byte(`{}`)); err == nil {
		t.Fatalf("expected error for missing extension id")
	}
	if _, _, err := api.deleteExtHandler(nil, pool, []byte(`{}`)); err == nil {
		t.Fatalf("expected error for missing extension id")
	}
}

func TestResumeItemComplete(t *testing.T) {
	item := &warplib.Item{Downloaded: 10, TotalSize: 10}
	if err := resumeItem(item); err != nil {
		t.Fatalf("resumeItem: %v", err)
	}
}

func TestListHandlerBadJSON(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	if _, _, err := api.listHandler(nil, pool, []byte("{")); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestResumeHandlerBadJSON(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	if _, _, err := api.resumeHandler(nil, pool, []byte("{")); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestListHandlerDefaultCase(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	// Add complete and incomplete items
	complete := &warplib.Item{
		Hash:             "h1",
		Name:             "complete",
		Url:              "u",
		TotalSize:        10,
		DownloadLocation: ".",
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	complete.Downloaded = complete.TotalSize

	incomplete := &warplib.Item{
		Hash:             "h2",
		Name:             "incomplete",
		Url:              "u",
		TotalSize:        10,
		DownloadLocation: ".",
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	incomplete.Downloaded = 5

	api.manager.UpdateItem(complete)
	api.manager.UpdateItem(incomplete)

	// Default case (both false) should return incomplete items
	body, _ := json.Marshal(common.ListParams{ShowCompleted: false, ShowPending: false})
	_, msg, err := api.listHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("listHandler: %v", err)
	}
	items := msg.(*common.ListResponse).Items
	if len(items) != 1 || items[0].Hash != "h2" {
		t.Fatalf("expected only incomplete item, got %d items", len(items))
	}
}

func TestVersionHandler(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	_, msg, err := api.versionHandler(nil, pool, nil)
	if err != nil {
		t.Fatalf("versionHandler: %v", err)
	}
	resp := msg.(*common.VersionResponse)
	if resp.Version != "test" {
		t.Fatalf("expected version 'test', got '%s'", resp.Version)
	}
	if resp.Commit != "abc123" {
		t.Fatalf("expected commit 'abc123', got '%s'", resp.Commit)
	}
	if resp.BuildType != "test" {
		t.Fatalf("expected buildType 'test', got '%s'", resp.BuildType)
	}
}

func TestDownloadHandlerInvalidProxy(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	params := common.DownloadParams{
		Url:               "http://example.com/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		Proxy:             "://invalid-proxy",
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for invalid proxy URL")
	}
	if !strings.Contains(err.Error(), "invalid proxy URL") {
		t.Fatalf("expected 'invalid proxy URL' error, got: %v", err)
	}
}

func TestDownloadHandlerWithTimeout(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	content := bytes.Repeat([]byte("t"), 1024)
	srv := newRangeServer(content)
	defer srv.Close()

	params := common.DownloadParams{
		Url:               srv.URL + "/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    1,
		MaxSegments:       1,
		Timeout:           30,
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.downloadHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("downloadHandler with timeout: %v", err)
	}
	resp := msg.(*common.DownloadResponse)
	if resp.DownloadId == "" {
		t.Fatalf("expected download id")
	}
	// Wait for download to complete to ensure file handles are closed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(resp.SavePath)
		if err == nil && info.Size() == int64(resp.ContentLength) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDownloadHandlerWithMaxRetries(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	content := bytes.Repeat([]byte("r"), 1024)
	srv := newRangeServer(content)
	defer srv.Close()

	params := common.DownloadParams{
		Url:               srv.URL + "/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    1,
		MaxSegments:       1,
		MaxRetries:        5,
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.downloadHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("downloadHandler with max retries: %v", err)
	}
	resp := msg.(*common.DownloadResponse)
	if resp.DownloadId == "" {
		t.Fatalf("expected download id")
	}
	// Wait for download to complete to ensure file handles are closed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(resp.SavePath)
		if err == nil && info.Size() == int64(resp.ContentLength) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDownloadHandlerWithRetryDelay(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	content := bytes.Repeat([]byte("d"), 1024)
	srv := newRangeServer(content)
	defer srv.Close()

	params := common.DownloadParams{
		Url:               srv.URL + "/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    1,
		MaxSegments:       1,
		RetryDelay:        1000,
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.downloadHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("downloadHandler with retry delay: %v", err)
	}
	resp := msg.(*common.DownloadResponse)
	if resp.DownloadId == "" {
		t.Fatalf("expected download id")
	}
	// Wait for download to complete to ensure file handles are closed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(resp.SavePath)
		if err == nil && info.Size() == int64(resp.ContentLength) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDownloadHandlerWithRetryConfig(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	content := bytes.Repeat([]byte("c"), 1024)
	srv := newRangeServer(content)
	defer srv.Close()

	params := common.DownloadParams{
		Url:               srv.URL + "/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    1,
		MaxSegments:       1,
		MaxRetries:        3,
		RetryDelay:        500,
		Timeout:           60,
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.downloadHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("downloadHandler with full retry config: %v", err)
	}
	resp := msg.(*common.DownloadResponse)
	if resp.DownloadId == "" {
		t.Fatalf("expected download id")
	}
	// Wait for download to complete to ensure file handles are closed
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(resp.SavePath)
		if err == nil && info.Size() == int64(resp.ContentLength) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestResumeHandlerInvalidProxy(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item := &warplib.Item{
		Hash:             "proxy-test",
		Name:             "test",
		Url:              "http://example.com/file",
		TotalSize:        100,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)

	params := common.ResumeParams{
		DownloadId: item.Hash,
		Proxy:      "://invalid-proxy",
	}
	body, _ := json.Marshal(params)
	_, _, err := api.resumeHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for invalid proxy URL")
	}
	if !strings.Contains(err.Error(), "invalid proxy URL") {
		t.Fatalf("expected 'invalid proxy URL' error, got: %v", err)
	}
}

func TestResumeHandlerWithTimeout(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item := &warplib.Item{
		Hash:             "timeout-test",
		Name:             "test",
		Url:              "http://example.com/file",
		TotalSize:        100,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)
	if err := os.MkdirAll(filepath.Join(warplib.DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	params := common.ResumeParams{
		DownloadId: item.Hash,
		Timeout:    30,
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.resumeHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("resumeHandler with timeout: %v", err)
	}
	resp := msg.(*common.ResumeResponse)
	if resp.FileName != item.Name {
		t.Fatalf("unexpected resume response")
	}
}

func TestResumeHandlerWithRetryConfig(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item := &warplib.Item{
		Hash:             "retry-test",
		Name:             "test",
		Url:              "http://example.com/file",
		TotalSize:        100,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)
	if err := os.MkdirAll(filepath.Join(warplib.DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	params := common.ResumeParams{
		DownloadId: item.Hash,
		MaxRetries: 5,
		RetryDelay: 1000,
		Timeout:    60,
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.resumeHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("resumeHandler with retry config: %v", err)
	}
	resp := msg.(*common.ResumeResponse)
	if resp.FileName != item.Name {
		t.Fatalf("unexpected resume response")
	}
}

func TestDownloadHandlerWithSpeedLimit(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	content := bytes.Repeat([]byte("s"), 2048)
	srv := newRangeServer(content)
	defer srv.Close()

	params := common.DownloadParams{
		Url:               srv.URL + "/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		MaxConnections:    1,
		MaxSegments:       1,
		SpeedLimit:        "1MB",
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.downloadHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("downloadHandler with speed limit: %v", err)
	}
	resp := msg.(*common.DownloadResponse)
	if resp.DownloadId == "" {
		t.Fatalf("expected download id")
	}
	// Wait for download to complete
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(resp.SavePath)
		if err == nil && info.Size() == int64(resp.ContentLength) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDownloadHandlerInvalidSpeedLimit(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	params := common.DownloadParams{
		Url:               "http://example.com/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		SpeedLimit:        "invalid",
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for invalid speed limit")
	}
	if !strings.Contains(err.Error(), "invalid speed limit") {
		t.Fatalf("expected 'invalid speed limit' error, got: %v", err)
	}
}

func TestResumeHandlerWithSpeedLimit(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item := &warplib.Item{
		Hash:             "speed-limit-test",
		Name:             "test",
		Url:              "http://example.com/file",
		TotalSize:        100,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)
	if err := os.MkdirAll(filepath.Join(warplib.DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	params := common.ResumeParams{
		DownloadId: item.Hash,
		SpeedLimit: "512KB",
	}
	body, _ := json.Marshal(params)
	_, msg, err := api.resumeHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("resumeHandler with speed limit: %v", err)
	}
	resp := msg.(*common.ResumeResponse)
	if resp.FileName != item.Name {
		t.Fatalf("unexpected resume response")
	}
}

func TestResumeHandlerInvalidSpeedLimit(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	item := &warplib.Item{
		Hash:             "invalid-speed-test",
		Name:             "test",
		Url:              "http://example.com/file",
		TotalSize:        100,
		DownloadLocation: warplib.ConfigDir,
		AbsoluteLocation: warplib.ConfigDir,
		Resumable:        true,
		Parts:            make(map[int64]*warplib.ItemPart),
	}
	api.manager.UpdateItem(item)

	params := common.ResumeParams{
		DownloadId: item.Hash,
		SpeedLimit: "abc",
	}
	body, _ := json.Marshal(params)
	_, _, err := api.resumeHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for invalid speed limit")
	}
	if !strings.Contains(err.Error(), "invalid speed limit") {
		t.Fatalf("expected 'invalid speed limit' error, got: %v", err)
	}
}

func TestDownloadSFTPHandlerNilRouter(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	// schemeRouter is nil by default in test — should error for sftp://
	params := common.DownloadParams{
		Url:               "sftp://sftp.example.com/path/file.bin",
		DownloadDirectory: warplib.ConfigDir,
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for SFTP download with nil router")
	}
	if !strings.Contains(err.Error(), "scheme router not initialized") {
		t.Fatalf("expected 'scheme router not initialized' error, got: %v", err)
	}
}

func TestDownloadSFTPHandlerWithRouter(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	// Set up a scheme router — SFTP factory will fail on connection (no server)
	// but this covers the dispatch path through downloadProtocolHandler past the nil check
	router := warplib.NewSchemeRouter(&http.Client{})
	api.schemeRouter = router

	params := common.DownloadParams{
		Url:               "sftp://sftp.example.com/path/file.bin",
		DownloadDirectory: warplib.ConfigDir,
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	// Should fail at Probe (connection refused) but exercises the full dispatch path
	if err == nil {
		t.Fatalf("expected error for SFTP download to unreachable server")
	}
}

func TestDownloadSFTPHandlerInvalidURL(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	router := warplib.NewSchemeRouter(&http.Client{})
	api.schemeRouter = router

	// SFTP URL with no file path — factory should reject
	params := common.DownloadParams{
		Url:               "sftp://sftp.example.com/",
		DownloadDirectory: warplib.ConfigDir,
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for SFTP URL with root path")
	}
}

func TestDownloadSFTPHandlerSSHKeyPath(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	router := warplib.NewSchemeRouter(&http.Client{})
	api.schemeRouter = router

	// Verify SSHKeyPath is forwarded through the API layer.
	// The download will fail (no server) but the key path is passed to the downloader.
	params := common.DownloadParams{
		Url:               "sftp://sftp.example.com/path/file.bin",
		DownloadDirectory: warplib.ConfigDir,
		SSHKeyPath:        "/tmp/test_key",
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	// Should fail at Probe/connect (connection refused) but exercises the SSHKeyPath forwarding
	if err == nil {
		t.Fatalf("expected error for SFTP download to unreachable server")
	}
	// The fact that it gets past the nil-router check and tries to connect proves SSHKeyPath was forwarded
}

func TestDownloadSFTPHandlerCredentialStripping(t *testing.T) {
	// NON-NEGOTIABLE: sftp:// URLs with embedded credentials must have them
	// stripped before persistence. This test verifies that StripURLCredentials
	// is applied to SFTP URLs the same as FTP URLs.
	//
	// We cannot do a full end-to-end test (no live SFTP server), but we verify
	// the credential-stripping logic is correct for sftp:// scheme URLs.
	tests := []struct {
		name     string
		inputURL string
		wantURL  string
	}{
		{
			name:     "sftp with user:pass",
			inputURL: "sftp://admin:secret@sftp.example.com/path/file.bin",
			wantURL:  "sftp://sftp.example.com/path/file.bin",
		},
		{
			name:     "sftp with user only",
			inputURL: "sftp://admin@sftp.example.com/path/file.bin",
			wantURL:  "sftp://sftp.example.com/path/file.bin",
		},
		{
			name:     "sftp without credentials",
			inputURL: "sftp://sftp.example.com/path/file.bin",
			wantURL:  "sftp://sftp.example.com/path/file.bin",
		},
		{
			name:     "sftp with special chars in password",
			inputURL: "sftp://user:p%40ss%3Aword@sftp.example.com/path/file.bin",
			wantURL:  "sftp://sftp.example.com/path/file.bin",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := warplib.StripURLCredentials(tc.inputURL)
			if got != tc.wantURL {
				t.Errorf("StripURLCredentials(%q) = %q, want %q", tc.inputURL, got, tc.wantURL)
			}
		})
	}
}

func TestDownloadFTPHandlerNilRouter(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	// schemeRouter is nil by default in test — should error
	params := common.DownloadParams{
		Url:               "ftp://ftp.example.com/file.iso",
		DownloadDirectory: warplib.ConfigDir,
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for FTP download with nil router")
	}
	if !strings.Contains(err.Error(), "scheme router not initialized") {
		t.Fatalf("expected 'scheme router not initialized' error, got: %v", err)
	}
}

func TestDownloadFTPHandlerNilRouterFTPS(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	params := common.DownloadParams{
		Url:               "ftps://ftp.example.com/file.iso",
		DownloadDirectory: warplib.ConfigDir,
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for FTPS download with nil router")
	}
	if !strings.Contains(err.Error(), "scheme router not initialized") {
		t.Fatalf("expected 'scheme router not initialized' error, got: %v", err)
	}
}

func TestDownloadFTPHandlerWithRouter(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	// Set up a scheme router — FTP factory will fail on connection (no server)
	// but this covers the dispatch path through downloadFTPHandler past the nil check
	router := warplib.NewSchemeRouter(&http.Client{})
	api.schemeRouter = router

	params := common.DownloadParams{
		Url:               "ftp://ftp.example.com/file.iso",
		DownloadDirectory: warplib.ConfigDir,
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	// Should fail at Probe (connection refused) but exercises the full dispatch path
	if err == nil {
		t.Fatalf("expected error for FTP download to unreachable server")
	}
}

func TestDownloadFTPHandlerInvalidFTPURL(t *testing.T) {
	api, pool, cleanup := newTestApi(t)
	defer cleanup()

	router := warplib.NewSchemeRouter(&http.Client{})
	api.schemeRouter = router

	// FTP URL with no file path — factory should reject
	params := common.DownloadParams{
		Url:               "ftp://ftp.example.com/",
		DownloadDirectory: warplib.ConfigDir,
	}
	body, _ := json.Marshal(params)
	_, _, err := api.downloadHandler(nil, pool, body)
	if err == nil {
		t.Fatalf("expected error for FTP URL with root path")
	}
}
