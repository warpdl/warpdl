package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	youtube "github.com/kkdai/youtube/v2"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// muxerFn returns the mux step implementation: the rs.muxer field when set
// (tests inject a stub to verify orchestration without invoking ffmpeg),
// muxFiles otherwise. Instance fields are used instead of package globals so
// test stubs need no global write-back, which would race with the adaptive
// goroutine's reads.
func (rs *RPCServer) muxerFn() func(ctx context.Context, videoIn, audioIn, out string) error {
	if rs.muxer != nil {
		return rs.muxer
	}
	return muxFiles
}

// legDownloaderFn returns the per-leg download implementation: the
// rs.legDownloader field when set (tests inject a stub that produces a fake
// file without touching warplib or the manager), defaultDownloadLeg
// otherwise. A leg downloads one stream of an adaptive YouTube video,
// registered with the warplib Manager so it appears in `warp list`, status,
// and pause/resume; it blocks until the leg completes (success or error).
// progressFn (if non-nil) is called with cumulative bytes per progress tick.
func (rs *RPCServer) legDownloaderFn() func(rs *RPCServer, streamURL, outPath string, connections int32, progressFn func(int64)) error {
	if rs.legDownloader != nil {
		return rs.legDownloader
	}
	return defaultDownloadLeg
}

func defaultDownloadLeg(rs *RPCServer, streamURL, outPath string, connections int32, progressFn func(int64)) error {
	dir := filepath.Dir(outPath)
	name := filepath.Base(outPath)

	done := make(chan error, 1)
	var seen int64

	opts := &warplib.DownloaderOpts{
		FileName:          name,
		DownloadDirectory: dir,
		MaxConnections:    connections,
		Handlers: &warplib.Handlers{
			DownloadProgressHandler: func(_ string, n int) {
				cum := atomic.AddInt64(&seen, int64(n))
				if progressFn != nil {
					progressFn(cum)
				}
			},
			DownloadCompleteHandler: func(_ string, _ int64) {
				select {
				case done <- nil:
				default:
				}
			},
			ErrorHandler: func(_ string, err error) {
				select {
				case done <- err:
				default:
				}
			},
		},
	}

	d, err := warplib.NewDownloader(rs.client, streamURL, opts)
	if err != nil {
		return err
	}
	if err := rs.manager.AddDownload(d, &warplib.AddDownloadOpts{
		AbsoluteLocation: dir,
	}); err != nil {
		return err
	}
	if rs.pool != nil {
		rs.pool.AddDownload(d.GetHash(), nil)
	}

	// Start returns synchronously for early failures (file open, disk
	// space) without invoking ErrorHandler; routing it into done both
	// surfaces the error and prevents this function from blocking forever.
	go func() {
		if err := d.Start(); err != nil {
			select {
			case done <- err:
			default:
			}
		}
	}()

	return <-done
}

