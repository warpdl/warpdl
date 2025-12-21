package keyring

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/zalando/go-keyring"
)

type Keyring struct {
	AppName  string
	KeyField string
}

var (
	keyringSet    = keyring.Set
	keyringGet    = keyring.Get
	keyringDelete = keyring.Delete
	randRead      = rand.Read
)

func NewKeyring() *Keyring {
	return &Keyring{
		AppName:  "warpdl",
		KeyField: "main",
	}
}

func (k *Keyring) SetKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := randRead(key); err != nil {
		return nil, err
	}
	keyString := hex.EncodeToString(key)
	err := keyringSet(k.AppName, k.KeyField, keyString)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (k *Keyring) GetKey() ([]byte, error) {
	key, err := keyringGet(k.AppName, k.KeyField)
	if err != nil {
		return nil, err
	}
	return []byte(key), nil
}

func (k *Keyring) DeleteKey() error {
	return keyringDelete(k.AppName, k.KeyField)
}
