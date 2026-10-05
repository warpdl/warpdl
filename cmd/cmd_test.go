package cmd

import (
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli"
	cmdcommon "github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// suppressVersionCheck sets the environment variable to suppress version
// mismatch warnings during tests. This prevents test pollution from tests
// that set currentBuildArgs.Version via Execute().
func suppressVersionCheck(t *testing.T) {
	t.Helper()
	t.Setenv(warpcli.VersionCheckEnv, "1")
}

type fakeServer struct {
	listener net.Listener
	wg       sync.WaitGroup
}

var listOverride []*warplib.Item
var queueStatusOverride *common.QueueStatusResponse

// authListOverride lets auth tests script the AuthList response the fake
// daemon returns for UPDATE_AUTH_LIST. nil means "empty store".
var authListOverride *common.AuthListResult

// authLoginOverride lets auth tests script the AuthLogin response the
// fake daemon returns for UPDATE_AUTH_REQUIRED. nil means an empty
// (zero-value) result.
var authLoginOverride *common.AuthLoginResult

func (s *fakeServer) close() {
	_ = s.listener.Close()
	s.wg.Wait()
}

func startFakeServer(t *testing.T, socketPath string, fail ...map[common.UpdateType]string) *fakeServer {
	t.Helper()
	suppressVersionCheck(t) // Prevent test pollution from currentBuildArgs

	// Use platform-specific listener (Unix socket on Unix, TCP on Windows)
	listener, err := createTestListener(t, socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &fakeServer{listener: listener}
	var failMap map[common.UpdateType]string
	if len(fail) > 0 {
		failMap = fail[0]
	}
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			srv.wg.Add(1)
			go func(c net.Conn) {
				defer srv.wg.Done()
				defer c.Close()
				for {
					reqBytes, err := readMessage(c)
					if err != nil {
						return
					}
					var req struct {
						Method  common.UpdateType `json:"method"`
						Message json.RawMessage   `json:"message"`
					}
					if err := json.Unmarshal(reqBytes, &req); err != nil {
						return
					}
					if failMap != nil {
						if msg, ok := failMap[req.Method]; ok {
							writeError(c, msg)
							return
						}
					}
					switch req.Method {
					case common.UPDATE_VERSION:
						resp := common.VersionResponse{
							Version:   "1-test",
							Commit:    "test",
							BuildType: "test",
						}
						writeResponse(c, req.Method, resp)
						continue // Keep loop for next request (download/attach/etc.)
					case common.UPDATE_DOWNLOAD:
						resp := common.DownloadResponse{
							DownloadId:        "id",
							FileName:          "file.bin",
							SavePath:          "file.bin",
							DownloadDirectory: ".",
							ContentLength:     warplib.ContentLength(10),
							MaxConnections:    1,
							MaxSegments:       1,
						}
						writeResponse(c, req.Method, resp)
						update := common.DownloadingResponse{
							DownloadId: "id",
							Action:     common.DownloadComplete,
							Hash:       warplib.MAIN_HASH,
							Value:      10,
						}
						writeResponse(c, common.UPDATE_DOWNLOADING, update)
					case common.UPDATE_ATTACH:
						resp := common.DownloadResponse{
							DownloadId:        "id",
							FileName:          "file.bin",
							SavePath:          "file.bin",
							DownloadDirectory: ".",
							ContentLength:     warplib.ContentLength(10),
							MaxConnections:    1,
							MaxSegments:       1,
						}
						writeResponse(c, req.Method, resp)
						update := common.DownloadingResponse{
							DownloadId: "id",
							Action:     common.DownloadComplete,
							Hash:       warplib.MAIN_HASH,
							Value:      10,
						}
						writeResponse(c, common.UPDATE_DOWNLOADING, update)
					case common.UPDATE_RESUME:
						resp := common.ResumeResponse{
							FileName:          "file.bin",
							SavePath:          "file.bin",
							DownloadDirectory: ".",
							AbsoluteLocation:  ".",
							ContentLength:     warplib.ContentLength(10),
							MaxConnections:    1,
							MaxSegments:       1,
						}
						writeResponse(c, req.Method, resp)
						update := common.DownloadingResponse{
							DownloadId: "id",
							Action:     common.DownloadComplete,
							Hash:       warplib.MAIN_HASH,
							Value:      10,
						}
						writeResponse(c, common.UPDATE_DOWNLOADING, update)
					case common.UPDATE_LIST:
						items := listOverride
						if items == nil {
							items = []*warplib.Item{{
								Hash:       "id",
								Name:       "file.bin",
								TotalSize:  10,
								Downloaded: 10,
								Hidden:     false,
								Children:   false,
								DateAdded:  time.Now(),
								Resumable:  true,
								Parts:      nil,
							}}
						}
						resp := common.ListResponse{Items: items}
						writeResponse(c, req.Method, resp)
						return // One-shot command, exit loop
					case common.UPDATE_QUEUE_STATUS:
						var qsResp common.QueueStatusResponse
						if queueStatusOverride != nil {
							qsResp = *queueStatusOverride
						}
						writeResponse(c, req.Method, qsResp)
						return
					case common.UPDATE_QUEUE_PAUSE, common.UPDATE_QUEUE_RESUME, common.UPDATE_QUEUE_MOVE:
						writeResponse(c, req.Method, nil)
						return
					case common.UPDATE_STOP, common.UPDATE_FLUSH:
						writeResponse(c, req.Method, nil)
						return // One-shot command, exit loop
					case common.UPDATE_AUTH_LIST:
						var resp common.AuthListResult
						if authListOverride != nil {
							resp = *authListOverride
						}
						writeResponse(c, req.Method, resp)
						return // One-shot RPC, exit loop
					case common.UPDATE_AUTH_LOGGED_OUT:
						// AuthLogout: daemon returns an empty payload on success.
						writeResponse(c, req.Method, nil)
						return
					case common.UPDATE_AUTH_REQUIRED:
						// AuthLogin: scripted login result (PKCE/device start).
						var resp common.AuthLoginResult
						if authLoginOverride != nil {
							resp = *authLoginOverride
						}
						writeResponse(c, req.Method, resp)
						return
					case common.UPDATE_AUTH_FAILED:
						// AuthCancel: empty payload on success.
						writeResponse(c, req.Method, nil)
						return
					default:
						writeError(c, "unknown method")
						return // Exit loop on unknown method
					}
				}
			}(conn)
		}
	}()
	return srv
}

