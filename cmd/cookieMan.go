//go:build !linux

package cmd

import (
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/keyring"
	"github.com/warpdl/warpdl/pkg/warplib"
)

const cookieKeyEnv = "WARPDL_COOKIE_KEY"

type keyringProvider interface {
	GetKey() ([]byte, error)
	SetKey() ([]byte, error)
}

var newKeyring = func() keyringProvider { return keyring.NewKeyring() }

func getCookieManager(ctx *cli.Context) (*credman.CookieManager, error) {
	if keyHex := os.Getenv(cookieKeyEnv); keyHex != "" {
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			common.PrintRuntimeErr(ctx, "daemon", "cookie-key", err)
			return nil, err
		}
		cookieFile := filepath.Join(warplib.ConfigDir, "cookies.warp")
		cm, err := credman.NewCookieManager(cookieFile, key)
		if err != nil {
			common.PrintRuntimeErr(ctx, "daemon", "credman", err)
			return nil, err
		}
		defer cm.Close()
		return cm, nil
	}

	kr := newKeyring()
	key, err := kr.GetKey()
	if err != nil {
		key, err = kr.SetKey()
		if err != nil {
			common.PrintRuntimeErr(ctx, "daemon", "keyring", err)
			return nil, err
		}
	}

	cookieFile := filepath.Join(warplib.ConfigDir, "cookies.warp")
	cm, err := credman.NewCookieManager(cookieFile, key)
	if err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "credman", err)
		return nil, err
	}
	defer cm.Close()
	return cm, nil
}
