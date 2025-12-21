package cmd

import (
	"log"
	"net/http"
	"net/http/cookiejar"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

var (
	cookieManagerFunc = getCookieManager
	startServerFunc   = func(serv *server.Server) error { return serv.Start() }
)

func daemon(ctx *cli.Context) error {
	l := log.Default()
	cm, err := cookieManagerFunc(ctx)
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
	jar, err := cookiejar.New(nil)
	if err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "cookie_jar", err)
		return nil
	}
	client := &http.Client{
		Jar: jar,
	}
	m, err := warplib.InitManager()
	if err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "init_manager", err)
		return nil
	}
	s, err := api.NewApi(l, m, client, elEng)
	if err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "new_api", err)
		return nil
	}
	serv := server.NewServer(l, m, DEF_PORT)
	s.RegisterHandlers(serv)
	return startServerFunc(serv)
}
