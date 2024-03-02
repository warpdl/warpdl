package service

import (
	"encoding/json"

	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

const UPDATE_RESUME = "resume"

type ResumeMessage struct {
	DownloadId     string          `json:"download_id"`
	Headers        warplib.Headers `json:"headers,omitempty"`
	ForceParts     bool            `json:"force_parts,omitempty"`
	MaxConnections int             `json:"max_connections,omitempty"`
	MaxSegments    int             `json:"max_segments,omitempty"`
}

type ResumeResponse struct {
	ChildHash         string                `json:"child_hash,omitempty"`
	FileName          string                `json:"file_name"`
	SavePath          string                `json:"save_path"`
	DownloadDirectory string                `json:"download_directory"`
	AbsoluteLocation  string                `json:"absolute_location"`
	ContentLength     warplib.ContentLength `json:"content_length"`
}

func getHandler(s *Service, pool *server.Pool, uidPtr *string, stopDownloadPtr *func()) *warplib.Handlers {
	return &warplib.Handlers{
		ErrorHandler: func(_ string, err error) {
			uid := *uidPtr
			er := pool.Broadcast(uid, server.InitError(err))
			pool.WriteError(uid, server.ErrorTypeCritical, err.Error())
			if er != nil {
				s.log.Printf("[%s]: %s", uid, er.Error())
			}
			pool.StopDownload(uid)
			(*stopDownloadPtr)()
		},
		ResumeProgressHandler: func(hash string, nread int) {
			uid := *uidPtr
			er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
				DownloadId: uid,
				Action:     "resume_progress",
				Value:      int64(nread),
				Hash:       hash,
			}))
			if er != nil {
				s.log.Printf("[%s]: %s", uid, er.Error())
			}
		},
		DownloadProgressHandler: func(hash string, nread int) {
			uid := *uidPtr
			er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
				DownloadId: uid,
				Action:     "download_progress",
				Value:      int64(nread),
				Hash:       hash,
			}))
			if er != nil {
				s.log.Printf("[%s]: %s", uid, er.Error())
			}
		},
		DownloadCompleteHandler: func(hash string, tread int64) {
			uid := *uidPtr
			er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
				DownloadId: uid,
				Action:     "download_complete",
				Value:      tread,
				Hash:       hash,
			}))
			if er != nil {
				s.log.Printf("[%s]: %s", uid, er.Error())
			}
		},
		DownloadStoppedHandler: func() {
			uid := *uidPtr
			er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
				DownloadId: uid,
				Action:     "download_stopped",
			}))
			if er != nil {
				s.log.Printf("[%s]: %s", uid, er.Error())
			}
		},
		CompileStartHandler: func(hash string) {
			uid := *uidPtr
			er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
				DownloadId: uid,
				Action:     "compile_start",
				Hash:       hash,
			}))
			if er != nil {
				s.log.Printf("[%s]: %s", uid, er.Error())
			}
		},
		CompileProgressHandler: func(hash string, nread int) {
			uid := *uidPtr
			er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
				DownloadId: uid,
				Action:     "compile_progress",
				Value:      int64(nread),
				Hash:       hash,
			}))
			if er != nil {
				s.log.Printf("[%s]: %s", uid, er.Error())
			}
		},
		CompileCompleteHandler: func(hash string, tread int64) {
			uid := *uidPtr
			er := pool.Broadcast(uid, server.MakeResult(UPDATE_DOWNLOADING, &DownloadingResponse{
				DownloadId: uid,
				Action:     "compile_complete",
				Value:      tread,
				Hash:       hash,
			}))
			if er != nil {
				s.log.Printf("[%s]: %s", uid, er.Error())
			}
		},
	}
}

func resumeItem(i *warplib.Item) error {
	if i.Downloaded >= i.TotalSize {
		return nil
	}
	return i.Resume()
}

func (s *Service) resumeHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (string, any, error) {
	var m ResumeMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return UPDATE_RESUME, nil, err
	}
	__stop := func() {}
	var (
		err          error
		item         *warplib.Item
		hash                 = &m.DownloadId
		stopDownload *func() = &__stop
	)
	item, err = s.manager.ResumeDownload(s.client, m.DownloadId, &warplib.ResumeDownloadOpts{
		Headers:        m.Headers,
		ForceParts:     m.ForceParts,
		MaxConnections: m.MaxConnections,
		MaxSegments:    m.MaxSegments,
		Handlers:       getHandler(s, pool, hash, stopDownload),
	})
	if err != nil {
		return UPDATE_RESUME, nil, err
	}
	pool.AddDownload(m.DownloadId, sconn)
	*hash = item.Hash
	*stopDownload = item.StopDownload
	var cItem *warplib.Item
	if item.ChildHash != "" {
		var (
			cStopDownload *func() = &__stop
		)
		cItem, err = s.manager.ResumeDownload(s.client, item.ChildHash, &warplib.ResumeDownloadOpts{
			Headers:        m.Headers,
			ForceParts:     m.ForceParts,
			MaxConnections: m.MaxConnections,
			MaxSegments:    m.MaxSegments,
			Handlers:       getHandler(s, pool, &item.ChildHash, cStopDownload),
		})
		if err != nil {
			return UPDATE_RESUME, nil, err
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
	return UPDATE_DOWNLOAD, &ResumeResponse{
		ChildHash:         item.ChildHash,
		ContentLength:     item.TotalSize,
		FileName:          item.Name,
		SavePath:          item.GetSavePath(),
		DownloadDirectory: item.DownloadLocation,
		AbsoluteLocation:  item.AbsoluteLocation,
	}, nil
}
