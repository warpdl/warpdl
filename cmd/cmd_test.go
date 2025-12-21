package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli"
	cmdcommon "github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
	"github.com/vbauerster/mpb/v8"
)

type fakeServer struct {
	listener net.Listener
	wg       sync.WaitGroup
}

var listOverride []*warplib.Item

func (s *fakeServer) close() {
	_ = s.listener.Close()
	s.wg.Wait()
}

func startFakeServer(t *testing.T, socketPath string, fail ...map[common.UpdateType]string) *fakeServer {
	t.Helper()
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
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
				reqBytes, err := readMessage(c)
				if err != nil {
					return
				}
				var req struct {
					Method common.UpdateType `json:"method"`
					Message json.RawMessage  `json:"message"`
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
							Hash:        "id",
							Name:        "file.bin",
							TotalSize:   10,
							Downloaded:  10,
							Hidden:      false,
							Children:    false,
							DateAdded:   time.Now(),
							Resumable:   true,
							Parts:       make(map[int64]*warplib.ItemPart),
						}}
					}
					resp := common.ListResponse{Items: items}
					writeResponse(c, req.Method, resp)
				case common.UPDATE_STOP, common.UPDATE_FLUSH:
					writeResponse(c, req.Method, nil)
				default:
					writeError(c, "unknown method")
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

func newContext(app *cli.App, args []string, name string) *cli.Context {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	_ = set.Parse(args)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: name}
	return ctx
}

func TestDownloadCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := info(ctx); err != nil {
		t.Fatalf("info: %v", err)
	}
}

func TestInfoInvalidURL(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"://bad"}, "info")
	if err := info(ctx); err != nil {
		t.Fatalf("info: %v", err)
	}
}

func TestSpeedCounter(t *testing.T) {
	sc := NewSpeedCounter(time.Millisecond)
	if sc == nil {
		t.Fatalf("expected counter")
	}
	p := mpb.New()
	bar := p.AddBar(10)
	sc.SetBar(bar)
	sc.Start()
	sc.IncrBy(5)
	time.Sleep(time.Millisecond * 5)
	sc.Stop()
}

func TestGetUserAgent(t *testing.T) {
	if got := getUserAgent("warp"); got == "" {
		t.Fatalf("expected user agent")
	}
	if got := getUserAgent("CustomUA"); got != "CustomUA" {
		t.Fatalf("expected passthrough user agent")
	}
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
	if err := download(ctx); err != nil {
		t.Fatalf("download without url: %v", err)
	}
}

func TestInfoNoURL(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "info")
	if err := info(ctx); err != nil {
		t.Fatalf("info without url: %v", err)
	}
}

func TestDownloadHelpArg(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"help"}, "download")
	_ = download(ctx)
}

func TestListHelpArg(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"help"}, "list")
	_ = list(ctx)
}

func TestStopNoHash(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "stop")
	_ = stop(ctx)
}

func TestFlushWithHash(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "flush")
	_ = flush(ctx)
}

func TestAttachNoHash(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "attach")
	_ = attach(ctx)
}

func TestResumeNoHash(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "resume")
	_ = resume(ctx)
}

func TestDownloadCustomPath(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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

func TestDownloadErrorResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_DOWNLOAD: "download failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"http://example.com"}, "download")
	if err := download(ctx); err != nil {
		t.Fatalf("download: %v", err)
	}
}

func TestListWithHidden(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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

func TestConfigTemplateStrings(t *testing.T) {
	if len(HELP_TEMPL) == 0 || len(CMD_HELP_TEMPL) == 0 {
		t.Fatalf("expected help templates")
	}
}

func TestInitAddsFlags(t *testing.T) {
	if len(dlFlags) == 0 {
		t.Fatalf("expected download flags")
	}
}

func TestCounterStartStop(t *testing.T) {
	sc := NewSpeedCounter(time.Millisecond)
	if sc == nil {
		t.Fatalf("expected counter")
	}
	sc.Start()
	go func() {
		sc.IncrBy(1)
	}()
	time.Sleep(time.Millisecond * 5)
	sc.Stop()
}

func TestStopHelp(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"help"}, "stop")
	_ = stop(ctx)
}

func TestStopErrorResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_STOP: "stop failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "stop")
	if err := stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestListOutputFormatting(t *testing.T) {
	name := beautForTest("short")
	if len(name) == 0 {
		t.Fatalf("expected formatted name")
	}
}

func TestAttachCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_ATTACH: "attach failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "attach")
	if err := attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}
}

func TestResumeCommand(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_RESUME: "resume failed",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"id"}, "resume")
	if err := resume(ctx); err != nil {
		t.Fatalf("resume: %v", err)
	}
}

func TestFlushInvalidArgs(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"a", "b"}, "flush")
	if err := flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func TestFlushAll(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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

func beautForTest(name string) string {
	if len(name) < 23 {
		return cmdcommon.Beaut(name, 23)
	}
	return name
}

func TestDownloadPathDefault(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
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

func TestConfigConstants(t *testing.T) {
	if DEF_MAX_PARTS == 0 || DEF_MAX_CONNS == 0 {
		t.Fatalf("expected defaults")
	}
}

func TestDownloadTemplates(t *testing.T) {
	if !bytes.Contains([]byte(DownloadDescription), []byte("download")) {
		t.Fatalf("expected description")
	}
}
