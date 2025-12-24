package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/urfave/cli"
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

	if _, err := getCookieManager(newContext(cli.NewApp(), nil, "daemon")); err != nil {
		t.Fatalf("getCookieManager: %v", err)
	}
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
	if cm == nil {
		t.Fatal("expected non-nil cookie manager")
	}
}
