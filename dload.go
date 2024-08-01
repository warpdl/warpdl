package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v8"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func download(ctx *cli.Context) (err error) {
	url := ctx.Args().First()
	if url == "" {
		if ctx.Command.Name == "" {
			return help(ctx)
		}
		return printErrWithCmdHelp(
			ctx,
			errors.New("no url provided"),
		)
	} else if url == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	fmt.Println(">> Initiating a WARP download << ")
	url = strings.TrimSpace(url)
	m, err := warplib.InitManager()
	if err != nil {
		printRuntimeErr(ctx, "info", "init_manager", err)
		return nil
	}
	defer m.Close()

	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	if vInfo, er := processVideo(url); er == nil {
		if vInfo.AudioFName != "" {
			nt := time.Now()
			er = downloadVideo(getHTTPClient(), headers, m, vInfo)
			if er != nil {
				printRuntimeErr(ctx, "info", "download_video", er)
			}
			if !timeTaken {
				return nil
			}
			fmt.Printf("\nTime Taken: %s\n", time.Since(nt).String())
			return nil
		}
		url = vInfo.VideoUrl
		fileName = vInfo.VideoFName
	}
	var (
		dbar, cbar *mpb.Bar
	)
	sc := NewSpeedCounter(4350 * time.Microsecond)

	d, err := warplib.NewDownloader(
		getHTTPClient(),
		url,
		&warplib.DownloaderOpts{
			ForceParts: forceParts,
			Handlers: &warplib.Handlers{
				ErrorHandler: func(hash string, err error) {
					sc.bar.Abort(false)
					fmt.Println("Failed to continue downloading:", rectifyError(err))
					os.Exit(0)
				},
				DownloadProgressHandler: func(_ string, nread int) {
					// dbar.IncrBy(nread)
					sc.IncrBy(nread)
				},
				CompileProgressHandler: func(hash string, nread int) {
					cbar.IncrBy(nread)
				},
				DownloadCompleteHandler: func(hash string, tread int64) {
					if hash != warplib.MAIN_HASH {
						return
					}
					sc.Stop()
					// fill download bar
					if dbar.Completed() {
						return
					}
					dbar.SetCurrent(tread)
					// fill compile bar
					if cbar.Completed() {
						return
					}
					cbar.SetCurrent(tread)
				},
			},
			MaxConnections:    maxConns,
			MaxSegments:       maxParts,
			FileName:          fileName,
			DownloadDirectory: dlPath,
			Headers:           headers,
		},
	)
	if err != nil {
		printRuntimeErr(ctx, "info", "create_downloader", err)
		return nil
	}
	m.AddDownload(d, nil)

	fileName = d.GetFileName()
	if fileName == "" {
		printRuntimeErr(ctx, "info", "get_file_name", errors.New("file name cannot be empty"))
		return
	}
	txt := fmt.Sprintf(`
Download Info
Name`+"\t\t"+`: %s
Size`+"\t\t"+`: %s
Save Location`+"\t"+`: %s/
Max Connections`+"\t"+`: %d
`,
		fileName,
		d.GetContentLengthAsString(),
		d.GetDownloadDirectory(),
		maxConns,
	)
	if maxParts != 0 {
		txt += fmt.Sprintf("Max Segments\t: %d\n", maxParts)
	}
	fmt.Println(txt)

	p := mpb.New(mpb.WithWidth(64), mpb.WithRefreshRate(time.Millisecond*100))
	dbar, cbar = initBars(p, "", d.GetContentLengthAsInt())
	sc.SetBar(dbar)
	sc.Start()
	nt := time.Now()
	err = d.Start()
	if err != nil {
		return err
	}
	p.Wait()
	if !timeTaken {
		return nil
	}
	fmt.Printf("\nTime Taken: %s\n", time.Since(nt).String())
	return nil
}

