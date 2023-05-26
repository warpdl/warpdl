package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/kkdai/youtube/v2"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"github.com/warpdl/warplib"
)

// ffmpeg -hide_banner -loglevel error -i video.mp4 -i audio.webm -c:v copy -map 0:v -map 1:a -y output.mp4

var barMap warplib.VMap[string, *mpb.Bar]

func newPart(p *mpb.Progress) warplib.SpawnPartHandlerFunc {
	return func(hash string, ioff, foff int64) {
		// fmt.Println("created new part with hash:", hash, "ioff:", ioff, "foff:", foff)
		name := "Process " + hash
		bar := p.New(0,
			// BarFillerBuilder with custom style
			mpb.BarStyle().Lbound("╢").Filler("█").Tip("█").Padding("░").Rbound("╟"),
			mpb.PrependDecorators(
				// display our name with one space on the right
				decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DidentRight}),
				// replace ETA decorator with "done" message, OnComplete event
				decor.OnComplete(
					decor.AverageETA(decor.ET_STYLE_GO, decor.WC{W: 4}), "Completed",
				),
			),
			mpb.AppendDecorators(decor.Percentage()),
		)
		bar.SetTotal(foff-ioff, false)
		bar.EnableTriggerComplete()
		barMap.Set(hash, bar)
	}
}

func progressHandler(hash string, nread int) {
	bar := barMap.Get(hash)
	if bar == nil {
		return
	}
	bar.IncrBy(nread)
}

func respawnHandler(p *mpb.Progress) warplib.RespawnPartHandlerFunc {
	return func(hash string, nread, ioff, foff int64) {
		// fmt.Println("reused part with hash:", hash, "ioff:", ioff, "foff:", foff)
		bar := barMap.Get(hash)
		name := "Process " + hash
		nbar := p.New(0,
			// BarFillerBuilder with custom style
			mpb.BarStyle().Lbound("╢").Filler("█").Tip("█").Padding("░").Rbound("╟"),
			mpb.BarQueueAfter(bar),
			mpb.PrependDecorators(
				// display our name with one space on the right
				decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DidentRight}),
				// replace ETA decorator with "done" message, OnComplete event
				decor.OnComplete(
					decor.AverageETA(decor.ET_STYLE_GO, decor.WC{W: 4}), "Completed",
				),
			),
			mpb.AppendDecorators(decor.Percentage()),
		)
		bar.Abort(true)
		nbar.SetTotal(foff-ioff+nread, false)
		nbar.EnableTriggerComplete()
		nbar.SetCurrent(nread)
		barMap.Set(hash, nbar)
	}
}

func findAudioByQuality(formats youtube.FormatList, ql string) *youtube.Format {
	for _, f := range formats {
		if f.QualityLabel != "" {
			continue
		}
		if f.AudioQuality == ql {
			return &f
		}
	}
	return nil
}

func main() {
	args := os.Args
	if len(args) < 2 {
		fmt.Println("specify a url")
		os.Exit(1)
	}
	url := args[1]
	if url == "version" {
		fmt.Println("warp: version 1.0.2")
		return
	}
	if (url == "yt" || url == "youtube") && len(args) > 2 {
		url = args[2]
		// url = strings.TrimPrefix(url, "http://")
		// url = strings.TrimPrefix(url, "https://")
		// url = strings.TrimPrefix(url, "youtu.be/")
		// url = strings.TrimPrefix(url, "youtube.com/")

		client := youtube.Client{}

		dlt := "video"

		if len(args) > 3 {
			dlt = args[3]
		}

		video, err := client.GetVideo(url)
		if err != nil {
			fmt.Println(err)
			return
		}
		formats := video.Formats
		//for _, x := range formats {
		//	if x.QualityLabel == "1080p" {
		//		url =
		//		break
		//	}
		//	fmt.Println("q:", x.Quality, "ql:", x.QualityLabel)
		//	fmt.Println("ac:", x.AudioChannels, "aq:", x.AudioQuality, "asr:", x.AudioSampleRate)
		//	fmt.Println("pt:", x.ProjectionType, "bt:", x.Bitrate, "abt:", x.AverageBitrate)
		//	fmt.Println()
		//}
		//return
		var format *youtube.Format
		switch dlt {
		case "audio":
			format = findAudioByQuality(formats, "AUDIO_QUALITY_HIGH")
			if format == nil {
				format = findAudioByQuality(formats, "AUDIO_QUALITY_MEDIUM")
			}
			if format == nil {
				format = findAudioByQuality(formats, "AUDIO_QUALITY_LOW")
			}
		default:
			format = formats.FindByQuality("2160p")
			if format == nil {
				format = formats.FindByQuality("1440p")
			}
			if format == nil {
				format = formats.FindByQuality("1080p")
			}
			if format == nil {
				format = formats.FindByQuality("720p")
			}
			if format == nil {
				format = formats.FindByQuality("360p")
			}
		}
		if format == nil {
			fmt.Println("fmt nil")
			return
		}
		fmt.Println("downloading at", format.QualityLabel, format.Bitrate, format.MimeType, "audio:", format.AudioQuality)
		// return
		url = format.URL
		// return
	}
	barMap.Make()
	// turl := "https://firebasestorage.googleapis.com/v0/b/skink-cdb44.appspot.com/o/skink.exe?alt=media&token=fa521a89-1a65-4fa9-a634-fee4bb4bdc71"
	// turl := "https://sample-videos.com/video123/mp4/720/big_buck_bunny_720p_30mb.mp4"

	tn := time.Now()

	d, err := warplib.NewDownloader(&http.Client{}, url, true)
	if err != nil {
		log.Fatalln(err)
	}

	d.SetMaxParts(16)
	d.SetFileName("video.mp4")
	d.SetDownloadLocation("./")

	fmt.Println("INFO:", d.GetParts(), d.GetFileName(), d.GetContentLengthAsString())

	p := mpb.New(mpb.WithWidth(64))

	d.Handlers = warplib.Handlers{
		SpawnPartHandler:   newPart(p),
		ProgressHandler:    progressHandler,
		RespawnPartHandler: respawnHandler(p),
	}

	err = d.Start()
	if err != nil {
		fmt.Println(err.Error())
	}
	p.Wait()
	fmt.Println("TIME TAKEN:", time.Since(tn))
	// "https://speed.hetzner.de/100MB.bin"
}
