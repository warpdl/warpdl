package cmd

import (
	"time"

	"github.com/vbauerster/mpb/v8"
	cmdCommon "github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

var daemonURI string

// getClient creates a warpcli client, using --daemon-uri if specified.
// If daemonURI is set, it connects directly without auto-spawning the daemon.
// Otherwise, it uses the default NewClient() which spawns the daemon if needed.
func getClient() (*warpcli.Client, error) {
	if daemonURI != "" {
		return warpcli.NewClientWithURI(daemonURI)
	}
	return warpcli.NewClient()
}

func downloadStopped(client *warpcli.Client, sc *SpeedCounter) func(dr *common.DownloadingResponse) error {
	return func(dr *common.DownloadingResponse) error {
		if dr.Hash != warplib.MAIN_HASH {
			return nil
		}
		sc.bar.Abort(false)
		// fmt.Println("Download Stopped: ", dr.DownloadId)
		client.Disconnect()
		return nil
	}
}

func downloadProgress(sc *SpeedCounter) func(dr *common.DownloadingResponse) error {
	return func(dr *common.DownloadingResponse) error {
		// fmt.Println(dr.Action, dr.DownloadId, dr.Hash, dr.Value)
		sc.IncrBy(int(dr.Value))
		return nil
	}
}

// resumeProgress handles ResumeProgress updates during download resumption.
// It has the same behavior as downloadProgress - incrementing the speed counter.
func resumeProgress(sc *SpeedCounter) func(dr *common.DownloadingResponse) error {
	return func(dr *common.DownloadingResponse) error {
		sc.IncrBy(int(dr.Value))
		return nil
	}
}

func downloadComplete(client *warpcli.Client, dbar, cbar *mpb.Bar, sc *SpeedCounter) func(dr *common.DownloadingResponse) error {
	return func(dr *common.DownloadingResponse) error {
		// fmt.Println("Download Complete: ", dr.Hash)
		if dr.Hash != warplib.MAIN_HASH {
			return nil
		}
		defer client.Disconnect()
		sc.Stop()
		// fill download bar
		if dbar.Completed() {
			return nil
		}
		dbar.SetCurrent(dr.Value)
		// fill compile bar
		if cbar.Completed() {
			return nil
		}
		cbar.SetCurrent(dr.Value)
		return nil
	}
}

func compileStart(dr *common.DownloadingResponse) error {
	return nil
}

func compileComplete(dr *common.DownloadingResponse) error {
	// fmt.Println("Compile Complete: ", dr.Hash)
	return nil
}

func compileProgress(bar *mpb.Bar) func(dr *common.DownloadingResponse) error {
	return func(dr *common.DownloadingResponse) error {
		bar.IncrBy(int(dr.Value))
		return nil
	}
}

// RegisterHandlersWithProgress registers all download event handlers with
// initial progress for resume scenarios.
func RegisterHandlersWithProgress(client *warpcli.Client, contentLength int64, initialProgress int64) {
	rr := time.Millisecond * 30
	sc := NewSpeedCounter(rr)
	p := mpb.New(mpb.WithWidth(64), mpb.WithRefreshRate(rr))
	dbar, cbar := cmdCommon.InitBarsWithProgress(p, "", contentLength, initialProgress)
	sc.SetBar(dbar)
	sc.Start()
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.DownloadStopped, downloadStopped(client, sc)),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.DownloadProgress, downloadProgress(sc)),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.ResumeProgress, resumeProgress(sc)),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.DownloadComplete, downloadComplete(client, dbar, cbar, sc)),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.CompileComplete, compileComplete),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.CompileProgress, compileProgress(cbar)),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.CompileStart, compileStart),
	)
}

// RegisterHandlers maintains backward compatibility for fresh downloads.
func RegisterHandlers(client *warpcli.Client, contentLength int64) {
	RegisterHandlersWithProgress(client, contentLength, 0)
}
