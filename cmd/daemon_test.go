package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestGetCookieManagerEnv(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	keyHex := strings.Repeat("11", 32)
	t.Setenv(cookieKeyEnv, keyHex)

	if _, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon")); err != nil {
		t.Fatalf("getCookieManager: %v", err)
	}
}

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
	startServerFunc = func(*server.Server) error { return nil }
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
