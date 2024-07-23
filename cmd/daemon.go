package cmd

import (
	"log"
	"path/filepath"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/keyring"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func daemon(ctx *cli.Context) error {
	l := log.Default()

	kr := keyring.NewKeyring()
	key, err := kr.GetKey()
	if err != nil {
		key, err = kr.SetKey()
		if err != nil {
			common.PrintRuntimeErr(ctx, "daemon", "keyring", err)
			return nil
		}
	}

	cookieFile := filepath.Join(warplib.ConfigDir, "cookies.warp")
	cm, err := credman.NewCookieManager(cookieFile, key)
	if err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "credman", err)
		return nil
	}
	defer cm.Close()

	elEng, err := extl.NewEngine(l, cm, false)
	if err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "extloader_engine", err)
		return nil
	}
	defer elEng.Close()
	s, err := api.NewApi(l, elEng)
	if err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "new_api", err)
		return nil
	}
	serv := server.NewServer(l)
	s.RegisterHandlers(serv)
	return serv.Start()
}
