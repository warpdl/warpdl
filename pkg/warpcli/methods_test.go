//go:build !windows

package warpcli

import (
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestNewClient(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_ = client.conn.Close()
	<-done
}

func TestClientRemoveHandlerDisconnect(t *testing.T) {
	client := &Client{
		mu:     &sync.RWMutex{},
		d:      &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
		listen: true,
	}
	client.AddHandler(common.UPDATE_DOWNLOADING, HandlerFunc(func(json.RawMessage) error { return nil }))
	client.RemoveHandler(common.UPDATE_DOWNLOADING)
	if len(client.d.Handlers) != 0 {
		t.Fatalf("expected handlers to be removed")
	}
	client.Disconnect()
	if client.listen {
		t.Fatalf("expected listen to be false after Disconnect")
	}
}

func TestClientMethods(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}
	go func() {
		for {
			reqBytes, err := read(c2)
			if err != nil {
				return
			}
			var req Request
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				return
			}
			var payload []byte
			switch req.Method {
			case common.UPDATE_RESUME:
				payload, _ = json.Marshal(common.ResumeResponse{
					FileName:          "file.bin",
					SavePath:          "file.bin",
					DownloadDirectory: ".",
					AbsoluteLocation:  ".",
					ContentLength:     warplib.ContentLength(10),
					MaxConnections:    1,
					MaxSegments:       1,
				})
			case common.UPDATE_LIST:
				payload, _ = json.Marshal(common.ListResponse{Items: []*warplib.Item{}})
			case common.UPDATE_ATTACH:
				payload, _ = json.Marshal(common.DownloadResponse{DownloadId: "id"})
			case common.UPDATE_STOP, common.UPDATE_FLUSH:
				payload = []byte(`{}`)
			case common.UPDATE_ADD_EXT, common.UPDATE_GET_EXT, common.UPDATE_ACTIVATE_EXT:
				payload, _ = json.Marshal(common.ExtensionInfo{Name: "Ext"})
			case common.UPDATE_DELETE_EXT, common.UPDATE_DEACTIVATE_EXT:
				payload, _ = json.Marshal(common.ExtensionName{Name: "Ext"})
			case common.UPDATE_LIST_EXT:
				payload, _ = json.Marshal([]common.ExtensionInfoShort{{Name: "Ext"}})
			default:
				payload = []byte(`{}`)
			}
			respBytes, _ := json.Marshal(Response{
				Ok:     true,
				Update: &Update{Type: req.Method, Message: json.RawMessage(payload)},
			})
			_ = write(c2, respBytes)
		}
	}()

	if _, err := client.Resume("id", nil); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if _, err := client.List(nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	if ok, err := client.Flush("id"); err != nil || !ok {
		t.Fatalf("Flush: %v", err)
	}
	if _, err := client.AttachDownload("id"); err != nil {
		t.Fatalf("AttachDownload: %v", err)
	}
	if ok, err := client.StopDownload("id"); err != nil || !ok {
		t.Fatalf("StopDownload: %v", err)
	}
	if _, err := client.AddExtension("."); err != nil {
		t.Fatalf("AddExtension: %v", err)
	}
	if _, err := client.GetExtension("id"); err != nil {
		t.Fatalf("GetExtension: %v", err)
	}
	if _, err := client.DeleteExtension("id"); err != nil {
		t.Fatalf("DeleteExtension: %v", err)
	}
	if _, err := client.DeactivateExtension("id"); err != nil {
		t.Fatalf("DeactivateExtension: %v", err)
	}
	if _, err := client.ActivateExtension("id"); err != nil {
		t.Fatalf("ActivateExtension: %v", err)
	}
	if _, err := client.ListExtension(true); err != nil {
		t.Fatalf("ListExtension: %v", err)
	}
}
