package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"testing"
	"time"

	cws "github.com/coder/websocket"
)

func TestWebServerShutdownDrainsHijackedWebSockets(t *testing.T) {
	webServer := NewWebServer(
		log.New(io.Discard, "", 0),
		nil,
		NewPool(log.New(io.Discard, "", 0)),
		0,
		nil,
		nil,
		&RPCConfig{},
	)
	serveErr, err := webServer.startAsync()
	if err != nil {
		t.Fatalf("startAsync: %v", err)
	}

	webServer.mu.Lock()
	address := webServer.listener.Addr().String()
	webServer.mu.Unlock()

	conns := make([]*cws.Conn, 0, 2)
	for i := range 2 {
		dialCtx, cancelDial := context.WithTimeout(context.Background(), time.Second)
		conn, _, err := cws.Dial(dialCtx, "ws://"+address+"/jsonrpc/ws", nil)
		cancelDial()
		if err != nil {
			t.Fatalf("dial JSON-RPC WebSocket %d: %v", i+1, err)
		}
		defer func() { _ = conn.CloseNow() }()
		conns = append(conns, conn)
	}

	waitForLifecycleCondition(t, func() bool {
		webServer.mu.Lock()
		defer webServer.mu.Unlock()
		return len(webServer.activeHandlers) == 2
	})

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	if err := webServer.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("serve loop error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve loop did not stop")
	}

	webServer.mu.Lock()
	activeHandlers := len(webServer.activeHandlers)
	webServer.mu.Unlock()
	if activeHandlers != 0 {
		t.Fatalf("active web handlers = %d, want 0", activeHandlers)
	}

	for i, conn := range conns {
		readCtx, cancelRead := context.WithTimeout(context.Background(), 2*time.Second)
		_, _, err := conn.Read(readCtx)
		cancelRead()
		if err == nil {
			t.Fatalf("JSON-RPC WebSocket %d remained open after shutdown", i+1)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("JSON-RPC WebSocket %d was not closed by shutdown: %v", i+1, err)
		}
	}

	repeatCtx, cancelRepeat := context.WithTimeout(context.Background(), time.Second)
	defer cancelRepeat()
	if err := webServer.Shutdown(repeatCtx); err != nil {
		t.Fatalf("repeated Shutdown: %v", err)
	}
}

func TestWebServerShutdownPropagatesListenerCloseError(t *testing.T) {
	closeErr := &lifecycleCloseError{err: context.Canceled}
	webServer := NewWebServer(
		log.New(io.Discard, "", 0),
		nil,
		NewPool(log.New(io.Discard, "", 0)),
		0,
		nil,
		nil,
		nil,
	)
	webServer.listen = closeErr.listen
	serveErr, err := webServer.startAsync()
	if err != nil {
		t.Fatalf("startAsync: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := webServer.Shutdown(shutdownCtx); err == nil {
		t.Fatal("Shutdown discarded listener close error")
	} else if errors.Is(err, ErrDrainIncomplete) {
		t.Fatalf("drained web server reported incomplete drain: %v", err)
	}
	select {
	case <-serveErr:
	case <-time.After(time.Second):
		t.Fatal("serve loop did not stop after listener close error")
	}
}

func TestWaitForWebDoneMarksOnlyIncompleteDrain(t *testing.T) {
	t.Run("eventual drain", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})
		go func() {
			time.Sleep(5 * time.Millisecond)
			close(done)
		}()

		quiesced, err := waitForWebDoneWithTimeout(ctx, done, time.Second)
		if !quiesced {
			t.Fatal("eventually drained web goroutine was not quiesced")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v, want context cancellation", err)
		}
		if errors.Is(err, ErrDrainIncomplete) {
			t.Fatalf("eventually drained web goroutine reported incomplete drain: %v", err)
		}
	})

	t.Run("incomplete drain", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})

		quiesced, err := waitForWebDoneWithTimeout(ctx, done, 5*time.Millisecond)
		close(done)
		if quiesced {
			t.Fatal("live web goroutine reported quiesced")
		}
		if !errors.Is(err, ErrDrainIncomplete) {
			t.Fatalf("wait error = %v, want ErrDrainIncomplete", err)
		}
	})
}

type lifecycleCloseError struct {
	err error
}

func (l *lifecycleCloseError) listen(network, address string) (net.Listener, error) {
	listener, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	return &closeErrorListener{Listener: listener, err: l.err}, nil
}

type closeErrorListener struct {
	net.Listener
	err error
}

func (l *closeErrorListener) Close() error {
	_ = l.Listener.Close()
	return l.err
}
