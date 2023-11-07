package main

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kkdai/youtube/v2"
	"github.com/vbauerster/mpb/v8"
	"github.com/warpdl/warplib"
)

const (
	AUDIO_QUALITY_LOW    = "AUDIO_QUALITY_LOW"
	AUDIO_QUALITY_MEDIUM = "AUDIO_QUALITY_MEDIUM"
)

// func isYoutubeVideo(url string) bool {
// 	url1, err := youtube.ExtractVideoID(url)
// 	return err == nil && url1 != url
// }

type videoInfo struct {
	Title                string
	Duration             time.Duration
	VideoFName, VideoUrl string
	AudioFName, AudioUrl string
}

type formatInfo struct {
	Quality  string
	Url      string
	Mime     string
	Size     warplib.ContentLength
	HasAudio bool
}

func processVideo(url string) (info *videoInfo, err error) {
	_, er := youtube.ExtractVideoID(url)
	if er != nil {
		err = er
		return
	}
	client := youtube.Client{}
	vid, er := client.GetVideo(url)
	if er != nil {
		err = er
		return
	}
	info = &videoInfo{
		Title:    vid.Title,
		Duration: vid.Duration,
	}
	fs := make([]formatInfo, 0)
	audio := make(map[string]youtube.Format)
	tmp := make(map[string]int)
	n := len(vid.Formats)
	for i := 0; i < n; i++ {
		format := vid.Formats[n-i-1]
		q := format.QualityLabel
		if q == "" {
			if format.AudioQuality != "" {
				audio[format.AudioQuality] = format
			}
			continue
		}
		_, ok := tmp[q]
		if ok {
			continue
		}
		fs = append(fs, formatInfo{
			Quality:  q,
			Url:      format.URL,
			Mime:     format.MimeType,
			Size:     warplib.ContentLength(format.ContentLength),
			HasAudio: format.AudioChannels > 0,
		})
		tmp[q] = 0
	}
	tmp = nil
	var (
		q        string
		hasAudio bool
	)
	q, info.VideoFName, info.VideoUrl, hasAudio, err = ytDialog(info, fs)
	if hasAudio {
		return
	}
	var (
		aud youtube.Format
		ok  bool
	)
	switch q {
	case "144p", "240p", "360p", "480p":
		aud, ok = audio[AUDIO_QUALITY_LOW]
		if !ok {
			aud = audio[AUDIO_QUALITY_MEDIUM]
		}
	default:
		aud, ok = audio[AUDIO_QUALITY_MEDIUM]
		if !ok {
			aud = audio[AUDIO_QUALITY_LOW]
		}
	}
	info.AudioUrl = aud.URL
	ext, er := filterMime(aud.MimeType)
	if er != nil {
		err = er
		return
	}
	info.AudioFName = info.Title + ext
	return
}

func ytDialog(info *videoInfo, fs []formatInfo) (q, fName, url string, hasAudio bool, err error) {
	fmt.Printf(`
Youtube Video Info
Title`+"\t\t"+`: %s
Duration`+"\t"+`: %s

Please choose a video quality from following:
`, info.Title, info.Duration.String())
	for i, q := range fs {
		fmt.Printf("[%d] %s (%s)\n", i+1, q.Quality, q.Size.String())
	}

	fmt.Print("\nPlease enter the index number of chosen quality: ")
	var n int
	_, er := fmt.Scanf("%d", &n)
	if er != nil {
		err = er
		return
	}
	if n < 1 {
		n = 1
	}
	if n > len(fs) {
		n = len(fs)
	}
	n--
	f := fs[n]
	hasAudio = f.HasAudio
	q = f.Quality
	url = f.Url
	fName = info.Title
	ext, er := filterMime(f.Mime)
	if er != nil {
		err = er
		return
	}
	fName += ext
	return
}

func filterMime(mimeT string) (ext string, err error) {
	exts, er := mime.ExtensionsByType(mimeT)
	if er != nil {
		err = er
		return
	}
	if len(exts) == 0 {
		err = fmt.Errorf("no extension found for mimetype: %s", mimeT)
		return
	}
	for _, tExt := range exts {
		switch tExt {
		case ".mp4", ".3gp", ".mkv", ".opus":
			ext = tExt
			return
		}
	}
	ext = exts[0]
	return
}

