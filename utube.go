package main

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"strings"
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
	for _, tExt := range exts {
		switch tExt {
		case ".mp4", ".3gp":
			ext = tExt
			return
		}
	}
	ext = exts[0]
	return
}

func downloadVideo(client *http.Client, m *warplib.Manager, vInfo *videoInfo) (err error) {
	var (
		vDBar, vCBar *mpb.Bar
		aDBar, aCBar *mpb.Bar
	)

	vd, er := warplib.NewDownloader(client, vInfo.VideoUrl, &warplib.DownloaderOpts{
		FileName:          vInfo.VideoFName,
		ForceParts:        forceParts,
		MaxConnections:    maxConns,
		MaxSegments:       maxParts,
		DownloadDirectory: warplib.DlDataDir,
		Handlers: &warplib.Handlers{
			ProgressHandler: func(_ string, nread int) {
				vDBar.IncrBy(nread)
			},
			CompileProgressHandler: func(nread int) {
				vCBar.IncrBy(nread)
			},
		},
	})
	if er != nil {
		err = er
		return
	}

	aConn := 3
	if maxConns != 0 && maxConns < aConn {
		aConn = maxConns
	}
	aSegments := 6
	if maxParts != 0 && maxParts < aSegments {
		aSegments = maxParts
	}

	ad, er := warplib.NewDownloader(client, vInfo.AudioUrl, &warplib.DownloaderOpts{
		FileName:          vInfo.AudioFName,
		ForceParts:        forceParts,
		MaxConnections:    aConn,
		MaxSegments:       aSegments,
		DownloadDirectory: warplib.DlDataDir,
		Handlers: &warplib.Handlers{
			ProgressHandler: func(_ string, nread int) {
				aDBar.IncrBy(nread)
			},
			CompileProgressHandler: func(nread int) {
				aCBar.IncrBy(nread)
			},
		},
	})
	if er != nil {
		err = er
		return
	}

	m.AddDownload(vd, &warplib.AddDownloadOpts{
		Child: ad,
	})
	m.AddDownload(ad, &warplib.AddDownloadOpts{
		IsHidden:   true,
		IsChildren: true,
	})

	vcl := vd.GetContentLengthAsInt()
	acl := ad.GetContentLengthAsInt()

	dlPath = strings.TrimSuffix(dlPath, "/")

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

	p := mpb.New(mpb.WithWidth(64))
	vDBar, vCBar = initBars(p, "Video: ", vd.GetContentLengthAsInt())
	aDBar, aCBar = initBars(p, "Audio: ", ad.GetContentLengthAsInt())

	wg := sync.WaitGroup{}
	wg.Add(2)

	dl := func(d *warplib.Downloader) {
		err = d.Start()
		wg.Done()
	}

	go dl(vd)
	go dl(ad)

	wg.Wait()
	p.Wait()

	fmt.Println("\nMixing video and audio...")

	err = mux(
		ad.GetSavePath(),
		vd.GetSavePath(),
		warplib.GetPath(dlPath, vInfo.VideoFName),
	)

	if err == nil {
		os.Remove(vd.GetSavePath())
		os.Remove(ad.GetSavePath())
		fmt.Println("Download Complete!")
		return
	}
	fmt.Println("warp:", err)
	fmt.Println("Saving video and audio separately...")

	os.Rename(vd.GetSavePath(), warplib.GetPath(dlPath, vInfo.VideoFName))
	os.Rename(ad.GetSavePath(), warplib.GetPath(dlPath, vInfo.AudioFName))
	return
}

func init() {
	mime.AddExtensionType(".opus", `audio/webm; codecs="opus"`)
	mime.AddExtensionType(".mkv", `video/webm; codecs="vp9"`)
}
