package cmd

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
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

func TestDaemonRejectsNegativeMaxConcurrent(t *testing.T) {
	set := flag.NewFlagSet("daemon", flag.ContinueOnError)
	set.Int("max-concurrent", 0, "")
	if err := set.Parse([]string{"--max-concurrent", "-1"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ctx := cli.NewContext(cli.NewApp(), set, nil)
	ctx.Command = cli.Command{Name: "daemon"}

	oldInit := initDaemonComponents
	initCalled := false
	initDaemonComponents = func(logger.Logger, int, *server.RPCConfig) (*DaemonComponents, error) {
		initCalled = true
		return nil, nil
	}
	defer func() { initDaemonComponents = oldInit }()

	var gotErr error
	_, stderr := captureOutput(func() { gotErr = daemon(ctx) })
	assertExitError(t, gotErr)
	if initCalled {
		t.Fatal("negative max-concurrent reached daemon initialization")
	}
	if !strings.Contains(stderr, "max-concurrent must be zero or greater") {
		t.Fatalf("validation output = %q", stderr)
	}
}

func TestDaemonShutdownFailureReturnsExitError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	oldInit := initDaemonComponents
	oldStart := startServerFunc
	oldClose := closeDaemonComponentsFn
	shutdownErr := errors.New("final snapshot failed")
	initDaemonComponents = func(logger.Logger, int, *server.RPCConfig) (*DaemonComponents, error) {
		return &DaemonComponents{Server: &server.Server{}}, nil
	}
	startServerFunc = func(*server.Server, context.Context) error { return nil }
	closeDaemonComponentsFn = func(*DaemonComponents) error { return shutdownErr }
	defer func() {
		initDaemonComponents = oldInit
		startServerFunc = oldStart
		closeDaemonComponentsFn = oldClose
	}()

	ctx := newContext(cli.NewApp(), nil, "daemon")
	var gotErr error
	_, stderr := captureOutput(func() { gotErr = daemon(ctx) })
	assertExitError(t, gotErr)
	if !bytes.Contains([]byte(stderr), []byte("daemon[shutdown]")) {
		t.Fatalf("shutdown failure was not reported on stderr: %s", stderr)
	}
	if !bytes.Contains([]byte(stderr), []byte(shutdownErr.Error())) {
		t.Fatalf("shutdown cause was not reported on stderr: %s", stderr)
	}
}

func TestDaemonClosesComponentsOnlyAfterProvenServerDrain(t *testing.T) {
	tests := []struct {
		name      string
		serverErr error
		wantClose int
	}{
		{
			name:      "ordinary runtime error",
			serverErr: errors.New("listener failed"),
			wantClose: 1,
		},
		{
			name:      "incomplete drain",
			serverErr: errors.Join(errors.New("listener failed"), server.ErrDrainIncomplete),
			wantClose: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			if err := warplib.SetConfigDir(base); err != nil {
				t.Fatalf("SetConfigDir: %v", err)
			}
			if err := extl.SetEngineStore(base); err != nil {
				t.Fatalf("SetEngineStore: %v", err)
			}

			oldInit := initDaemonComponents
			oldStart := startServerFunc
			oldClose := closeDaemonComponentsFn
			closeCalls := 0
			initDaemonComponents = func(logger.Logger, int, *server.RPCConfig) (*DaemonComponents, error) {
				return &DaemonComponents{Server: &server.Server{}}, nil
			}
			startServerFunc = func(*server.Server, context.Context) error {
				return tt.serverErr
			}
			closeDaemonComponentsFn = func(*DaemonComponents) error {
				closeCalls++
				return nil
			}
			defer func() {
				initDaemonComponents = oldInit
				startServerFunc = oldStart
				closeDaemonComponentsFn = oldClose
			}()

			ctx := newContext(cli.NewApp(), nil, "daemon")
			err := daemon(ctx)
			if !errors.Is(err, tt.serverErr) {
				t.Fatalf("daemon error = %v, want %v", err, tt.serverErr)
			}
			if closeCalls != tt.wantClose {
				t.Fatalf("component close calls = %d, want %d", closeCalls, tt.wantClose)
			}
		})
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
	assertExitError(t, daemon(ctx))
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
	assertExitError(t, daemon(ctx))
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
	assertExitError(t, daemon(ctx))
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
	assertExitError(t, daemon(ctx))
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
