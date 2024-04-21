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

func NewKeyring() *Keyring {
	return &Keyring{
		AppName:  "warpdl",
		KeyField: "main",
	}
}

func (k *Keyring) SetKey() ([]byte, error) {
	key := make([]byte, 32)
	rand.Read(key)
	keyString := hex.EncodeToString(key)
	err := keyring.Set(k.AppName, k.KeyField, keyString)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (k *Keyring) GetKey() ([]byte, error) {
	key, err := keyring.Get(k.AppName, k.KeyField)
	if err != nil {
		return nil, err
	}
	return []byte(key), nil
}

func (k *Keyring) DeleteKey() error {
	return keyring.Delete(k.AppName, k.KeyField)
}