func resume(ctx *cli.Context) (err error) {
	hash := ctx.Args().First()
	if hash == "" {
		if ctx.Command.Name == "" {
			return help(ctx)
		}
		return printErrWithCmdHelp(
			ctx,
			errors.New("no hash provided"),
		)
	} else if hash == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}

	fmt.Println(">> Initiating a WARP download << ")
	m, err := warplib.InitManager()
	if err != nil {
		printRuntimeErr(ctx, "info", "init_manager", err)
		return nil
	}
	defer m.Close()

	var (
		dbar, cbar *mpb.Bar
	)
	sc := NewSpeedCounter(4350 * time.Microsecond)

	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}

	client := getHTTPClient()
	var item *warplib.Item
	item, err = m.ResumeDownload(client, hash, &warplib.ResumeDownloadOpts{
		ForceParts:     forceParts,
		MaxConnections: maxConns,
		MaxSegments:    maxParts,
		Headers:        headers,
		Handlers: &warplib.Handlers{
			ErrorHandler: func(hash string, err error) {
				dbar.Abort(false)
				fmt.Println("Failed to continue downloading:", rectifyError(err))
				os.Exit(0)
			},
			ResumeProgressHandler: func(hash string, nread int) {
				dbar.IncrBy(nread)
			},
			DownloadProgressHandler: func(_ string, nread int) {
				sc.IncrBy(nread)
				// dbar.IncrBy(nread)
			},
			CompileProgressHandler: func(hash string, nread int) {
				cbar.IncrBy(nread)
			},
			DownloadCompleteHandler: func(hash string, _ int64) {
				if hash != warplib.MAIN_HASH {
					return
				}
				sc.Stop()
				// fill download bar
				if dbar.Completed() {
					return
				}
				dbar.SetCurrent(int64(item.TotalSize))
				// fill compile bar
				if cbar.Completed() {
					return
				}
				cbar.SetCurrent(int64(item.TotalSize))
			},
		},
	})
	if err != nil {
		printRuntimeErr(ctx, "resume", "primary", err)
		return nil
	}
	var (
		cItem        *warplib.Item
		sDBar, sCBar *mpb.Bar
		sc1          *SpeedCounter
	)
	if item.ChildHash != "" {
		sc1 = NewSpeedCounter(4350 * time.Microsecond)
		cItem, err = m.ResumeDownload(client, item.ChildHash, &warplib.ResumeDownloadOpts{
			ForceParts:     forceParts,
			MaxConnections: maxConns,
			MaxSegments:    maxParts,
			Headers:        headers,
			Handlers: &warplib.Handlers{
				ErrorHandler: func(hash string, err error) {
					sDBar.Abort(false)
					fmt.Println("Failed to continue downloading:", rectifyError(err))
					os.Exit(0)
				},
				ResumeProgressHandler: func(hash string, nread int) {
					sDBar.IncrBy(nread)
				},
				DownloadProgressHandler: func(hash string, nread int) {
					sc1.IncrBy(nread)
				},
				CompileProgressHandler: func(hash string, nread int) {
					sCBar.IncrBy(nread)
				},
				DownloadCompleteHandler: func(hash string, _ int64) {
					if hash != warplib.MAIN_HASH {
						return
					}
					sc1.Stop()
					// fill download bar
					if sDBar.Completed() {
						return
					}
					sDBar.SetCurrent(int64(item.TotalSize))
					// fill compile bar
					if sCBar.Completed() {
						return
					}
					sCBar.SetCurrent(int64(item.TotalSize))
				},
			},
		})
		if err != nil {
			printRuntimeErr(ctx, "resume", "secondary", err)
			return nil
		}
	}

	size := item.TotalSize
	if cItem != nil {
		size += cItem.TotalSize
	}

	txt := fmt.Sprintf(`
Download Info
Name`+"\t\t"+`: %s
Size`+"\t\t"+`: %s
Save Location`+"\t"+`: %s/
Max Connections`+"\t"+`: %d
`,
		item.Name,
		size.String(),
		func() string {
			loc := item.AbsoluteLocation
			if loc != "" {
				return loc
			}
			return item.DownloadLocation
		}(),
		maxConns,
	)
	if maxParts != 0 {
		txt += fmt.Sprintf("Max Segments\t: %d\n", maxParts)
	}
	fmt.Println(txt)

	wg := &sync.WaitGroup{}

	resumeItem := func(wg *sync.WaitGroup, i *warplib.Item, db, cb *mpb.Bar) {
		if i.Downloaded < i.TotalSize {
			err = i.Resume()
		} else {
			db.SetCurrent(int64(i.TotalSize))
			cb.SetCurrent(int64(i.TotalSize))
		}
		wg.Done()
	}
	p := mpb.New(mpb.WithWidth(64), mpb.WithRefreshRate(time.Millisecond*100))

	if cItem != nil {
		dbar, cbar = initBars(p, "Video: ", int64(item.TotalSize))
		wg.Add(1)
		go resumeItem(wg, item, dbar, cbar)

		sDBar, sCBar = initBars(p, "Audio: ", int64(cItem.TotalSize))
		wg.Add(1)
		go resumeItem(wg, cItem, sDBar, sCBar)

		sc1.SetBar(sDBar)
		sc1.Start()
	} else {
		dbar, cbar = initBars(p, "", int64(item.TotalSize))
		wg.Add(1)
		go resumeItem(wg, item, dbar, cbar)
	}
	sc.SetBar(dbar)
	sc.Start()
	wg.Wait()
	cbar.Abort(false)
	if sCBar != nil {
		sCBar.Abort(false)
	}
	p.Wait()
	if err != nil {
		printRuntimeErr(ctx, "resume", "main", err)
		err = nil
		return
	}
	if cItem == nil {
		return
	}
	compileVideo(
		item.GetSavePath(),
		cItem.GetSavePath(),
		item.Name,
		cItem.Name,
		item.AbsoluteLocation,
	)
	return
}
