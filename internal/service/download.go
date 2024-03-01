package service

import (
	"encoding/json"

	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type DownloadMessage struct {
	Url               string          `json:"url"`
	DownloadDirectory string          `json:"download_directory"`
	FileName          string          `json:"file_name"`
	Headers           warplib.Headers `json:"headers,omitempty"`
	ForceParts        bool            `json:"force_parts,omitempty"`
	MaxConnections    int             `json:"max_connections,omitempty"`
	MaxSegments       int             `json:"max_segments,omitempty"`
	ChildHash         string          `json:"child_hash,omitempty"`
	IsHidden          bool            `json:"is_hidden,omitempty"`
	IsChildren        bool            `json:"is_children,omitempty"`
}

const UPDATE_NEW_DOWNLOAD = "new_download"

type NewDownloadResponse struct {
	Uid               string                `json:"uid"`
	FileName          string                `json:"file_name"`
	SavePath          string                `json:"save_path"`
	DownloadDirectory string                `json:"download_directory"`
	ContentLength     warplib.ContentLength `json:"content_length"`
}

const UPDATE_DOWNLOADING = "downloading"

type DownloadingResponse struct {
	Action string `json:"action"`
	Hash   string `json:"hash"`
	Value  int64  `json:"value,omitempty"`
}

func (s *Service) downloadHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m DownloadMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return UPDATE_NEW_DOWNLOAD, nil, err
	}
	var (
		d   *warplib.Downloader
		err error
	)
	d, err = warplib.NewDownloader(s.client, m.Url, &warplib.DownloaderOpts{
		Headers:           m.Headers,
		ForceParts:        m.ForceParts,
		FileName:          m.FileName,
		DownloadDirectory: m.DownloadDirectory,
		MaxConnections:    m.MaxConnections,
		MaxSegments:       m.MaxSegments,
		Handlers: &warplib.Handlers{
			ErrorHandler: func(_ string, err error) {
				uid := d.GetHash()
				er := pool.Broadcast(uid, server.InitError(err))
				pool.WriteError(uid, server.ErrorTypeCritical, err.Error())
				if er != nil {
					s.log.Printf("[%s]: %s", uid, er.Error())
				}
				pool.StopDownload(uid)
				d.Stop()
			},
			DownloadProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
					Action: "download_progress",
					Value:  int64(nread),
					Hash:   hash,
				}))
				if er != nil {
					s.log.Printf("[%s]: %s", uid, er.Error())
				}
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
					Action: "download_complete",
					Value:  tread,
					Hash:   hash,
				}))
				if er != nil {
					s.log.Printf("[%s]: %s", uid, er.Error())
				}
			},
			DownloadStoppedHandler: func() {
				uid := d.GetHash()
				er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
					Action: "download_stopped",
				}))
				if er != nil {
					s.log.Printf("[%s]: %s", uid, er.Error())
				}
			},
			CompileStartHandler: func(hash string) {
				uid := d.GetHash()
				er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
					Action: "compile_start",
					Hash:   hash,
				}))
				if er != nil {
					s.log.Printf("[%s]: %s", uid, er.Error())
				}
			},
			CompileProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
					Action: "compile_progress",
					Value:  int64(nread),
					Hash:   hash,
				}))
				if er != nil {
					s.log.Printf("[%s]: %s", uid, er.Error())
				}
			},
			CompileCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
					Action: "compile_complete",
					Value:  tread,
					Hash:   hash,
				}))
				if er != nil {
					s.log.Printf("[%s]: %s", uid, er.Error())
				}
			},
		},
	})
	if err != nil {
		return UPDATE_NEW_DOWNLOAD, nil, err
	}
	pool.AddDownload(d.GetHash(), sconn)
	err = s.manager.AddDownload(d, &warplib.AddDownloadOpts{
		ChildHash:        m.ChildHash,
		IsHidden:         m.IsHidden,
		IsChildren:       m.IsChildren,
		AbsoluteLocation: d.GetDownloadDirectory(),
	})
	if err != nil {
		return UPDATE_NEW_DOWNLOAD, nil, err
	}
	go d.Start()
	return UPDATE_NEW_DOWNLOAD, &NewDownloadResponse{
		ContentLength:     d.GetContentLength(),
		Uid:               d.GetHash(),
		FileName:          d.GetFileName(),
		SavePath:          d.GetSavePath(),
		DownloadDirectory: d.GetDownloadDirectory(),
	}, nil
}