// youtubeDownload is the handler for the JSON-RPC method "youtube.download".
//
// Two modes:
//   - Progressive (AudioFormatID empty) — kicks off a single warplib download
//     for the (already audio+video bundled) stream and returns the warplib GID.
//   - Adaptive (AudioFormatID set) — generates a synthetic parent GID,
//     downloads video and audio legs to a tmp dir in parallel, then runs
//     ffmpeg to remux them into the final container. Progress and completion
//     notifications use the parent GID.
func (rs *RPCServer) youtubeDownload(ctx context.Context, p *common.YouTubeDownloadParams) (*common.YouTubeDownloadResult, error) {
	if p == nil {
		return nil, rpcErrPublic(codeInvalidParams, "missing params")
	}
	if strings.TrimSpace(p.VideoID) == "" {
		return nil, rpcErrPublic(codeInvalidParams, "missing required param: videoId")
	}
	if strings.TrimSpace(p.VideoFormatID) == "" {
		return nil, rpcErrPublic(codeInvalidParams, "missing required param: videoFormatId")
	}

	connections := p.Connections
	if connections <= 0 {
		connections = 24
	}

	fetcher := ytClientFactory()
	video, err := fetcher.GetVideoContext(ctx, "https://www.youtube.com/watch?v="+p.VideoID)
	if err != nil {
		return nil, rpcErrWrap(codeResolverFailed, err)
	}

	videoFormat, err := findFormatByItag(video, p.VideoFormatID)
	if err != nil {
		return nil, err
	}

	if p.AudioFormatID == "" {
		return rs.startProgressive(ctx, fetcher, video, videoFormat, p, connections)
	}

	audioFormat, err := findFormatByItag(video, p.AudioFormatID)
	if err != nil {
		return nil, err
	}
	mainV, _ := splitMimeType(videoFormat.MimeType)
	mainA, _ := splitMimeType(audioFormat.MimeType)
	if !strings.HasPrefix(mainV, "video/") {
		return nil, rpcErrPublic(codeFormatMismatch, "videoFormatId does not refer to a video stream")
	}
	if !strings.HasPrefix(mainA, "audio/") {
		return nil, rpcErrPublic(codeFormatMismatch, "audioFormatId does not refer to an audio stream")
	}

	if !muxAvailable() {
		return nil, rpcErrPublic(codeMuxerUnavailable,
			"ffmpeg not found on PATH; install ffmpeg to download adaptive (HD) YouTube formats")
	}

	return rs.startAdaptive(ctx, fetcher, video, videoFormat, audioFormat, p, connections)
}

// startProgressive kicks off a single-stream download (itag 18, 22, etc.)
// using the existing manager pipeline so it benefits from the same status,
// pause/resume, and progress notifications as any other download.
func (rs *RPCServer) startProgressive(ctx context.Context, fetcher ytFetcher, video *youtube.Video, format *youtube.Format, p *common.YouTubeDownloadParams, connections int32) (*common.YouTubeDownloadResult, error) {
	streamURL, err := fetcher.GetStreamURLContext(ctx, video, format)
	if err != nil {
		return nil, rpcErrWrap(codeResolverFailed, err)
	}

	mainMime, _ := splitMimeType(format.MimeType)
	ext := extFromMime(mainMime, format.MimeType)
	fileName := outputFileName(p.FileName, video.Title, ext)
	dir := p.Dir

	opts := &warplib.DownloaderOpts{
		FileName:          fileName,
		DownloadDirectory: dir,
		MaxConnections:    connections,
		Handlers:          rs.notifierHandlers(),
	}
	d, err := warplib.NewDownloader(rs.client, streamURL, opts)
	if err != nil {
		return nil, rpcErrWrap(codeInvalidParams, err)
	}
	if err := rs.manager.AddDownload(d, &warplib.AddDownloadOpts{
		AbsoluteLocation: d.GetDownloadDirectory(),
	}); err != nil {
		return nil, rpcErrWrap(codeInvalidParams, err)
	}
	hash := d.GetHash()
	if rs.pool != nil {
		rs.pool.AddDownload(hash, nil)
	}
	if rs.notifier != nil {
		rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
			GID:         hash,
			FileName:    d.GetFileName(),
			TotalLength: d.GetContentLengthAsInt(),
		})
	}
	// Early Start failures (file open, disk space) return synchronously
	// without invoking ErrorHandler; broadcast them so clients are not
	// left waiting on a download that never began.
	go func() {
		if err := d.Start(); err != nil {
			rs.broadcastError(hash, "download start failed: "+err.Error())
		}
	}()

	return &common.YouTubeDownloadResult{
		GID:      hash,
		Muxed:    false,
		FileName: d.GetFileName(),
	}, nil
}

