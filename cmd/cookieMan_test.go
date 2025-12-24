package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/types"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type fakeKeyring struct {
	getKey []byte
	getErr error
	setKey []byte
	setErr error
	gotGet bool
	gotSet bool
}

func (f *fakeKeyring) GetKey() ([]byte, error) {
	f.gotGet = true
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.getKey, nil
}

func (f *fakeKeyring) SetKey() ([]byte, error) {
	f.gotSet = true
	if f.setErr != nil {
		return nil, f.setErr
	}
	return f.setKey, nil
}

func TestGetCookieManagerKeyringFallback(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{
		getErr: errors.New("missing"),
		setKey: bytes.Repeat([]byte{0x11}, 32),
	}
	oldKeyring := newKeyring
	newKeyring = func() keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	cm, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon"))
	if err != nil {
		t.Fatalf("getCookieManager: %v", err)
	}
	defer cm.Close()
	if cm == nil || !fake.gotGet || !fake.gotSet {
		t.Fatalf("expected keyring GetKey and SetKey to be called")
	}
}

func TestGetCookieManagerKeyringGetSuccess(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{
		getKey: bytes.Repeat([]byte{0x22}, 32),
	}
	oldKeyring := newKeyring
	newKeyring = func() keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	cm, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon"))
	if err != nil {
		t.Fatalf("getCookieManager: %v", err)
	}
	defer cm.Close()
	if cm == nil || !fake.gotGet || fake.gotSet {
		t.Fatalf("expected keyring GetKey only")
	}
}

func TestGetCookieManagerKeyringSetError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	fake := &fakeKeyring{
		getErr: errors.New("missing"),
		setErr: errors.New("set failed"),
	}
	oldKeyring := newKeyring
	newKeyring = func() keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	if _, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon")); err == nil {
		t.Fatalf("expected error for keyring set failure")
	}
}

func TestGetCookieManagerEnv(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	keyHex := strings.Repeat("11", 32)
	t.Setenv(cookieKeyEnv, keyHex)

	cm, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon"))
	if err != nil {
		t.Fatalf("getCookieManager: %v", err)
	}
	defer cm.Close()
}

func TestGetCookieManagerEnv_InvalidHex(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	// Invalid hex string
	t.Setenv(cookieKeyEnv, "not-valid-hex")

	_, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon"))
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestGetCookieManagerEnv_ValidHex(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	// Valid 32-byte key in hex
	keyHex := strings.Repeat("ab", 32)
	t.Setenv(cookieKeyEnv, keyHex)

	cm, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon"))
	if err != nil {
		t.Fatalf("getCookieManager: %v", err)
	}
	defer cm.Close()
	if cm == nil {
		t.Fatal("expected non-nil cookie manager")
	}
}

// TestGetCookieManagerEnvCredmanError tests credman initialization failure
// when WARPDL_COOKIE_KEY is set to valid hex but the resulting key fails
// during cookie file decryption. This simulates a corrupted cookie file that
// was encrypted with a different key.
func TestGetCookieManagerEnvCredmanError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	cookiePath := filepath.Join(base, "cookies.warp")

	// First, create a valid cookie file with a 32-byte key
	validKey := bytes.Repeat([]byte{0xaa}, 32)
	cm, err := credman.NewCookieManager(cookiePath, validKey)
	if err != nil {
		t.Fatalf("NewCookieManager: %v", err)
	}
	if err := cm.SetCookie(types.Cookie{Name: "test", Value: "encrypted-data"}); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	if err := cm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Now try to open with a different 32-byte key via environment
	// This should fail during loadCookies when it tries to decrypt existing data
	_ = bytes.Repeat([]byte{0xbb}, 32)
	t.Setenv(cookieKeyEnv, strings.ToUpper(string([]byte("bb")))[:2]+strings.Repeat("bb", 31))

	// Actually, the decode happens on the GOB data, not the encrypted values
	// So we need to corrupt the GOB structure itself
	// Let's just write invalid GOB data to the cookie file
	if err := os.WriteFile(cookiePath, []byte("not valid gob data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv(cookieKeyEnv, strings.Repeat("bb", 32))

	_, err = getCookieManager(newContext(cli.NewApp(), nil, "daemon"))
	if err == nil {
		t.Fatal("expected error for corrupt cookie file")
	}
}

// TestGetCookieManagerKeyringCredmanError tests credman initialization failure
// when keyring returns a valid key but the cookie file has corrupt GOB data.
func TestGetCookieManagerKeyringCredmanError(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	t.Setenv(cookieKeyEnv, "")

	cookiePath := filepath.Join(base, "cookies.warp")

	// Create corrupt cookie file with invalid GOB data
	if err := os.WriteFile(cookiePath, []byte("corrupt gob data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Return a valid key from keyring
	fake := &fakeKeyring{
		getKey: bytes.Repeat([]byte{0x22}, 32),
	}
	oldKeyring := newKeyring
	newKeyring = func() keyringProvider { return fake }
	defer func() { newKeyring = oldKeyring }()

	_, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon"))
	if err == nil {
		t.Fatal("expected error for corrupt cookie file from keyring path")
	}
}
