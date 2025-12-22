package cmd

import (
	"context"
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
	startServerFunc   = func(serv *server.Server, ctx context.Context) error { return serv.Start(ctx) }
)

func daemon(ctx *cli.Context) error {
	l := log.Default()

	// Write PID file
	if err := WritePidFile(); err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "write_pid", err)
		return nil
	}
	defer RemovePidFile()

	// Setup signal handler for graceful shutdown
	shutdownCtx, cancel := setupShutdownHandler()
	defer cancel()

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

	// Deferred cleanup on shutdown (runs in reverse order)
	defer func() {
		l.Println("Shutting down daemon...")

		// Stop all active downloads (progress is auto-persisted via UpdateItem)
		for _, item := range m.GetItems() {
			if item.IsDownloading() {
				l.Printf("Stopping download: %s", item.Hash)
				item.StopDownload()
			}
		}

		// Close API (closes manager, flushes state)
		if err := s.Close(); err != nil {
			l.Printf("Error closing API: %v", err)
		}

		// Close extension engine
		elEng.Close()

		l.Println("Daemon stopped")
	}()

	serv := server.NewServer(l, m, DEF_PORT)
	s.RegisterHandlers(serv)
	return startServerFunc(serv, shutdownCtx)
}
