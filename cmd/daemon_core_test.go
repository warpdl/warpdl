package cmd

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/pkg/credman/keyring"
	"github.com/warpdl/warpdl/pkg/logger"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestLoggerKeyringAdapterWarning(t *testing.T) {
	l := &loggerKeyringAdapter{log: logger.NewNopLogger()}
	l.Warning("test warning: %s %d", "arg", 42) // must not panic
}

func TestGetCookieManagerWithLogger_KeyringGetSuccess(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{getKey: bytes.Repeat([]byte{0x22}, 32)}
	oldKeyring := newKeyring
	newKeyring = func(configDir string, _ keyring.Logger) keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	cm, err := getCookieManagerWithLogger(logger.NewNopLogger())
	if err != nil {
		t.Fatalf("getCookieManagerWithLogger: %v", err)
	}
	defer cm.Close()
}

func TestGetCookieManagerWithLogger_KeyringSetKeySuccess(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{
		getErr: errors.New("no key"),
		setKey: bytes.Repeat([]byte{0x33}, 32),
	}
	oldKeyring := newKeyring
	newKeyring = func(configDir string, _ keyring.Logger) keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	cm, err := getCookieManagerWithLogger(logger.NewNopLogger())
	if err != nil {
		t.Fatalf("getCookieManagerWithLogger: %v", err)
	}
	defer cm.Close()
}

func TestGetCookieManagerWithLogger_KeyringSetKeyError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{
		getErr: errors.New("no key"),
		setErr: errors.New("set failed"),
	}
	oldKeyring := newKeyring
	newKeyring = func(configDir string, _ keyring.Logger) keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	if _, err := getCookieManagerWithLogger(logger.NewNopLogger()); err == nil {
		t.Fatal("expected error for keyring set failure")
	}
}

func TestGetCookieManagerWithLogger_InvalidHex(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "not-valid-hex")

	if _, err := getCookieManagerWithLogger(logger.NewNopLogger()); err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestGetCookieManagerWithLogger_CredmanError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write corrupt GOB data to cookie file so NewCookieManager fails to load
	cookiePath := base + "/cookies.warp"
	if err := os.WriteFile(cookiePath, []byte("not valid gob data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv(cookieKeyEnv, strings.Repeat("bb", 32))

	_, err := getCookieManagerWithLogger(logger.NewNopLogger())
	if err == nil {
		t.Fatal("expected error for corrupt cookie file")
	}
}

func TestInitDaemonComponents_WithCookieKey(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if err := extl.SetEngineStore(base); err != nil {
		t.Fatalf("SetEngineStore: %v", err)
	}

	t.Setenv(cookieKeyEnv, strings.Repeat("11", 32))

	oldBuildArgs := currentBuildArgs
	currentBuildArgs = BuildArgs{
		Version:   "1.0.0",
		Commit:    "test",
		BuildType: "test",
	}
	defer func() { currentBuildArgs = oldBuildArgs }()

	components, err := initDaemonComponents(logger.NewNopLogger(), 0, nil)
	if err != nil {
		t.Fatalf("initDaemonComponents: %v", err)
	}
	if components == nil || components.Server == nil || components.Manager == nil || components.Api == nil {
		t.Fatal("initDaemonComponents returned incomplete components")
	}

	components.Close()
}
