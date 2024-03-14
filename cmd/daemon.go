package cmd

import (
	"log"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/server"
)

func daemon(ctx *cli.Context) error {
	s, err := api.NewApi(log.Default())
	if err != nil {
		printRuntimeErr(ctx, "daemon", "new_api", err)
	}
	serv := server.NewServer(log.Default())
	s.RegisterHandlers(serv)
	return serv.Start()
}
