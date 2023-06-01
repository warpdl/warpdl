package main

import (
	"fmt"
	"mime"
	"time"

	"github.com/kkdai/youtube/v2"
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
	Quality string
	Url     string
	Mime    string
}

func processVideo(url string) (durl string, err error) {
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
	tmp := make(map[string]int)
	n := len(vid.Formats)
	for i := 0; i < n; i++ {
		format := vid.Formats[n-i-1]
		q := format.QualityLabel
		if q == "" {
			continue
		}
		_, ok := tmp[q]
		if ok {
			continue
		}
		fs = append(fs, formatInfo{
			Quality: q,
			Url:     format.URL,
			Mime:    format.MimeType,
		})
		tmp[q] = 0
	}
	tmp = nil
	durl, err = ytDialog(info, fs)
	return
}

func ytDialog(info *videoInfo, fs []formatInfo) (url string, err error) {
	fmt.Printf(`
Youtube Video Info
Title`+"\t\t"+`: %s
Duration`+"\t"+`: %s

Please choose a video quality from following:
`, info.Title, info.Duration.String())
	for i, q := range fs {
		fmt.Printf("[%d] %s\n", i+1, q.Quality)
	}

	fmt.Print("\nPlease enter the index number of chosen quality: ")
	var n int
	fmt.Scan(&n)
	if n < 1 {
		n = 1
	}
	if n > len(fs) {
		n = len(fs)
	}
	n--
	f := fs[n]
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