func readMessage(conn net.Conn) ([]byte, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return nil, err
	}
	length := int(head[0]) | int(head[1])<<8 | int(head[2])<<16 | int(head[3])<<24
	buf := make([]byte, length)
	_, err := io.ReadFull(conn, buf)
	return buf, err
}

func writeMessage(conn net.Conn, b []byte) error {
	head := []byte{byte(len(b)), byte(len(b) >> 8), byte(len(b) >> 16), byte(len(b) >> 24)}
	if _, err := conn.Write(head); err != nil {
		return err
	}
	_, err := conn.Write(b)
	return err
}

func writeResponse(conn net.Conn, typ common.UpdateType, msg any) {
	payload, _ := json.Marshal(msg)
	resp, _ := json.Marshal(map[string]any{
		"ok": true,
		"update": map[string]any{
			"type":    typ,
			"message": json.RawMessage(payload),
		},
	})
	_ = writeMessage(conn, resp)
}

func writeError(conn net.Conn, errMsg string) {
	resp, _ := json.Marshal(map[string]any{
		"ok":    false,
		"error": errMsg,
	})
	_ = writeMessage(conn, resp)
}

func TestDownloadCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"http://example.com"}, "download")
	oldDlPath, oldFileName := dlPath, fileName
	dlPath = ""
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()
	if err := download(ctx); err != nil {
		t.Fatalf("download: %v", err)
	}
}

func TestListCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "list")
	if err := list(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestListEmpty(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	listOverride = []*warplib.Item{}
	defer func() { listOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "list")
	if err := list(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestListHiddenOnly(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	listOverride = []*warplib.Item{{
		Hash:       "id",
		Name:       "secret.bin",
		TotalSize:  10,
		Downloaded: 10,
		Hidden:     true,
		Children:   false,
		DateAdded:  time.Now(),
		Resumable:  true,
		Parts:      make(map[int64]*warplib.ItemPart),
	}}
	defer func() { listOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "list")
	if err := list(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestListLongName(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	listOverride = []*warplib.Item{{
		Hash:       "id",
		Name:       strings.Repeat("x", 30),
		TotalSize:  10,
		Downloaded: 10,
		Hidden:     false,
		Children:   false,
		DateAdded:  time.Now(),
		Resumable:  true,
		Parts:      make(map[int64]*warplib.ItemPart),
	}}
	defer func() { listOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "list")
	if err := list(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestStopFlushCommands(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "stop")
	if err := stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	ctx = newContext(app, nil, "flush")
	if err := flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func TestInfoCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	app := cli.NewApp()
	ctx := newContext(app, []string{srv.URL + "/file.bin"}, "info")
	oldUA := userAgent
	userAgent = "warp"
	defer func() { userAgent = oldUA }()
	if err := info(ctx); err != nil {
		t.Fatalf("info: %v", err)
	}
}

func TestInfoCommandClosesRetainedResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validator-less metadata responses are retained by NewDownloader so
		// Start can consume that exact representation. The info command never
		// starts a transfer, so it must close the downloader before returning.
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))

	app := cli.NewApp()
	ctx := newContext(app, []string{srv.URL + "/file.bin"}, "info")
	oldUA, oldProxy := userAgent, proxyURL
	userAgent, proxyURL = "warp", ""
	defer func() {
		userAgent, proxyURL = oldUA, oldProxy
	}()
	if err := info(ctx); err != nil {
		srv.CloseClientConnections()
		srv.Close()
		t.Fatalf("info: %v", err)
	}

	closed := make(chan struct{})
	go func() {
		srv.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		srv.CloseClientConnections()
		<-closed
		t.Fatal("info left its retained HTTP response body open")
	}
}

func TestInfoNoFileName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	app := cli.NewApp()
	ctx := newContext(app, []string{srv.URL}, "info")
	oldUA := userAgent
	userAgent = "warp"
	defer func() { userAgent = oldUA }()
	assertExitError(t, info(ctx))
}

func TestInfoInvalidURL(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"://bad"}, "info")
	assertExitError(t, info(ctx))
}

// TestGetUserAgent covers the UserAgents alias contract: known aliases expand
// to a browser UA, alias lookup is case-insensitive, and unknown values pass
// through unchanged so custom UAs still work.
func TestGetUserAgent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string // exact match when set
		contains string // substring match when want is empty
	}{
		{name: "warp alias is default UA", input: "warp", want: warplib.DEF_USER_AGENT},
		{name: "firefox alias", input: "firefox", contains: "Firefox/"},
		{name: "chrome alias", input: "chrome", contains: "Chrome/"},
		{name: "edge alias", input: "edge", contains: "Edg/"},
		{name: "alias case insensitive", input: "FIREFOX", contains: "Firefox/"},
		{name: "alias mixed case", input: "Chrome", contains: "Chrome/"},
		{name: "unknown passthrough", input: "MyCustomUA/1.0", want: "MyCustomUA/1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getUserAgent(tt.input)
			if tt.want != "" && got != tt.want {
				t.Fatalf("getUserAgent(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if tt.contains != "" && !strings.Contains(got, tt.contains) {
				t.Fatalf("getUserAgent(%q) = %q, want substring %q", tt.input, got, tt.contains)
			}
		})
	}
}

// assertEmptyCommandNameShowsAppHelp covers the shared empty-command-name
// branch: commands fall back to application help with exit code 0 instead of
// reporting a command-level usage error.
func assertEmptyCommandNameShowsAppHelp(t *testing.T, action func(*cli.Context) error) {
	t.Helper()
	app := GetApp(BuildArgs{Version: "1.0.0", BuildType: "test"})
	ctx := newContext(app, nil, "")

	exitCode := -1
	called := false
	prev := cmdcommon.SetShowAppHelpAndExit(func(_ *cli.Context, code int) {
		called = true
		exitCode = code
	})
	defer func() { cmdcommon.SetShowAppHelpAndExit(prev) }()

	stdout, _ := captureOutput(func() {
		if err := action(ctx); err != nil {
			t.Errorf("empty command name: %v", err)
		}
	})

	if !called {
		t.Fatal("expected application help to be shown")
	}
	if exitCode != 0 {
		t.Fatalf("expected app help exit code 0, got %d", exitCode)
	}
	assertContains(t, stdout, "warpdl")
}

