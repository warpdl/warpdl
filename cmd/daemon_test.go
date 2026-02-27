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
	"github.com/warpdl/warpdl/pkg/logger"
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
	oldInit := initDaemonComponents
	oldStart := startServerFunc
	initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
		key := bytes.Repeat([]byte{0x11}, 32)
		m, err := credman.NewCookieManager(filepath.Join(base, "cookies.warp"), key)
		if err != nil {
			return nil, err
		}
		cm = m
		return &DaemonComponents{
			CookieManager: m,
			Server:        &server.Server{},
		}, nil
	}
	startServerFunc = func(*server.Server, context.Context) error { return nil }
	defer func() {
		initDaemonComponents = oldInit
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

func TestDaemonInitComponentsError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	oldInit := initDaemonComponents
	initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
		return nil, errors.New("init components error")
	}
	defer func() {
		initDaemonComponents = oldInit
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
	oldInit := initDaemonComponents
	initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
		key := bytes.Repeat([]byte{0x11}, 32)
		m, err := credman.NewCookieManager(filepath.Join(base, "cookies.warp"), key)
		if err != nil {
			return nil, err
		}
		cm = m
		// Simulate ext engine error
		return nil, errors.New("extension engine error")
	}
	defer func() {
		initDaemonComponents = oldInit
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

// TestDaemonInitManagerError tests daemon initialization when userdata.warp
// contains invalid GOB data. warplib.InitManager() ignores GOB decode errors,
// so this test verifies the daemon can start successfully even with corrupt data.
// This is important because the daemon should be resilient to state file corruption
// and start with an empty download list rather than failing completely.
func TestDaemonInitManagerError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	// Create corrupt userdata.warp with invalid GOB data
	userdataPath := filepath.Join(base, "userdata.warp")
	if err := os.WriteFile(userdataPath, []byte("invalid gob data that will fail to decode"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var cm *credman.CookieManager
	oldInit := initDaemonComponents
	oldStart := startServerFunc
	initDaemonComponents = func(log logger.Logger, maxConcurrent int, rpcCfg *server.RPCConfig) (*DaemonComponents, error) {
		key := bytes.Repeat([]byte{0x11}, 32)
		m, err := credman.NewCookieManager(filepath.Join(base, "cookies.warp"), key)
		if err != nil {
			return nil, err
		}
		cm = m
		return &DaemonComponents{
			CookieManager: m,
			Server:        &server.Server{},
		}, nil
	}
	startServerFunc = func(*server.Server, context.Context) error { return nil }
	defer func() {
		initDaemonComponents = oldInit
		startServerFunc = oldStart
		if cm != nil {
			_ = cm.Close()
		}
	}()

	ctx := newContext(cli.NewApp(), nil, "daemon")

	// daemon should start successfully even with corrupt userdata
	// because InitManager ignores GOB decode errors
	if err := daemon(ctx); err != nil {
		t.Fatalf("daemon: %v", err)
	}
}
