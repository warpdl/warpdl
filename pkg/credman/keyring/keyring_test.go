package keyring

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestKeyringSetGetDelete(t *testing.T) {
	origSet := keyringSet
	origGet := keyringGet
	origDelete := keyringDelete
	origRandRead := randRead
	defer func() {
		keyringSet = origSet
		keyringGet = origGet
		keyringDelete = origDelete
		randRead = origRandRead
	}()

	var setApp, setKey, setValue string
	keyringSet = func(app, key, value string) error {
		setApp = app
		setKey = key
		setValue = value
		return nil
	}
	randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0x01
		}
		return len(b), nil
	}

	kr := NewKeyring()
	key, err := kr.SetKey()
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}
	expectedHex := hex.EncodeToString(key)
	if setApp != kr.AppName || setKey != kr.KeyField || setValue != expectedHex {
		t.Fatalf("unexpected set call: %q %q %q", setApp, setKey, setValue)
	}

	keyringGet = func(app, key string) (string, error) {
		if app != kr.AppName || key != kr.KeyField {
			return "", errors.New("unexpected key")
		}
		return "value", nil
	}
	got, err := kr.GetKey()
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if string(got) != "value" {
		t.Fatalf("unexpected key value: %q", string(got))
	}

	var deleteApp, deleteKey string
	keyringDelete = func(app, key string) error {
		deleteApp = app
		deleteKey = key
		return nil
	}
	if err := kr.DeleteKey(); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if deleteApp != kr.AppName || deleteKey != kr.KeyField {
		t.Fatalf("unexpected delete call: %q %q", deleteApp, deleteKey)
	}
}

func TestKeyringSetError(t *testing.T) {
	origSet := keyringSet
	origRandRead := randRead
	defer func() {
		keyringSet = origSet
		randRead = origRandRead
	}()

	randRead = func(b []byte) (int, error) { return 0, errors.New("rand fail") }
	kr := NewKeyring()
	if _, err := kr.SetKey(); err == nil {
		t.Fatalf("expected rand error")
	}

	randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0x02
		}
		return len(b), nil
	}
	keyringSet = func(string, string, string) error { return errors.New("set fail") }
	if _, err := kr.SetKey(); err == nil {
		t.Fatalf("expected set error")
	}
}