// startAdaptive coordinates the video+audio download and ffmpeg remux.
// Returns a synthetic parent GID immediately; the actual work runs in a
// goroutine and reports progress / completion via the existing notifier.
func (rs *RPCServer) startAdaptive(ctx context.Context, fetcher ytFetcher, video *youtube.Video, vFmt, aFmt *youtube.Format, p *common.YouTubeDownloadParams, connections int32) (*common.YouTubeDownloadResult, error) {
	videoURL, err := fetcher.GetStreamURLContext(ctx, video, vFmt)
	if err != nil {
		return nil, rpcErrWrap(codeResolverFailed, fmt.Errorf("video URL resolution failed: %w", err))
	}
	audioURL, err := fetcher.GetStreamURLContext(ctx, video, aFmt)
	if err != nil {
		return nil, rpcErrWrap(codeResolverFailed, fmt.Errorf("audio URL resolution failed: %w", err))
	}

	gid, err := genGID()
	if err != nil {
		return nil, rpcErrWrap(codeResolverFailed, fmt.Errorf("gid: %w", err))
	}

	mainV, vCodecList := splitMimeType(vFmt.MimeType)
	mainA, aCodecList := splitMimeType(aFmt.MimeType)
	vCodec := first(vCodecList)
	aCodec := first(aCodecList)

	container := pickContainer(vCodec, aCodec)
	finalExt := container

	dir := p.Dir
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, rpcErrWrap(codeResolverFailed, fmt.Errorf("mkdir output: %w", err))
	}

	tmpDir, err := os.MkdirTemp(dir, ".warpdl-mux-*")
	if err != nil {
		return nil, rpcErrWrap(codeResolverFailed, fmt.Errorf("tmpdir: %w", err))
	}

	videoExt := extFromMime(mainV, vFmt.MimeType)
	audioExt := extFromMime(mainA, aFmt.MimeType)
	videoTmp := filepath.Join(tmpDir, "video."+videoExt)
	audioTmp := filepath.Join(tmpDir, "audio."+audioExt)
	finalName := outputFileName(p.FileName, video.Title, finalExt)
	finalPath := filepath.Join(dir, finalName)

	// Combined expected size for progress reporting.
	totalLen := vFmt.ContentLength + aFmt.ContentLength

	if rs.manager != nil {
		item := &warplib.Item{
			Hash:             gid,
			Name:             finalName,
			Url:              "https://www.youtube.com/watch?v=" + p.VideoID,
			TotalSize:        warplib.ContentLength(totalLen),
			DownloadLocation: dir,
			AbsoluteLocation: dir,
			Resumable:        true,
			Parts:            make(map[int64]*warplib.ItemPart),
		}
		rs.manager.UpdateItem(item)
	}
	if rs.pool != nil {
		rs.pool.AddDownload(gid, nil)
	}

	if rs.notifier != nil {
		rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
			GID:         gid,
			FileName:    finalName,
			TotalLength: totalLen,
		})
	}

	go rs.runAdaptive(&adaptiveJob{
		gid:         gid,
		videoURL:    videoURL,
		audioURL:    audioURL,
		videoTmp:    videoTmp,
		audioTmp:    audioTmp,
		finalPath:   finalPath,
		tmpDir:      tmpDir,
		connections: connections,
	})

	return &common.YouTubeDownloadResult{
		GID:      gid,
		Muxed:    true,
		FileName: finalName,
	}, nil
}

type adaptiveJob struct {
	gid         string
	videoURL    string
	audioURL    string
	videoTmp    string
	audioTmp    string
	finalPath   string
	tmpDir      string
	connections int32
}

