package cmd

import (
	"log"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/extloader"
	"github.com/warpdl/warpdl/internal/server"
)

// s, err := api.NewApi(l)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &Daemon{
// 		l:   l,
// 		api: s,
// 	}, nil
// 	serv := server.NewServer(d.l)
// 	d.api.RegisterHandlers(serv)
// 	return serv.Start()

func daemon(ctx *cli.Context) error {
	l := log.Default()
	elEng, err := extloader.NewEngine(l)
	if err != nil {
		printRuntimeErr(ctx, "daemon", "extloader_engine", err)
	}
	defer elEng.Close()
	s, err := api.NewApi(l, elEng)
	if err != nil {
		printRuntimeErr(ctx, "daemon", "new_api", err)
	}
	serv := server.NewServer(l)
	s.RegisterHandlers(serv)
	return serv.Start()
}
