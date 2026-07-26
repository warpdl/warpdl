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
	"golang.org/x/net/websocket"
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

	extensionConn, err := websocket.Dial(
		"ws://"+address+"/",
		"",
		"http://"+address,
	)
	if err != nil {
		t.Fatalf("dial extension WebSocket: %v", err)
	}
	defer extensionConn.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), time.Second)
	defer cancelDial()
	rpcConn, _, err := cws.Dial(dialCtx, "ws://"+address+"/jsonrpc/ws", nil)
	if err != nil {
		t.Fatalf("dial JSON-RPC WebSocket: %v", err)
	}
	defer rpcConn.CloseNow()

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

	if err := extensionConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("set extension read deadline: %v", err)
	}
	if _, err := extensionConn.Read(make([]byte, 1)); err == nil {
		t.Fatal("extension WebSocket remained open after shutdown")
	}
	readCtx, cancelRead := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelRead()
	if _, _, err := rpcConn.Read(readCtx); err == nil {
		t.Fatal("JSON-RPC WebSocket remained open after shutdown")
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
