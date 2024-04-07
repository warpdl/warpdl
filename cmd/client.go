package cmd

import (
	"fmt"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func downloadStopped(client *warpcli.Client) func(dr *common.DownloadingResponse) error {
	return func(dr *common.DownloadingResponse) error {
		if dr.Hash != warplib.MAIN_HASH {
			return nil
		}
		fmt.Println("Download Stopped: ", dr.DownloadId)
		client.Disconnect()
		return nil
	}
}

func downloadProgress(dr *common.DownloadingResponse) error {
	return nil
}

func downloadComplete(client *warpcli.Client) func(dr *common.DownloadingResponse) error {
	return func(dr *common.DownloadingResponse) error {
		fmt.Println("Download Complete: ", dr.Hash)
		if dr.Hash == warplib.MAIN_HASH {
			client.Disconnect()
		}
		return nil
	}
}

func compileStart(dr *common.DownloadingResponse) error {
	return nil
}

func compileComplete(dr *common.DownloadingResponse) error {
	fmt.Println("Compile Complete: ", dr.Hash)
	return nil
}

func compileProgress(dr *common.DownloadingResponse) error {
	return nil
}

func RegisterHandlers(client *warpcli.Client) {
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.DownloadStopped, downloadStopped(client)),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.DownloadProgress, downloadProgress),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.DownloadComplete, downloadComplete(client)),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.CompileComplete, compileComplete),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.CompileProgress, compileProgress),
	)
	client.AddHandler(
		common.UPDATE_DOWNLOADING,
		warpcli.NewDownloadingHandler(common.CompileStart, compileStart),
	)
}
