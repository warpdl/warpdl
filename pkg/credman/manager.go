package credman

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"

	"github.com/warpdl/warpdl/pkg/credman/encryption"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

type CookieManager struct {
	filePath string
	key      []byte
	cookies  map[string]*types.Cookie
}

func NewCookieManager(filePath string, key []byte) (*CookieManager, error) {
	cm := &CookieManager{
		filePath: filePath,
		key:      key,
		cookies:  make(map[string]*types.Cookie),
	}

	err := cm.loadCookies()
	if err != nil {
		return nil, err
	}

	return cm, nil
}

func (cm *CookieManager) loadCookies() error {
	_, err := os.Stat(cm.filePath)
	if os.IsNotExist(err) {
		return nil
	}

	file, err := os.Open(cm.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var cookiesData []byte
	_, err = file.Read(cookiesData)
	if err != nil {
		return err
	}

	buf := bytes.NewBuffer(cookiesData)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&cm.cookies)
	if err != nil {
		return err
	}

	return nil
}

func (cm *CookieManager) saveCookies() error {
	file, err := os.Create(cm.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err = enc.Encode(cm.cookies)
	if err != nil {
		return err
	}

	_, err = file.Write(buf.Bytes())
	if err != nil {
		return err
	}

	return nil
}

func (cm *CookieManager) SetCookie(cookie types.Cookie) error {
	encryptedValue, err := encryption.EncryptValue(cookie.Value, cm.key)
	if err != nil {
		return err
	}
	cookie.Value = string(encryptedValue)
	cm.cookies[cookie.Name] = &cookie
	return cm.saveCookies()
}

func (cm *CookieManager) GetCookie(name string) (*types.Cookie, error) {
	cookie, ok := cm.cookies[name]
	if !ok {
		return nil, fmt.Errorf("cookie not found: %s", name)
	}

	decrpytedValue, err := encryption.DecryptValue([]byte(cookie.Value), cm.key)
	if err != nil {
		return nil, err
	}
	cookie.Value = string(decrpytedValue)
	return cookie, nil
}

func (cm *CookieManager) DeleteCookie(name string) error {
	_, ok := cm.cookies[name]
	if !ok {
		return fmt.Errorf("cookie not found: %s", name)
	}
	delete(cm.cookies, name)
	return cm.saveCookies()
}

func (cm *CookieManager) UpdateCookie(cookie *types.Cookie) error {
	encryptedValue, err := encryption.EncryptValue(cookie.Value, cm.key)
	if err != nil {
		return err
	}
	cookie.Value = string(encryptedValue)
	cm.cookies[cookie.Name] = cookie
	return cm.saveCookies()
}

func (cm *CookieManager) Close() error {
	return cm.saveCookies()
}
