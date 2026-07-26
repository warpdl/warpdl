package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
)

type lifecycleListener struct {
	net.Listener
	closeOnce sync.Once
	closed    chan struct{}
}

func newLifecycleListener(t *testing.T) *lifecycleListener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return &lifecycleListener{
		Listener: listener,
		closed:   make(chan struct{}),
	}
}

func (l *lifecycleListener) Close() error {
	err := l.Listener.Close()
	l.closeOnce.Do(func() { close(l.closed) })
	return err
}

type failingLifecycleListener struct {
	err       error
	closeOnce sync.Once
	closed    chan struct{}
}

func (l *failingLifecycleListener) Accept() (net.Conn, error) {
	return nil, l.err
}

func (l *failingLifecycleListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *failingLifecycleListener) Addr() net.Addr {
	return lifecycleAddr("failing-listener")
}

type lifecycleAddr string

func (a lifecycleAddr) Network() string { return "test" }
func (a lifecycleAddr) String() string  { return string(a) }

func TestServerStartPropagatesWebBindFailure(t *testing.T) {
	bindErr := errors.New("web bind failed")
	server := NewServer(log.New(io.Discard, "", 0), nil, 0, nil, nil, nil)
	server.ws.listen = func(string, string) (net.Listener, error) {
		return nil, bindErr
	}
	ipcCalled := false
	server.listenIPC = func() (net.Listener, error) {
		ipcCalled = true
		return nil, errors.New("unexpected IPC bind")
	}

	err := server.Start(context.Background())
	if !errors.Is(err, bindErr) {
		t.Fatalf("Start error = %v, want web bind error", err)
	}
	if ipcCalled {
		t.Fatal("IPC listener was created after the web bind failed")
	}
}

func TestServerIPCBindFailureStopsWebServer(t *testing.T) {
	t.Setenv(common.SocketPathEnv, filepath.Join(t.TempDir(), "warpdl.sock"))
	ipcErr := errors.New("IPC bind failed")
	webListener := newLifecycleListener(t)

	server := NewServer(log.New(io.Discard, "", 0), nil, 0, nil, nil, nil)
	server.ws.listen = func(string, string) (net.Listener, error) {
		return webListener, nil
	}
	servedBeforeIPCBind := false
	server.listenIPC = func() (net.Listener, error) {
		conn, err := net.DialTimeout("tcp", webListener.Addr().String(), time.Second)
		if err != nil {
			t.Fatalf("dial prepared web listener: %v", err)
		}
		if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")); err != nil {
			_ = conn.Close()
			t.Fatalf("write prepared web listener: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
			_ = conn.Close()
			t.Fatalf("set prepared web listener deadline: %v", err)
		}
		buffer := make([]byte, 1)
		if n, _ := conn.Read(buffer); n != 0 {
			servedBeforeIPCBind = true
		}
		_ = conn.Close()
		return nil, ipcErr
	}

	err := server.Start(context.Background())
	if !errors.Is(err, ipcErr) {
		t.Fatalf("Start error = %v, want IPC bind error", err)
	}
	if servedBeforeIPCBind {
		t.Fatal("web server accepted a request before the IPC listener was bound")
	}
	select {
	case <-webListener.closed:
	default:
		t.Fatal("web listener leaked after IPC bind failure")
	}
	if conn, dialErr := net.DialTimeout("tcp", webListener.Addr().String(), 100*time.Millisecond); dialErr == nil {
		_ = conn.Close()
		t.Fatal("web listener still accepted connections after rollback")
	}
}

func TestServerStartPropagatesWebRuntimeFailure(t *testing.T) {
	t.Setenv(common.SocketPathEnv, filepath.Join(t.TempDir(), "warpdl.sock"))
	runtimeErr := errors.New("web accept failed")
	webListener := &failingLifecycleListener{
		err:    runtimeErr,
		closed: make(chan struct{}),
	}
	ipcListener := newLifecycleListener(t)

	server := NewServer(log.New(io.Discard, "", 0), nil, 0, nil, nil, nil)
	server.ws.listen = func(string, string) (net.Listener, error) {
		return webListener, nil
	}
	server.listenIPC = func() (net.Listener, error) {
		return ipcListener, nil
	}

	err := server.Start(context.Background())
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("Start error = %v, want web runtime error", err)
	}
	if errors.Is(err, ErrDrainIncomplete) {
		t.Fatalf("completed server loops reported incomplete drain: %v", err)
	}
	select {
	case <-ipcListener.closed:
	default:
		t.Fatal("IPC listener remained open after web runtime failure")
	}
}

func TestServerShutdownClosesAndWaitsForIPCConnections(t *testing.T) {
	t.Setenv(common.SocketPathEnv, filepath.Join(t.TempDir(), "warpdl.sock"))
	ipcListener := newLifecycleListener(t)
	server := NewServer(log.New(io.Discard, "", 0), nil, 0, nil, nil, nil)
	server.listenIPC = func() (net.Listener, error) {
		return ipcListener, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	startErr := make(chan error, 1)
	go func() {
		startErr <- server.Start(ctx)
	}()

	client, err := net.DialTimeout("tcp", ipcListener.Addr().String(), time.Second)
	if err != nil {
		cancel()
		t.Fatalf("dial IPC listener: %v", err)
	}
	defer client.Close()

	waitForLifecycleCondition(t, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(server.connections) == 1
	})

	cancel()
	select {
	case err := <-startErr:
		if err != nil {
			t.Fatalf("Start returned error during cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not wait for IPC connection shutdown")
	}

	server.mu.Lock()
	activeConnections := len(server.connections)
	server.mu.Unlock()
	if activeConnections != 0 {
		t.Fatalf("active IPC connections = %d, want 0", activeConnections)
	}
	if err := client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("IPC client remained connected after Server.Start returned")
	}
}

func TestWaitForServerGroupMarksOnlyIncompleteDrain(t *testing.T) {
	t.Run("eventual drain", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var group sync.WaitGroup
		group.Add(1)
		go func() {
			time.Sleep(5 * time.Millisecond)
			group.Done()
		}()

		err := waitForServerGroupWithTimeout(ctx, &group, time.Second)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v, want context cancellation", err)
		}
		if errors.Is(err, ErrDrainIncomplete) {
			t.Fatalf("eventually drained group reported incomplete drain: %v", err)
		}
	})

	t.Run("incomplete drain", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var group sync.WaitGroup
		group.Add(1)
		err := waitForServerGroupWithTimeout(ctx, &group, 5*time.Millisecond)
		group.Done()
		if !errors.Is(err, ErrDrainIncomplete) {
			t.Fatalf("wait error = %v, want ErrDrainIncomplete", err)
		}
	})
}

func TestWaitForServerResultMarksTimeoutAsIncompleteDrain(t *testing.T) {
	result := make(chan error)
	err := waitForServerResultWithTimeout(result, 5*time.Millisecond)
	if !errors.Is(err, ErrDrainIncomplete) {
		t.Fatalf("wait error = %v, want ErrDrainIncomplete", err)
	}

	runtimeErr := errors.New("serve failed")
	result = make(chan error, 1)
	result <- runtimeErr
	err = waitForServerResultWithTimeout(result, time.Second)
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("wait error = %v, want runtime error", err)
	}
	if errors.Is(err, ErrDrainIncomplete) {
		t.Fatalf("completed server loop reported incomplete drain: %v", err)
	}
}

func waitForLifecycleCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for lifecycle condition")
}
