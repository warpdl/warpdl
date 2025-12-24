package ext

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

type fakeServer struct {
	listener net.Listener
	wg       sync.WaitGroup
}

// emptyListOverride is used to return empty extension list in tests
var emptyListOverride bool

func (s *fakeServer) close() {
	_ = s.listener.Close()
	s.wg.Wait()
}

func startFakeServer(t *testing.T, fail ...map[common.UpdateType]string) *fakeServer {
	t.Helper()
	listener, socketPath, err := createTestListener(t)
	if err != nil {
		t.Fatalf("createTestListener: %v", err)
	}
	if socketPath != "" {
		t.Setenv("WARPDL_SOCKET_PATH", socketPath)
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
				case common.UPDATE_ADD_EXT, common.UPDATE_GET_EXT, common.UPDATE_ACTIVATE_EXT:
					resp := common.ExtensionInfo{ExtensionId: "ext1", Name: "Ext", Version: "1.0", Description: "desc"}
					writeResponse(c, req.Method, resp)
				case common.UPDATE_DELETE_EXT, common.UPDATE_DEACTIVATE_EXT:
					resp := common.ExtensionName{Name: "Ext"}
					writeResponse(c, req.Method, resp)
				case common.UPDATE_LIST_EXT:
					var resp []common.ExtensionInfoShort
					if emptyListOverride {
						resp = []common.ExtensionInfoShort{}
					} else {
						resp = []common.ExtensionInfoShort{{ExtensionId: "ext1", Name: "Ext", Activated: true}}
					}
					writeResponse(c, req.Method, resp)
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

// captureOutput captures stdout and stderr during function execution.
func captureOutput(f func()) (stdout, stderr string) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	f()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var bufOut, bufErr bytes.Buffer
	io.Copy(&bufOut, rOut)
	io.Copy(&bufErr, rErr)
	rOut.Close()
	rErr.Close()

	return bufOut.String(), bufErr.String()
}

// assertContains checks if output contains the expected substring.
func assertContains(t *testing.T, output, expected string) {
	t.Helper()
	if !strings.Contains(output, expected) {
		t.Errorf("expected output to contain %q, got:\n%s", expected, output)
	}
}

func TestExtCommands(t *testing.T) {
	srv := startFakeServer(t)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"."}, "install")
	if err := install(ctx); err != nil {
		t.Fatalf("install: %v", err)
	}

	ctx = newContext(app, []string{"ext1"}, "info")
	if err := info(ctx); err != nil {
		t.Fatalf("info: %v", err)
	}

	ctx = newContext(app, nil, "list")
	if err := list(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}

	ctx = newContext(app, []string{"ext1"}, "activate")
	if err := activate(ctx); err != nil {
		t.Fatalf("activate: %v", err)
	}

	ctx = newContext(app, []string{"ext1"}, "deactivate")
	if err := deactivate(ctx); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	ctx = newContext(app, []string{"ext1"}, "uninstall")
	if err := uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
}

func TestExtHelpPaths(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, []string{"help"}, "install")
	_ = install(ctx)
	ctx = newContext(app, []string{"help"}, "list")
	_ = list(ctx)
	ctx = newContext(app, []string{"help"}, "activate")
	_ = activate(ctx)
	ctx = newContext(app, []string{"help"}, "deactivate")
	_ = deactivate(ctx)
	ctx = newContext(app, []string{"help"}, "uninstall")
	_ = uninstall(ctx)
	ctx = newContext(app, []string{"help"}, "info")
	_ = info(ctx)
}

func TestExtMissingArgs(t *testing.T) {
	app := cli.NewApp()
	ctx := newContext(app, nil, "install")
	_ = install(ctx)
	ctx = newContext(app, nil, "uninstall")
	_ = uninstall(ctx)
	ctx = newContext(app, nil, "activate")
	_ = activate(ctx)
	ctx = newContext(app, nil, "deactivate")
	_ = deactivate(ctx)
	ctx = newContext(app, nil, "info")
	_ = info(ctx)
}

func TestExtCommandsErrorResponse(t *testing.T) {
	fail := map[common.UpdateType]string{
		common.UPDATE_ADD_EXT:        "add failed",
		common.UPDATE_GET_EXT:        "get failed",
		common.UPDATE_LIST_EXT:       "list failed",
		common.UPDATE_ACTIVATE_EXT:   "activate failed",
		common.UPDATE_DEACTIVATE_EXT: "deactivate failed",
		common.UPDATE_DELETE_EXT:     "delete failed",
	}
	srv := startFakeServer(t, fail)
	defer srv.close()

	app := cli.NewApp()
	ctx := newContext(app, []string{"."}, "install")
	if err := install(ctx); err != nil {
		t.Fatalf("install: %v", err)
	}
	ctx = newContext(app, []string{"ext1"}, "info")
	if err := info(ctx); err != nil {
		t.Fatalf("info: %v", err)
	}
	ctx = newContext(app, nil, "list")
	if err := list(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}
	ctx = newContext(app, []string{"ext1"}, "activate")
	if err := activate(ctx); err != nil {
		t.Fatalf("activate: %v", err)
	}
	ctx = newContext(app, []string{"ext1"}, "deactivate")
	if err := deactivate(ctx); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	ctx = newContext(app, []string{"ext1"}, "uninstall")
	if err := uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
}