// assertCommandHelpArg covers the in-command "help" sentinel: the argument
// renders the command's help through the real CLI templates instead of being
// treated as a URL/hash and executed.
func assertCommandHelpArg(t *testing.T, name string, action func(*cli.Context) error) {
	t.Helper()
	app := GetApp(BuildArgs{Version: "1.0.0", BuildType: "test"})
	ctx := newContext(app, []string{"help"}, name)

	stdout, _ := captureOutput(func() {
		app.Writer = os.Stdout // help is rendered to the captured stdout
		if err := action(ctx); err != nil {
			t.Errorf("%s help: %v", name, err)
		}
	})

	assertContains(t, stdout, name)
}

func TestConfirmForce(t *testing.T) {
	if !confirm(command("test"), true) {
		t.Fatalf("expected confirm to return true")
	}
}

func TestExecuteVersion(t *testing.T) {
	args := []string{"warpdl", "version"}
	if err := Execute(args, BuildArgs{Version: "1", BuildType: "dev"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestDownloadNoURL(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "download")
	if err := download(ctx); err == nil {
		t.Fatalf("expected error when download called with no URL")
	}
}

func TestDownloadNoURLEmptyCommandName(t *testing.T) {
	assertEmptyCommandNameShowsAppHelp(t, download)
}

func TestInfoNoURL(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "info")
	if err := info(ctx); err == nil {
		t.Fatalf("expected error when info called with no URL")
	}
}

func TestListHelpArg(t *testing.T) {
	assertCommandHelpArg(t, "list", list)
}

func TestStopNoHashEmptyCommandName(t *testing.T) {
	assertEmptyCommandNameShowsAppHelp(t, stop)
}

func TestAttachNoHashEmptyCommandName(t *testing.T) {
	assertEmptyCommandNameShowsAppHelp(t, attach)
}

func TestAttachHelpArg(t *testing.T) {
	assertCommandHelpArg(t, "attach", attach)
}

func TestResumeNoHashEmptyCommandName(t *testing.T) {
	assertEmptyCommandNameShowsAppHelp(t, resume)
}

func TestResumeHelpArg(t *testing.T) {
	assertCommandHelpArg(t, "resume", resume)
}

func TestDownloadCustomPath(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"http://example.com"}, "download")
	oldDlPath, oldFileName := dlPath, fileName
	dlPath = t.TempDir()
	fileName = "custom.bin"
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()
	if err := download(ctx); err != nil {
		t.Fatalf("download: %v", err)
	}
}

func TestDownloadGetwdError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot delete a directory while inside it")
	}

	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if err := os.RemoveAll(tmp); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	app := cli.NewApp()
	ctx := newContext(app, []string{"http://example.com"}, "download")
	assertExitError(t, download(ctx))
}

func TestDownloadErrorResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_DOWNLOAD: "download failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"http://example.com"}, "download")
	assertExitError(t, download(ctx))
}

func TestDownloadInvalidProxyURL(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"http://example.com"}, "download")
	oldDlPath, oldFileName, oldProxy := dlPath, fileName, proxyURL
	dlPath = ""
	fileName = ""
	proxyURL = "://invalid"
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
		proxyURL = oldProxy
	}()
	assertExitError(t, download(ctx))
}

func TestResumeInvalidProxy(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "resume")
	oldProxy := proxyURL
	proxyURL = "://invalid"
	defer func() { proxyURL = oldProxy }()
	assertExitError(t, resume(ctx))
}

func TestListWithHidden(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "list")
	oldShowHidden := showHidden
	showHidden = true
	defer func() { showHidden = oldShowHidden }()
	if err := list(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}
}

// flagNames splits a cli flag declaration ("name, alias") into its names.
func flagNames(f cli.Flag) []string {
	parts := strings.Split(f.GetName(), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// TestInitAddsFlags verifies init() folds the resume and info flag sets into
// the download command, so download exposes --max-parts/--max-connection and
// --user-agent alongside its own flags.
func TestInitAddsFlags(t *testing.T) {
	names := make(map[string]bool, len(dlFlags))
	for _, f := range dlFlags {
		for _, name := range flagNames(f) {
			names[name] = true
		}
	}
	for _, want := range []string{"max-parts", "max-connection", "user-agent"} {
		if !names[want] {
			t.Errorf("download flags missing %q", want)
		}
	}
}

func TestStopHelp(t *testing.T) {
	assertCommandHelpArg(t, "stop", stop)
}

func TestStopErrorResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_STOP: "stop failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "stop")
	assertExitError(t, stop(ctx))
}

func TestAttachCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "attach")
	if err := attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}
}

func TestAttachErrorResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_ATTACH: "attach failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "attach")
	assertExitError(t, attach(ctx))
}

func TestResumeCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "resume")
	oldMaxParts, oldMaxConns, oldForce := maxParts, maxConns, forceParts
	maxParts, maxConns, forceParts = 1, 1, false
	defer func() {
		maxParts, maxConns, forceParts = oldMaxParts, oldMaxConns, oldForce
	}()
	if err := resume(ctx); err != nil {
		t.Fatalf("resume: %v", err)
	}
}

func TestResumeErrorResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_RESUME: "resume failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "resume")
	assertExitError(t, resume(ctx))
}

func TestFlushCancelled(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "flush")
	oldForce := forceFlush
	forceFlush = false
	defer func() { forceFlush = oldForce }()

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("no\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	if err := flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func TestFlushErrorResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_FLUSH: "flush failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "flush")
	oldForce := forceFlush
	oldHash := hashToFlush
	forceFlush = true
	hashToFlush = ""
	defer func() {
		forceFlush = oldForce
		hashToFlush = oldHash
	}()
	assertExitError(t, flush(ctx))
}

func TestFlushAll(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "flush")
	oldForce := forceFlush
	oldHash := hashToFlush
	forceFlush = true
	hashToFlush = ""
	defer func() {
		forceFlush = oldForce
		hashToFlush = oldHash
	}()
	if err := flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func TestInfoUserAgentOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get(warplib.USER_AGENT_KEY)
		if ua == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", "2")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	app := cli.NewApp()
	ctx := newContext(app, []string{srv.URL + "/file.bin"}, "info")
	oldUA := userAgent
	userAgent = "chrome"
	defer func() { userAgent = oldUA }()
	if err := info(ctx); err != nil {
		t.Fatalf("info: %v", err)
	}
}

func TestListNoDownloads(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "list")
	showHidden = false
	if err := list(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestDownloadURLTrim(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"  http://example.com  "}, "download")
	oldDlPath, oldFileName := dlPath, fileName
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()
	if err := download(ctx); err != nil {
		t.Fatalf("download: %v", err)
	}
}

// TestConfigConstants verifies the documented DEF_* defaults are the ones
// actually wired into the resume/download flag set users see.
func TestConfigConstants(t *testing.T) {
	want := map[string]int{
		"max-parts":      DEF_MAX_PARTS,
		"max-connection": DEF_MAX_CONNS,
		"timeout":        DEF_TIMEOUT_SEC,
		"max-retries":    DEF_MAX_RETRIES,
		"retry-delay":    DEF_RETRY_DELAY,
	}
	for _, f := range rsFlags {
		intFlag, ok := f.(cli.IntFlag)
		if !ok {
			continue
		}
		for _, name := range flagNames(intFlag) {
			wantValue, ok := want[name]
			if !ok {
				continue
			}
			if intFlag.Value != wantValue {
				t.Errorf("flag %q default = %d, want %d", name, intFlag.Value, wantValue)
			}
			delete(want, name)
		}
	}
	for name := range want {
		t.Errorf("flag %q missing from rsFlags", name)
	}
}

// newContextWithFlags creates a CLI context with registered string/bool flags.
// flagArgs are key-value pairs: ["-flag", "value", "-bool-flag", "true", ...]
func newContextWithFlags(app *cli.App, flagDefs []struct {
	name string
	val  any
}, flagArgs []string, args []string, name string) *cli.Context {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	for _, fd := range flagDefs {
		switch v := fd.val.(type) {
		case string:
			set.String(fd.name, v, "")
		case bool:
			set.Bool(fd.name, v, "")
		}
	}
	_ = set.Parse(append(flagArgs, args...))
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: name}
	return ctx
}

// TestDownloadWithStartAt covers the validateStartAt path inside download().
func TestDownloadWithStartAt(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	future := time.Now().Add(2 * time.Hour)
	startAt := future.Format("2006-01-02 15:04")

	app := cli.NewApp()
	ctx := newContextWithFlags(app,
		[]struct {
			name string
			val  any
		}{
			{"start-at", ""},
		},
		[]string{"-start-at", startAt},
		[]string{"http://example.com"},
		"download",
	)

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = t.TempDir()
	fileName = "test.bin"
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	if err := download(ctx); err != nil {
		t.Fatalf("download with start-at: %v", err)
	}
}

