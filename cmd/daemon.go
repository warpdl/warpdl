package cmd

import (
	"log"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
)

func daemon(ctx *cli.Context) error {
	l := log.Default()
	cm, err := getCookieManager(ctx)
	if err != nil {
		// nil because err has already been handled in getCookieManager function
		return nil
	}
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
