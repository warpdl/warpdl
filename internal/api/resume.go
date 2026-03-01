package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/cookies"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func getHandler(pool *server.Pool, uidPtr *string, stopDownloadPtr *func() error, isStoppedPtr *func() bool) *warplib.Handlers {
	return &warplib.Handlers{
		ErrorHandler: func(_ string, err error) {
			if errors.Is(err, context.Canceled) && *isStoppedPtr != nil && (*isStoppedPtr)() {
				return
			}
			uid := *uidPtr
			pool.Broadcast(uid, server.InitError(err))
			pool.WriteError(uid, server.ErrorTypeCritical, err.Error())
			pool.StopDownload(uid)
			(*stopDownloadPtr)()
		},
		ResumeProgressHandler: func(hash string, nread int) {
			uid := *uidPtr
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.ResumeProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		DownloadProgressHandler: func(hash string, nread int) {
			uid := *uidPtr
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		DownloadCompleteHandler: func(hash string, tread int64) {
			uid := *uidPtr
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadComplete,
				Value:      tread,
				Hash:       hash,
			}))
		},
		DownloadStoppedHandler: func() {
			uid := *uidPtr
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.DownloadStopped,
			}))
		},
		CompileStartHandler: func(hash string) {
			uid := *uidPtr
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileStart,
				Hash:       hash,
			}))
		},
		CompileProgressHandler: func(hash string, nread int) {
			uid := *uidPtr
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileProgress,
				Value:      int64(nread),
				Hash:       hash,
			}))
		},
		CompileCompleteHandler: func(hash string, tread int64) {
			uid := *uidPtr
			pool.Broadcast(uid, server.MakeResult(common.UPDATE_DOWNLOADING, &common.DownloadingResponse{
				DownloadId: uid,
				Action:     common.CompileComplete,
				Value:      tread,
				Hash:       hash,
			}))
		},
	}
}

func resumeItem(i *warplib.Item) error {
	if i.Downloaded >= i.TotalSize {
		return nil
	}
	return i.Resume()
}

var __stop = func() error { return nil }

func (s *Api) resumeHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var m common.ResumeParams
	if err := json.Unmarshal(body, &m); err != nil {
		return common.UPDATE_RESUME, nil, err
	}

	// Determine which client to use based on proxy setting
	rsClient := s.client
	if m.Proxy != "" {
		var err error
		rsClient, err = warplib.NewHTTPClientWithProxy(m.Proxy)
		if err != nil {
			return common.UPDATE_RESUME, nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		// Preserve cookie jar from default client
		if s.client.Jar != nil {
			rsClient.Jar = s.client.Jar
		}
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
	var err error
	if m.SpeedLimit != "" {
		speedLimit, err = warplib.ParseSpeedLimit(m.SpeedLimit)
		if err != nil {
			return common.UPDATE_RESUME, nil, fmt.Errorf("invalid speed limit: %w", err)
		}
	}

	var (
		item         *warplib.Item
		hash         = &m.DownloadId
		stopDownload = &__stop
		isStopped    = func() bool { return false }
	)
	item, err = s.manager.ResumeDownload(rsClient, m.DownloadId, &warplib.ResumeDownloadOpts{
		Headers:        m.Headers,
		ForceParts:     m.ForceParts,
		MaxConnections: m.MaxConnections,
		MaxSegments:    m.MaxSegments,
		Handlers:       getHandler(pool, hash, stopDownload, &isStopped),
		RetryConfig:    retryConfig,
		RequestTimeout: requestTimeout,
		SpeedLimit:     speedLimit,
	})
	if err != nil {
		return common.UPDATE_RESUME, nil, err
	}
	// Re-import cookies on resume if CookieSourcePath is set
	if item.CookieSourcePath != "" {
		parsedURL, urlErr := url.Parse(item.Url)
		if urlErr == nil {
			domain := parsedURL.Hostname()
			var importedCookies []cookies.Cookie
			var source *cookies.CookieSource
			var cookieErr error
			if item.CookieSourcePath == "auto" {
				importedCookies, source, cookieErr = cookies.DetectBrowserCookies(domain)
			} else {
				importedCookies, source, cookieErr = cookies.ImportCookies(item.CookieSourcePath, domain)
			}
			if cookieErr != nil {
				s.log.Printf("warning: failed to re-import cookies on resume: %s\n", cookieErr.Error())
			} else if len(importedCookies) > 0 {
				cookieHeader := cookies.BuildCookieHeader(importedCookies)
				item.Headers.Update("Cookie", cookieHeader)
				s.manager.UpdateItem(item)
				s.log.Printf("Re-imported %d cookies for %s from %s\n", len(importedCookies), domain, source.Browser)
			}
		}
	}

	pool.AddDownload(m.DownloadId, sconn)
	*hash = item.Hash
	*stopDownload = item.StopDownload
	isStopped = item.IsStopped
	var cItem *warplib.Item
	if item.ChildHash != "" {
		var cStopDownload = &__stop
		cIsStopped := func() bool { return false }
		cItem, err = s.manager.ResumeDownload(rsClient, item.ChildHash, &warplib.ResumeDownloadOpts{
			Headers:        m.Headers,
			ForceParts:     m.ForceParts,
			MaxConnections: m.MaxConnections,
			MaxSegments:    m.MaxSegments,
			Handlers:       getHandler(pool, &item.ChildHash, cStopDownload, &cIsStopped),
			RetryConfig:    retryConfig,
			RequestTimeout: requestTimeout,
			SpeedLimit:     speedLimit,
		})
		if err != nil {
			// Clean up parent's downloader before returning
			_ = item.CloseDownloader()
			return common.UPDATE_RESUME, nil, err
		}
		pool.AddDownload(item.ChildHash, sconn)
		*cStopDownload = cItem.StopDownload
		cIsStopped = cItem.IsStopped
		// need more opinions on this one:
		item.TotalSize += cItem.TotalSize
	}
	go resumeItem(item)
	if cItem != nil {
		go resumeItem(cItem)
	}
	maxConn, _ := item.GetMaxConnections()
	maxParts, _ := item.GetMaxParts()
	return common.UPDATE_RESUME, &common.ResumeResponse{
		ChildHash:         item.ChildHash,
		ContentLength:     item.TotalSize,
		Downloaded:        item.Downloaded,
		FileName:          item.Name,
		SavePath:          item.GetSavePath(),
		DownloadDirectory: item.DownloadLocation,
		AbsoluteLocation:  item.AbsoluteLocation,
		MaxConnections:    maxConn,
		MaxSegments:       maxParts,
	}, nil
}
