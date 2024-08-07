package api

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func getHandler(pool *server.Pool, uidPtr *string, stopDownloadPtr *func() error) *warplib.Handlers {
	return &warplib.Handlers{
		ErrorHandler: func(_ string, err error) {
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
	var (
		err          error
		item         *warplib.Item
		hash         = &m.DownloadId
		stopDownload = &__stop
	)
	item, err = s.manager.ResumeDownload(s.client, m.DownloadId, &warplib.ResumeDownloadOpts{
		Headers:        m.Headers,
		ForceParts:     m.ForceParts,
		MaxConnections: m.MaxConnections,
		MaxSegments:    m.MaxSegments,
		Handlers:       getHandler(pool, hash, stopDownload),
	})
	if err != nil {
		return common.UPDATE_RESUME, nil, err
	}
	pool.AddDownload(m.DownloadId, sconn)
	*hash = item.Hash
	*stopDownload = item.StopDownload
	var cItem *warplib.Item
	if item.ChildHash != "" {
		var cStopDownload = &__stop
		cItem, err = s.manager.ResumeDownload(s.client, item.ChildHash, &warplib.ResumeDownloadOpts{
			Headers:        m.Headers,
			ForceParts:     m.ForceParts,
			MaxConnections: m.MaxConnections,
			MaxSegments:    m.MaxSegments,
			Handlers:       getHandler(pool, &item.ChildHash, cStopDownload),
		})
		if err != nil {
			return common.UPDATE_RESUME, nil, err
		}
		pool.AddDownload(item.ChildHash, sconn)
		*cStopDownload = cItem.StopDownload
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
		FileName:          item.Name,
		SavePath:          item.GetSavePath(),
		DownloadDirectory: item.DownloadLocation,
		AbsoluteLocation:  item.AbsoluteLocation,
		MaxConnections:    maxConn,
		MaxSegments:       maxParts,
	}, nil
}