func downloadVideo(client *http.Client, headers warplib.Headers, m *warplib.Manager, vInfo *videoInfo) (err error) {
	dlPath, err = filepath.Abs(
		dlPath,
	)
	if err != nil {
		return
	}

	var (
		vDBar, vCBar *mpb.Bar
		aDBar, aCBar *mpb.Bar
	)

	sc := NewSpeedCounter(4350 * time.Microsecond)
	vd, er := warplib.NewDownloader(client, vInfo.VideoUrl, &warplib.DownloaderOpts{
		FileName:          vInfo.VideoFName + ".wtemp",
		ForceParts:        forceParts,
		MaxConnections:    maxConns,
		MaxSegments:       maxParts,
		DownloadDirectory: dlPath,
		Headers:           headers,
		Handlers: &warplib.Handlers{
			ErrorHandler: func(hash string, err error) {
				sc.bar.Abort(false)
				fmt.Println("Failed to continue downloading:", rectifyError(err))
				os.Exit(0)
			},
			DownloadProgressHandler: func(_ string, nread int) {
				sc.IncrBy(nread)
			},
			CompileProgressHandler: func(hash string, nread int) {
				vCBar.IncrBy(nread)
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				if hash != warplib.MAIN_HASH {
					return
				}
				sc.Stop()
				// fill download bar
				if vDBar.Completed() {
					return
				}
				vDBar.SetCurrent(tread)
				// fill compile bar
				if vCBar.Completed() {
					return
				}
				vCBar.SetCurrent(tread)
			},
		},
	})
	if er != nil {
		err = er
		return
	}

	sc1 := NewSpeedCounter(4350 * time.Microsecond)
	ad, er := warplib.NewDownloader(client, vInfo.AudioUrl, &warplib.DownloaderOpts{
		FileName:          vInfo.AudioFName + ".wtemp",
		ForceParts:        forceParts,
		MaxConnections:    maxConns,
		MaxSegments:       maxParts,
		DownloadDirectory: dlPath,
		Headers:           headers,
		Handlers: &warplib.Handlers{
			ErrorHandler: func(hash string, err error) {
				sc1.bar.Abort(false)
				fmt.Println("Failed to continue downloading:", rectifyError(err))
				os.Exit(0)
			},
			DownloadProgressHandler: func(_ string, nread int) {
				sc1.IncrBy(nread)
			},
			CompileProgressHandler: func(hash string, nread int) {
				aCBar.IncrBy(nread)
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				if hash != warplib.MAIN_HASH {
					return
				}
				sc1.Stop()
				// fill download bar
				if aDBar.Completed() {
					return
				}
				aDBar.SetCurrent(tread)
				// fill compile bar
				if aCBar.Completed() {
					return
				}
				aCBar.SetCurrent(tread)
			},
		},
	})
	if er != nil {
		err = er
		return
	}

	m.AddDownload(vd, &warplib.AddDownloadOpts{
		Child:            ad,
		AbsoluteLocation: dlPath,
	})
	m.AddDownload(ad, &warplib.AddDownloadOpts{
		IsHidden:   true,
		IsChildren: true,
	})

	vcl := vd.GetContentLengthAsInt()
	acl := ad.GetContentLengthAsInt()

	if fileName == "" {
		fileName = vInfo.VideoFName
	}
	txt := fmt.Sprintf(`
Download Info
Name`+"\t\t"+`: %s
Size`+"\t\t"+`: %s
Save Location`+"\t"+`: %s/
Max Connections`+"\t"+`: %d
`,
		fileName,
		warplib.ContentLength(vcl+acl).String(),
		dlPath,
		maxConns,
	)
	if maxParts != 0 {
		txt += fmt.Sprintf("Max Segments\t: %d\n", maxParts)
	}
	fmt.Println(txt)
	fmt.Println(vd)

	p := mpb.New(mpb.WithWidth(64), mpb.WithRefreshRate(time.Millisecond*100))
	vDBar, vCBar = initBars(p, "Video: ", vd.GetContentLengthAsInt())
	aDBar, aCBar = initBars(p, "Audio: ", ad.GetContentLengthAsInt())

	sc.SetBar(vDBar)
	sc1.SetBar(aDBar)
	wg := sync.WaitGroup{}
	wg.Add(2)

	dl := func(d *warplib.Downloader) {
		err = d.Start()
		wg.Done()
	}

	go dl(vd)
	go dl(ad)

	sc.Start()
	sc1.Start()

	wg.Wait()
	p.Wait()

	compileVideo(
		vd.GetSavePath(),
		ad.GetSavePath(),
		vInfo.VideoFName,
		vInfo.AudioFName,
		dlPath,
	)
	return
}

func compileVideo(vPath, aPath, vName, aName, absolPath string) {
	fmt.Println("\nMixing video and audio...")

	err := mux(
		aPath,
		vPath,
		warplib.GetPath(absolPath, vName),
	)

	if err == nil {
		os.Remove(vPath)
		os.Remove(aPath)
		fmt.Println("Download Complete!")
		return
	}
	fmt.Println("warp:", err)
	fmt.Println("Saving video and audio separately...")

	os.Rename(vPath, warplib.GetPath(absolPath, vName))
	os.Rename(aPath, warplib.GetPath(absolPath, aName))
}

func init() {
	mime.AddExtensionType(".opus", `audio/webm; codecs="opus"`)
	mime.AddExtensionType(".mkv", `video/webm; codecs="vp9"`)
}
