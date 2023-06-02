package main

import (
	"fmt"
	"mime"
	"time"

	"github.com/kkdai/youtube/v2"
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
	Title    string
	Duration time.Duration
}

type formatInfo struct {
	Quality  string
	Url      string
	Mime     string
	Size     warplib.ContentLength
	HasAudio bool
}

func processVideo(url string) (vurl, aurl string, err error) {
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
	info := &videoInfo{
		Title:    vid.Title,
		Duration: vid.Duration,
	}
	fs := make([]formatInfo, 0)
	audio := make(map[string]string)
	tmp := make(map[string]int)
	n := len(vid.Formats)
	for i := 0; i < n; i++ {
		format := vid.Formats[n-i-1]
		q := format.QualityLabel
		if q == "" {
			if format.AudioQuality != "" {
				audio[format.AudioQuality] = format.URL
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
	q, vurl, hasAudio, err = ytDialog(info, fs)
	if hasAudio {
		return
	}
	switch q {
	case "144p", "240p", "360p", "480p":
		audUrl, ok := audio[AUDIO_QUALITY_LOW]
		if !ok {
			audUrl = audio[AUDIO_QUALITY_MEDIUM]
		}
		aurl = audUrl
	default:
		audUrl, ok := audio[AUDIO_QUALITY_MEDIUM]
		if !ok {
			audUrl = audio[AUDIO_QUALITY_LOW]
		}
		aurl = audUrl
	}
	return
}

func ytDialog(info *videoInfo, fs []formatInfo) (q, url string, hasAudio bool, err error) {
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
	q = f.Quality
	url = f.Url
	if fileName != "" {
		return
	}
	fileName = info.Title
	ext, er := filterMime(f.Mime)
	if er != nil {
		err = er
		return
	}
	fileName += ext
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

func init() {
	mime.AddExtensionType(".opus", `audio/webm; codecs="opus"`)
	mime.AddExtensionType(".mkv", `video/webm; codecs="vp9"`)
}
