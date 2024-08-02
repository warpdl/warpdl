//go:build !linux

package cmd

import (
	"path/filepath"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/keyring"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func getCookieManager(ctx *cli.Context) (*credman.CookieManager, error) {
	kr := keyring.NewKeyring()
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
