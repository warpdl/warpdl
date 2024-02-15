package service

import (
	"encoding/json"
	"net"

	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

type DownloadMessage struct {
	Url               string          `json:"url"`
	Headers           warplib.Headers `json:"headers"`
	ForceParts        bool            `json:"force_parts"`
	FileName          string          `json:"file_name"`
	DownloadDirectory string          `json:"download_directory"`
	MaxConnections    int             `json:"max_connections"`
	MaxSegments       int             `json:"max_segments"`
	ChildHash         string          `json:"child_hash"`
	IsHidden          bool            `json:"is_hidden"`
	IsChildren        bool            `json:"is_children"`
}

type NewDownloadResponse struct {
	Uid               string                `json:"uid"`
	FileName          string                `json:"file_name"`
	SavePath          string                `json:"save_path"`
	DownloadDirectory string                `json:"download_directory"`
	ContentLength     warplib.ContentLength `json:"content_length"`
}

type DownloadingResponse struct {
	Action string `json:"action"`
	Hash   string `json:"hash"`
	Value  int64  `json:"value,omitempty"`
}

func (s *Service) downloadHandler(conn net.Conn, pool *server.Pool, body json.RawMessage) (any, error) {
	var m DownloadMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
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
				pool.Broadcast(uid, server.InitError(err))
				pool.WriteError(uid, server.ErrorTypeCritical, err.Error())
			},
			DownloadProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(&DownloadingResponse{
					Action: "download_progress",
					Value:  int64(nread),
					Hash:   hash,
				}))
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(&DownloadingResponse{
					Action: "download_complete",
					Value:  tread,
					Hash:   hash,
				}))
			},
			CompileStartHandler: func(hash string) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(&DownloadingResponse{
					Action: "compile_start",
					Hash:   hash,
				}))
			},
			CompileProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(&DownloadingResponse{
					Action: "compile_progress",
					Value:  int64(nread),
					Hash:   hash,
				}))
			},
			CompileCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(&DownloadingResponse{
					Action: "compile_complete",
					Value:  tread,
					Hash:   hash,
				}))
			},
		},
	})
	if err != nil {
		return nil, err
	}
	pool.AddDownload(d.GetHash(), conn)
	err = s.manager.AddDownload(d, &warplib.AddDownloadOpts{
		ChildHash:        m.ChildHash,
		IsHidden:         m.IsHidden,
		IsChildren:       m.IsChildren,
		AbsoluteLocation: d.GetDownloadDirectory(),
	})
	if err != nil {
		return nil, err
	}
	go d.Start()
	return &NewDownloadResponse{
		ContentLength:     d.GetContentLength(),
		Uid:               d.GetHash(),
		FileName:          d.GetFileName(),
		SavePath:          d.GetSavePath(),
		DownloadDirectory: d.GetDownloadDirectory(),
	}, nil
}