// runAdaptive is the goroutine body for the adaptive download path.
// It downloads both legs in parallel, runs ffmpeg, and emits notifications.
func (rs *RPCServer) runAdaptive(job *adaptiveJob) {
	defer func() {
		if err := os.RemoveAll(job.tmpDir); err != nil {
			rs.logf("youtube: failed to clean up mux tmp dir %s: %v", job.tmpDir, err)
		}
	}()

	var (
		wg        sync.WaitGroup
		videoErr  error
		audioErr  error
		videoSeen int64
		audioSeen int64
	)

	progress := func(which *int64) func(int64) {
		return func(cumulative int64) {
			atomic.StoreInt64(which, cumulative)
			if rs.notifier == nil {
				return
			}
			total := atomic.LoadInt64(&videoSeen) + atomic.LoadInt64(&audioSeen)
			rs.notifier.Broadcast("download.progress", &DownloadProgressNotification{
				GID:             job.gid,
				CompletedLength: total,
			})
		}
	}

	leg := rs.legDownloaderFn()
	wg.Add(2)
	go func() {
		defer wg.Done()
		videoErr = leg(rs, job.videoURL, job.videoTmp, job.connections, progress(&videoSeen))
	}()
	go func() {
		defer wg.Done()
		audioErr = leg(rs, job.audioURL, job.audioTmp, job.connections, progress(&audioSeen))
	}()
	wg.Wait()

	if err := errors.Join(videoErr, audioErr); err != nil {
		rs.broadcastError(job.gid, "download leg failed: "+err.Error())
		return
	}

	if err := rs.muxerFn()(context.Background(), job.videoTmp, job.audioTmp, job.finalPath); err != nil {
		rs.broadcastError(job.gid, err.Error())
		return
	}

	if rs.notifier != nil {
		fi, statErr := os.Stat(job.finalPath)
		var size int64
		if statErr == nil {
			size = fi.Size()
		}
		rs.notifier.Broadcast("download.complete", &DownloadCompleteNotification{
			GID:         job.gid,
			TotalLength: size,
		})
	}
}

func (rs *RPCServer) broadcastError(gid, msg string) {
	if rs.notifier == nil {
		return
	}
	rs.notifier.Broadcast("download.error", &DownloadErrorNotification{
		GID:   gid,
		Error: msg,
	})
}

// notifierHandlers returns a Handlers struct that re-broadcasts events to
// the rpc notifier. Mirrors the pattern in downloadAdd.
func (rs *RPCServer) notifierHandlers() *warplib.Handlers {
	if rs.notifier == nil {
		return nil
	}
	return &warplib.Handlers{
		ErrorHandler: func(hash string, err error) {
			rs.notifier.Broadcast("download.error", &DownloadErrorNotification{GID: hash, Error: err.Error()})
		},
		DownloadProgressHandler: func(hash string, nread int) {
			rs.notifier.Broadcast("download.progress", &DownloadProgressNotification{GID: hash, CompletedLength: int64(nread)})
		},
		DownloadCompleteHandler: func(hash string, tread int64) {
			rs.notifier.Broadcast("download.complete", &DownloadCompleteNotification{GID: hash, TotalLength: tread})
		},
	}
}

// findFormatByItag looks up a kkdai Format by itag (string-encoded int).
// Returns an invalid_params RPC error if the itag does not parse, or
// format_not_found if the format is not present in the video.
func findFormatByItag(v *youtube.Video, itag string) (*youtube.Format, error) {
	n, err := strconv.Atoi(itag)
	if err != nil {
		return nil, rpcErrPublic(codeInvalidParams, "invalid format id (must be an integer itag): "+itag)
	}
	for i := range v.Formats {
		if v.Formats[i].ItagNo == n {
			return &v.Formats[i], nil
		}
	}
	return nil, rpcErrPublic(codeFormatNotFound, "format id not found: "+itag)
}

// outputFileName produces a sanitized "<base>.<ext>" filename.
// If userBase is non-empty it is used; otherwise videoTitle is sanitized.
func outputFileName(userBase, videoTitle, ext string) string {
	base := strings.TrimSpace(userBase)
	if base == "" {
		base = videoTitle
	}
	base = sanitizeOutputName(base)
	if base == "" {
		base = "video"
	}
	if ext == "" {
		return base
	}
	if strings.HasSuffix(strings.ToLower(base), "."+strings.ToLower(ext)) {
		return base
	}
	return base + "." + ext
}

// sanitizeOutputName removes path-unsafe characters from a filename.
func sanitizeOutputName(s string) string {
	const unsafe = `<>:"/\|?*`
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 0x20 {
			continue
		}
		if strings.ContainsRune(unsafe, r) {
			out = append(out, '_')
			continue
		}
		out = append(out, r)
	}
	res := strings.TrimSpace(string(out))
	if len(res) > 200 {
		res = res[:200]
	}
	return res
}

// genGID returns a 16-byte random hex string for synthetic parent GIDs.
func genGID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
