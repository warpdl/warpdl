package warpcli

import (
	"encoding/json"
	"net"
	"sync"
	"testing"

	"github.com/warpdl/warpdl/common"
)

func TestBufioRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	msg := []byte("hello")
	go func() {
		_ = write(c1, msg)
	}()
	got, err := read(c2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("unexpected payload: %s", string(got))
	}
}

func TestDispatcherProcess(t *testing.T) {
	d := &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)}
	if err := d.process([]byte(`{"ok":true,"update":{"type":"download","message":{}}}`)); err == nil {
		t.Fatalf("expected error for missing handler")
	}
	called := false
	d.AddHandler(common.UPDATE_DOWNLOAD, HandlerFunc(func(b json.RawMessage) error {
		called = true
		return nil
	}))
	if err := d.process([]byte(`{"ok":true,"update":{"type":"download","message":{}}}`)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}
}

type HandlerFunc func(json.RawMessage) error

func (h HandlerFunc) Handle(b json.RawMessage) error { return h(b) }

func TestDownloadingHandler(t *testing.T) {
	called := false
	h := NewDownloadingHandler(common.DownloadProgress, func(dr *common.DownloadingResponse) error {
		called = true
		return nil
	})
	msg := []byte(`{"action":"download_progress","value":5}`)
	if err := h.Handle(msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !called {
		t.Fatalf("expected callback to be called")
	}
}

func TestClientInvokeDownload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d: &Dispatcher{
			Handlers: make(map[common.UpdateType][]Handler),
		},
	}
	go func() {
		reqBytes, err := read(c2)
		if err != nil {
			return
		}
		var req Request
		_ = json.Unmarshal(reqBytes, &req)
		respMsg, _ := json.Marshal(common.DownloadResponse{DownloadId: "id", FileName: "file", DownloadDirectory: "."})
		respBytes, _ := json.Marshal(Response{Ok: true, Update: &Update{Type: req.Method, Message: respMsg}})
		_ = write(c2, respBytes)
	}()

	resp, err := client.Download("http://example.com", "file", ".", nil)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if resp.DownloadId != "id" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestClientListenDisconnect(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d: &Dispatcher{
			Handlers: make(map[common.UpdateType][]Handler),
		},
	}
	client.AddHandler(common.UPDATE_DOWNLOADING, HandlerFunc(func(b json.RawMessage) error {
		return ErrDisconnect
	}))
	go func() {
		respBytes, _ := json.Marshal(Response{Ok: true, Update: &Update{Type: common.UPDATE_DOWNLOADING, Message: json.RawMessage(`{"action":"download_progress"}`)}})
		_ = write(c2, respBytes)
	}()
	if err := client.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
}
