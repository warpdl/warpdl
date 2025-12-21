package ext

import (
	"encoding/json"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
)

type fakeServer struct {
	listener net.Listener
	wg       sync.WaitGroup
}

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
					resp := []common.ExtensionInfoShort{{ExtensionId: "ext1", Name: "Ext", Activated: true}}
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

func TestExtCommands(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	srv := startFakeServer(t, socketPath)
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
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	if err := os.Setenv("WARPDL_SOCKET_PATH", socketPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	fail := map[common.UpdateType]string{
		common.UPDATE_ADD_EXT:       "add failed",
		common.UPDATE_GET_EXT:       "get failed",
		common.UPDATE_LIST_EXT:      "list failed",
		common.UPDATE_ACTIVATE_EXT:  "activate failed",
		common.UPDATE_DEACTIVATE_EXT:"deactivate failed",
		common.UPDATE_DELETE_EXT:    "delete failed",
	}
	srv := startFakeServer(t, socketPath, fail)
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
