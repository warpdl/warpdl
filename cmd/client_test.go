package cmd

import (
	"testing"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestDownloadHandlers(t *testing.T) {
	p := mpb.New()
	dbar := p.AddBar(10)
	cbar := p.AddBar(10)
	sc := NewSpeedCounter(time.Millisecond)
	sc.SetBar(dbar)
	sc.Start()
	defer sc.Stop()

	client := &warpcli.Client{}

	if err := downloadProgress(sc)(&common.DownloadingResponse{Value: 3}); err != nil {
		t.Fatalf("downloadProgress: %v", err)
	}
	if err := compileProgress(cbar)(&common.DownloadingResponse{Value: 2}); err != nil {
		t.Fatalf("compileProgress: %v", err)
	}
	if err := downloadComplete(client, dbar, cbar, sc)(&common.DownloadingResponse{Hash: "other", Value: 5}); err != nil {
		t.Fatalf("downloadComplete non-main: %v", err)
	}
	if err := downloadComplete(client, dbar, cbar, sc)(&common.DownloadingResponse{Hash: warplib.MAIN_HASH, Value: 10}); err != nil {
		t.Fatalf("downloadComplete main: %v", err)
	}
	if err := downloadStopped(client, sc)(&common.DownloadingResponse{Hash: "other"}); err != nil {
		t.Fatalf("downloadStopped non-main: %v", err)
	}
	if err := downloadStopped(client, sc)(&common.DownloadingResponse{Hash: warplib.MAIN_HASH}); err != nil {
		t.Fatalf("downloadStopped main: %v", err)
	}
	if err := compileStart(&common.DownloadingResponse{}); err != nil {
		t.Fatalf("compileStart: %v", err)
	}
	if err := compileComplete(&common.DownloadingResponse{}); err != nil {
		t.Fatalf("compileComplete: %v", err)
	}
}
