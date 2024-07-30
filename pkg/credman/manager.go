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
	f        *os.File
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
	var err error
	cm.f, err = os.OpenFile(cm.filePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}

	var cookiesData []byte
	_, err = cm.f.Read(cookiesData)
	if err != nil {
		return err
	}
	if len(cookiesData) == 0 { // don't decode empty data
		return nil
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
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(cm.cookies)
	if err != nil {
		return err
	}

	_, err = cm.f.Write(buf.Bytes())
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
	defer cm.f.Close()
	return cm.saveCookies()
}
