package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func (s *Api) downloadHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.DownloadParams
	if err := json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}

	// Determine which client to use based on proxy setting
	dlClient := s.client
	if m.Proxy != "" {
		var err error
		dlClient, err = warplib.NewHTTPClientWithProxy(m.Proxy)
		if err != nil {
			return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		// Preserve cookie jar from default client
		if s.client.Jar != nil {
			dlClient.Jar = s.client.Jar
		}
	}

	var (
		d *warplib.Downloader
	)
	url, err := s.elEngine.Extract(m.Url)
	if err != nil {
		s.log.Printf("failed to extract URL from extension: %s\n", err.Error())
		url = m.Url
	}

	// Build retry config from params
	var retryConfig *warplib.RetryConfig
	if m.MaxRetries != 0 || m.RetryDelay != 0 {
		cfg := warplib.DefaultRetryConfig()
		if m.MaxRetries != 0 {
			cfg.MaxRetries = m.MaxRetries
		}
		if m.RetryDelay != 0 {
			cfg.BaseDelay = time.Duration(m.RetryDelay) * time.Millisecond
		}
		retryConfig = &cfg
	}

	// Convert timeout from seconds to duration
	var requestTimeout time.Duration
	if m.Timeout > 0 {
		requestTimeout = time.Duration(m.Timeout) * time.Second
	}

	// Parse speed limit
	var speedLimit int64
	if m.SpeedLimit != "" {
		speedLimit, err = warplib.ParseSpeedLimit(m.SpeedLimit)
		if err != nil {
			return common.UPDATE_DOWNLOAD, nil, fmt.Errorf("invalid speed limit: %w", err)
		}
	}

	d, err = warplib.NewDownloader(dlClient, url, &warplib.DownloaderOpts{
		Headers:           m.Headers,
		ForceParts:        m.ForceParts,
		FileName:          m.FileName,
		DownloadDirectory: m.DownloadDirectory,
		MaxConnections:    m.MaxConnections,
		MaxSegments:       m.MaxSegments,
		Overwrite:         m.Overwrite,
		RetryConfig:       retryConfig,
		RequestTimeout:    requestTimeout,
		SpeedLimit:        speedLimit,
		Handlers: &warplib.Handlers{
			ErrorHandler: func(_ string, err error) {
				if errors.Is(err, context.Canceled) && d.IsStopped() {
					return
				}
				uid := d.GetHash()
				pool.Broadcast(uid, server.InitError(err))
				pool.WriteError(uid, server.ErrorTypeCritical, err.Error())
				pool.StopDownload(uid)
				s.manager.GetItem(uid).StopDownload()
			},
			DownloadProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadProgress,
					Value:      int64(nread),
					Hash:       hash,
				}))
			},
			DownloadCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadComplete,
					Value:      tread,
					Hash:       hash,
				}))
			},
			DownloadStoppedHandler: func() {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.DownloadStopped,
				}))
			},
			CompileStartHandler: func(hash string) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileStart,
					Hash:       hash,
				}))
			},
			CompileProgressHandler: func(hash string, nread int) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileProgress,
					Value:      int64(nread),
					Hash:       hash,
				}))
			},
			CompileCompleteHandler: func(hash string, tread int64) {
				uid := d.GetHash()
				pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
					DownloadId: uid,
					Action:     common.CompileComplete,
					Value:      tread,
					Hash:       hash,
				}))
			},
		},
	})
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}
	pool.AddDownload(d.GetHash(), sconn)
	err = s.manager.AddDownload(d, &warplib.AddDownloadOpts{
		ChildHash:        m.ChildHash,
		IsHidden:         m.IsHidden,
		IsChildren:       m.IsChildren,
		AbsoluteLocation: d.GetDownloadDirectory(),
		Priority:         warplib.Priority(m.Priority),
	})
	if err != nil {
		return common.UPDATE_DOWNLOAD, nil, err
	}
	// todo: handle download start error
	go d.Start()
	return common.UPDATE_DOWNLOAD, &common.DownloadResponse{
		ContentLength:     d.GetContentLength(),
		DownloadId:        d.GetHash(),
		FileName:          d.GetFileName(),
		SavePath:          d.GetSavePath(),
		DownloadDirectory: d.GetDownloadDirectory(),
		MaxConnections:    d.GetMaxConnections(),
		MaxSegments:       d.GetMaxParts(),
	}, nil
}
