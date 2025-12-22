package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestDaemonStartStub(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	var cm *credman.CookieManager
	oldCookie := cookieManagerFunc
	oldStart := startServerFunc
	cookieManagerFunc = func(*cli.Context) (*credman.CookieManager, error) {
		key := bytes.Repeat([]byte{0x11}, 32)
		m, err := credman.NewCookieManager(filepath.Join(base, "cookies.warp"), key)
		cm = m
		return m, err
	}
	startServerFunc = func(*server.Server, context.Context) error { return nil }
	defer func() {
		cookieManagerFunc = oldCookie
		startServerFunc = oldStart
		if cm != nil {
			_ = cm.Close()
		}
	}()

	ctx := newContext(cli.NewApp(), nil, "daemon")
	if err := daemon(ctx); err != nil {
		t.Fatalf("daemon: %v", err)
	}
}

func TestDaemonCookieManagerError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	oldCookie := cookieManagerFunc
	cookieManagerFunc = func(*cli.Context) (*credman.CookieManager, error) {
		return nil, errors.New("cookie manager error")
	}
	defer func() {
		cookieManagerFunc = oldCookie
	}()

	ctx := newContext(cli.NewApp(), nil, "daemon")
	// daemon returns nil even on error (errors are logged, not returned)
	err := daemon(ctx)
	if err != nil {
		t.Fatalf("daemon returned unexpected error: %v", err)
	}
}

func TestDaemonCleanupStalePidFileError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := WritePidFile(); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}
	ctx := newContext(cli.NewApp(), nil, "daemon")
	if err := daemon(ctx); err != nil {
		t.Fatalf("daemon: %v", err)
	}
}

func TestDaemonExtEngineError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	oldEngineStore := extl.ENGINE_STORE
	oldModuleStore := extl.MODULE_STORE
	extl.ENGINE_STORE = filepath.Join(t.TempDir(), "missing")
	extl.MODULE_STORE = filepath.Join(extl.ENGINE_STORE, "extstore")
	defer func() {
		extl.ENGINE_STORE = oldEngineStore
		extl.MODULE_STORE = oldModuleStore
	}()

	var cm *credman.CookieManager
	oldCookie := cookieManagerFunc
	cookieManagerFunc = func(*cli.Context) (*credman.CookieManager, error) {
		key := bytes.Repeat([]byte{0x11}, 32)
		m, err := credman.NewCookieManager(filepath.Join(base, "cookies.warp"), key)
		cm = m
		return m, err
	}
	defer func() {
		cookieManagerFunc = oldCookie
		if cm != nil {
			_ = cm.Close()
		}
	}()

	ctx := newContext(cli.NewApp(), nil, "daemon")
	if err := daemon(ctx); err != nil {
		t.Fatalf("daemon: %v", err)
	}
}

func TestDaemonWritePidFileError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := os.Chmod(base, 0555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer os.Chmod(base, 0755)

	ctx := newContext(cli.NewApp(), nil, "daemon")
	if err := daemon(ctx); err != nil {
		t.Fatalf("daemon: %v", err)
	}
}