func TestExtInstallGetwdError(t *testing.T) {
	base := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if err := os.RemoveAll(base); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWD)
	}()

	app := cli.NewApp()
	ctx := newContext(app, []string{"."}, "install")
	if err := install(ctx); err != nil {
		t.Fatalf("install: %v", err)
	}
}

func TestExtInstallClientError(t *testing.T) {
	oldClient := newClient
	newClient = func() (*warpcli.Client, error) {
		return nil, errors.New("client error")
	}
	defer func() { newClient = oldClient }()

	app := cli.NewApp()
	ctx := newContext(app, []string{"."}, "install")
	if err := install(ctx); err != nil {
		t.Fatalf("install: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Extension Command Output Tests
// -----------------------------------------------------------------------------

// TestOutput_ExtInstall_NoPath verifies install command shows error when no path provided.
func TestOutput_ExtInstall_NoPath(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "install")

	stdout, _ := captureOutput(func() {
		_ = install(ctx)
	})

	assertContains(t, stdout, "no path provided")
}

// TestOutput_ExtUninstall_NoId verifies uninstall command shows error when no ID provided.
func TestOutput_ExtUninstall_NoId(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "uninstall")

	stdout, _ := captureOutput(func() {
		_ = uninstall(ctx)
	})

	assertContains(t, stdout, "no extension id provided")
}

// TestOutput_ExtActivate_NoId verifies activate command shows error when no ID provided.
func TestOutput_ExtActivate_NoId(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "activate")

	stdout, _ := captureOutput(func() {
		_ = activate(ctx)
	})

	assertContains(t, stdout, "no extension id provided")
}

// TestOutput_ExtDeactivate_NoId verifies deactivate command shows error when no ID provided.
func TestOutput_ExtDeactivate_NoId(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "deactivate")

	stdout, _ := captureOutput(func() {
		_ = deactivate(ctx)
	})

	assertContains(t, stdout, "no extension id provided")
}

// TestOutput_ExtInfo_NoId verifies info command shows error when no ID provided.
func TestOutput_ExtInfo_NoId(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "info")

	stdout, _ := captureOutput(func() {
		_ = info(ctx)
	})

	assertContains(t, stdout, "no extension id provided")
}

// TestOutput_ExtList_Empty verifies list command output with empty extension list.
func TestOutput_ExtList_Empty(t *testing.T) {
	emptyListOverride = true
	defer func() { emptyListOverride = false }()

	srv := startFakeServer(t)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "list")

	stdout, _ := captureOutput(func() {
		_ = list(ctx)
	})

	// Should show table header even if empty
	assertContains(t, stdout, "Name")
	assertContains(t, stdout, "Unique Hash")
	assertContains(t, stdout, "Activated")
}

// TestOutput_ExtList_Success verifies list command output with extensions.
func TestOutput_ExtList_Success(t *testing.T) {
	srv := startFakeServer(t)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "list")

	stdout, _ := captureOutput(func() {
		_ = list(ctx)
	})

	// Should show table with extension info
	assertContains(t, stdout, "Ext")
	assertContains(t, stdout, "ext1")
	assertContains(t, stdout, "true")
}

// TestOutput_ExtInstall_Success verifies install command success output.
func TestOutput_ExtInstall_Success(t *testing.T) {
	srv := startFakeServer(t)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"."}, "install")

	stdout, _ := captureOutput(func() {
		_ = install(ctx)
	})

	assertContains(t, stdout, "Successfully installed extension")
	assertContains(t, stdout, "Ext")
	assertContains(t, stdout, "1.0")
}

// TestOutput_ExtUninstall_Success verifies uninstall command success output.
func TestOutput_ExtUninstall_Success(t *testing.T) {
	srv := startFakeServer(t)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"ext1"}, "uninstall")

	stdout, _ := captureOutput(func() {
		_ = uninstall(ctx)
	})

	assertContains(t, stdout, "Successfully uninstalled extension")
	assertContains(t, stdout, "Ext")
}

// TestOutput_ExtActivate_Success verifies activate command success output.
func TestOutput_ExtActivate_Success(t *testing.T) {
	srv := startFakeServer(t)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"ext1"}, "activate")

	stdout, _ := captureOutput(func() {
		_ = activate(ctx)
	})

	assertContains(t, stdout, "Successfully activated extension")
	assertContains(t, stdout, "Ext")
	assertContains(t, stdout, "1.0")
}

// TestOutput_ExtDeactivate_Success verifies deactivate command success output.
func TestOutput_ExtDeactivate_Success(t *testing.T) {
	srv := startFakeServer(t)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"ext1"}, "deactivate")

	stdout, _ := captureOutput(func() {
		_ = deactivate(ctx)
	})

	assertContains(t, stdout, "Successfully deactivated extension")
	assertContains(t, stdout, "Ext")
}

// TestOutput_ExtInfo_Success verifies info command success output.
func TestOutput_ExtInfo_Success(t *testing.T) {
	srv := startFakeServer(t)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"ext1"}, "info")

	stdout, _ := captureOutput(func() {
		_ = info(ctx)
	})

	assertContains(t, stdout, "Extension Info")
	assertContains(t, stdout, "Name")
	assertContains(t, stdout, "Version")
	assertContains(t, stdout, "Description")
	assertContains(t, stdout, "Ext")
	assertContains(t, stdout, "1.0")
	assertContains(t, stdout, "desc")
}