// TestDownloadWithInvalidSchedule covers the validateSchedule error path inside download().
func TestDownloadWithInvalidSchedule(t *testing.T) {
	// Use /tmp directly with a short name to avoid Unix socket path length limit (108 chars).
	socketPath := filepath.Join(os.TempDir(), "wdl-sched-test.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContextWithFlags(app,
		[]struct {
			name string
			val  any
		}{
			{"schedule", ""},
		},
		[]string{"-schedule", "bad-cron"},
		[]string{"http://example.com"},
		"download",
	)

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = t.TempDir()
	fileName = "test.bin"
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	assertExitError(t, download(ctx))
}

// TestGetClientWithCustomURI covers the daemonURI != "" branch in getClient().
func TestGetClientWithCustomURI(t *testing.T) {
	old := daemonURI
	daemonURI = "://invalid-uri"
	defer func() { daemonURI = old }()

	_, err := getClient()
	if err == nil {
		t.Fatal("expected error for invalid URI")
	}
}

// TestWaitForBatchSubmissions_AlreadyComplete covers waitForBatchSubmissions
// when the daemon reports an authoritative completed manager state.
func TestWaitForBatchSubmissions_AlreadyComplete(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	f, err := os.CreateTemp("", "batch-complete-*.bin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(f.Name())
	data := []byte("hello")
	if _, err := f.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	f.Close()

	result := NewBatchResult(1)
	result.AddSuccess()
	result.Submissions = []BatchSubmission{
		{
			URL:           "http://example.com/file.bin",
			DownloadID:    "test-id",
			SavePath:      f.Name(),
			ContentLength: int64(len(data)),
		},
	}

	waitForBatchSubmissions(result)

	// Submission should remain a success (no error).
	if result.Failed != 0 {
		t.Errorf("expected 0 failures, got %d", result.Failed)
	}
}

func TestConfirm_YesInput(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("yes\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	if !confirm(command("test")) {
		t.Fatal("expected confirm to return true for 'yes'")
	}
}

func TestConfirm_YInput(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("y\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	if !confirm(command("test")) {
		t.Fatal("expected confirm to return true for 'y'")
	}
}

func TestConfirm_TrueInput(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("true\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	if !confirm(command("test")) {
		t.Fatal("expected confirm to return true for 'true'")
	}
}

func TestConfirm_OneInput(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("1\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	if !confirm(command("test")) {
		t.Fatal("expected confirm to return true for '1'")
	}
}

func TestConfirm_NoInput(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("no\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	if confirm(command("test")) {
		t.Fatal("expected confirm to return false for 'no'")
	}
}

func TestConfirm_InvalidInput(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	_, _ = w.Write([]byte("maybe\n"))
	_ = w.Close()
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	if confirm(command("test")) {
		t.Fatal("expected confirm to return false for invalid input")
	}
}

func TestCommandAction(t *testing.T) {
	c := command("test")
	action := c.action()
	if action != "test command" {
		t.Fatalf("expected 'test command', got: %s", action)
	}
}

func TestResumeWithUserAgent(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "resume")
	oldMaxParts, oldMaxConns, oldForce, oldUA := maxParts, maxConns, forceParts, userAgent
	maxParts, maxConns, forceParts = 1, 1, false
	userAgent = "firefox"
	defer func() {
		maxParts, maxConns, forceParts = oldMaxParts, oldMaxConns, oldForce
		userAgent = oldUA
	}()
	if err := resume(ctx); err != nil {
		t.Fatalf("resume: %v", err)
	}
}

func TestInfoHelp(t *testing.T) {
	assertCommandHelpArg(t, "info", info)
}

func TestListErrorResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_LIST: "list failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, nil, "list")
	assertExitError(t, list(ctx))
}

// TestDownloadInputFileFlag verifies that the download command has the -i/--input-file flag.
// This flag enables batch downloading from a file containing URLs.
func TestDownloadInputFileFlag(t *testing.T) {
	// Check dlFlags for the input-file flag
	found := false
	for _, f := range dlFlags {
		sf, ok := f.(cli.StringFlag)
		if !ok {
			continue
		}
		// Check if the flag name contains "input-file" or "i"
		if strings.Contains(sf.Name, "input-file") {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("download command missing required -i/--input-file flag in dlFlags")
	}
}
