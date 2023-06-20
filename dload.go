package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v8"
	"github.com/warpdl/warplib"
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
			er = downloadVideo(&http.Client{}, headers, m, vInfo)
			if er != nil {
				printRuntimeErr(ctx, "info", "download_video", err)
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

	d, err := warplib.NewDownloader(
		&http.Client{},
		url,
		&warplib.DownloaderOpts{
			ForceParts: forceParts,
			Handlers: &warplib.Handlers{
				DownloadProgressHandler: func(_ string, nread int) {
					dbar.IncrBy(nread)
				},
				CompileProgressHandler: func(hash string, nread int) {
					cbar.IncrBy(nread)
				},
				DownloadCompleteHandler: func(hash string, tread int64) {
					if hash != warplib.MAIN_HASH {
						return
					}
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

	p := mpb.New(mpb.WithWidth(64))
	dbar, cbar = initBars(p, "", d.GetContentLengthAsInt())

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

	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}

	client := &http.Client{}
	var item *warplib.Item
	item, err = m.ResumeDownload(client, hash, &warplib.ResumeDownloadOpts{
		ForceParts:     forceParts,
		MaxConnections: maxConns,
		MaxSegments:    maxParts,
		Headers:        headers,
		Handlers: &warplib.Handlers{
			ResumeProgressHandler: func(hash string, nread int) {
				dbar.IncrBy(nread)
			},
			DownloadProgressHandler: func(_ string, nread int) {
				dbar.IncrBy(nread)
			},
			CompileProgressHandler: func(hash string, nread int) {
				cbar.IncrBy(nread)
			},
			DownloadCompleteHandler: func(hash string, _ int64) {
				if hash != warplib.MAIN_HASH {
					return
				}
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
	)
	if item.ChildHash != "" {
		cItem, err = m.ResumeDownload(client, item.ChildHash, &warplib.ResumeDownloadOpts{
			ForceParts:     forceParts,
			MaxConnections: maxConns,
			MaxSegments:    maxParts,
			Headers:        headers,
			Handlers: &warplib.Handlers{
				ResumeProgressHandler: func(hash string, nread int) {
					sDBar.IncrBy(nread)
				},
				DownloadProgressHandler: func(hash string, nread int) {
					sDBar.IncrBy(nread)
				},
				CompileProgressHandler: func(hash string, nread int) {
					sCBar.IncrBy(nread)
				},
				DownloadCompleteHandler: func(hash string, _ int64) {
					if hash != warplib.MAIN_HASH {
						return
					}
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
	p := mpb.New(mpb.WithWidth(64))

	if cItem != nil {
		dbar, cbar = initBars(p, "Video: ", int64(item.TotalSize))
		wg.Add(1)
		go resumeItem(wg, item, dbar, cbar)

		sDBar, sCBar = initBars(p, "Audio: ", int64(cItem.TotalSize))
		wg.Add(1)
		go resumeItem(wg, cItem, sDBar, sCBar)
	} else {
		dbar, cbar = initBars(p, "", int64(item.TotalSize))
		wg.Add(1)
		go resumeItem(wg, item, dbar, cbar)
	}
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
