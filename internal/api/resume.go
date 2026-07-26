package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/cookies"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func resumeLease(
	ctx context.Context,
	i *warplib.Item,
	lease *warplib.ReconstructionLease,
) error {
	snapshot := i.Snapshot()
	if snapshot.Downloaded >= snapshot.TotalSize {
		return nil
	}
	return lease.ResumeContext(ctx)
}

func closeReconstructionLease(lease *warplib.ReconstructionLease) error {
	if lease == nil {
		return nil
	}
	_, err := lease.Close()
	return err
}

func (s *Api) reimportCookieHeader(snapshot warplib.ItemSnapshot, operation string) warplib.Headers {
	if snapshot.CookieSourcePath == "" {
		return nil
	}
	parsedURL, err := url.Parse(snapshot.URL)
	if err != nil {
		s.log.Printf("warning: failed to parse URL for cookie re-import on %s: %s\n", operation, err.Error())
		return nil
	}

	domain := parsedURL.Hostname()
	var (
		importedCookies []cookies.Cookie
		source          *cookies.CookieSource
	)
	if snapshot.CookieSourcePath == "auto" {
		importedCookies, source, err = cookies.DetectBrowserCookies(domain)
	} else {
		importedCookies, source, err = cookies.ImportCookies(snapshot.CookieSourcePath, domain)
	}
	if err != nil {
		s.log.Printf("warning: failed to re-import cookies on %s: %s\n", operation, err.Error())
		return nil
	}
	if len(importedCookies) == 0 {
		return nil
	}

	sourceName := snapshot.CookieSourcePath
	if source != nil {
		sourceName = source.Browser
	}
	s.log.Printf("Re-imported %d cookies for %s from %s\n", len(importedCookies), domain, sourceName)
	return warplib.Headers{{
		Key:   "Cookie",
		Value: cookies.BuildCookieHeader(importedCookies),
	}}
}

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

	transferCtx := s.manager.TransferContext()
	generation, reserved := pool.BeginDownload(m.DownloadId, sconn)
	if !reserved {
		return common.UPDATE_RESUME, nil, errors.New("download is already running or still stopping")
	}
	admitted := false
	var childGeneration *server.TransferGeneration
	defer func() {
		if !admitted {
			generation.Abort()
			if childGeneration != nil {
				childGeneration.Abort()
			}
		}
	}()

	var item *warplib.Item
	var parentLease *warplib.ReconstructionLease
	var transientHeaders warplib.Headers
	if existing := s.manager.GetItem(m.DownloadId); existing != nil {
		transientHeaders = s.reimportCookieHeader(existing.Snapshot(), "resume")
	}
	parentHandlers := managedTransferHandlers(
		func() *server.TransferGeneration { return generation },
		func() bool { return item != nil && item.IsStopped() },
		transferCtx,
	)
	item, parentLease, err = s.manager.ResumeDownloadWithLease(rsClient, m.DownloadId, &warplib.ResumeDownloadOpts{
		Headers:          m.Headers,
		TransientHeaders: transientHeaders,
		ForceParts:       m.ForceParts,
		MaxConnections:   m.MaxConnections,
		MaxSegments:      m.MaxSegments,
		Handlers:         parentHandlers,
		RetryConfig:      retryConfig,
		RequestTimeout:   requestTimeout,
		SpeedLimit:       speedLimit,
		ProxyURL:         m.Proxy,
	})
	if err != nil {
		return common.UPDATE_RESUME, nil, errors.Join(
			err,
			closeReconstructionLease(parentLease),
		)
	}
	itemSnapshot := item.Snapshot()

	var cItem *warplib.Item
	var childLease *warplib.ReconstructionLease
	var childTotal warplib.ContentLength
	childHash := itemSnapshot.ChildHash
	if childHash != "" {
		childGeneration, reserved = pool.BeginDownload(childHash, sconn)
		if !reserved {
			return common.UPDATE_RESUME, nil, errors.Join(
				errors.New("child download is already running or still stopping"),
				closeReconstructionLease(parentLease),
			)
		}
		var childTransientHeaders warplib.Headers
		if existingChild := s.manager.GetItem(childHash); existingChild != nil {
			childTransientHeaders = s.reimportCookieHeader(existingChild.Snapshot(), "child resume")
		}
		childHandlers := managedTransferHandlers(
			func() *server.TransferGeneration { return childGeneration },
			func() bool { return cItem != nil && cItem.IsStopped() },
			transferCtx,
		)
		cItem, childLease, err = s.manager.ResumeDownloadWithLease(rsClient, childHash, &warplib.ResumeDownloadOpts{
			Headers:          m.Headers,
			TransientHeaders: childTransientHeaders,
			ForceParts:       m.ForceParts,
			MaxConnections:   m.MaxConnections,
			MaxSegments:      m.MaxSegments,
			Handlers:         childHandlers,
			RetryConfig:      retryConfig,
			RequestTimeout:   requestTimeout,
			SpeedLimit:       speedLimit,
			ProxyURL:         m.Proxy,
		})
		if err != nil {
			return common.UPDATE_RESUME, nil, errors.Join(
				err,
				closeReconstructionLease(childLease),
				closeReconstructionLease(parentLease),
			)
		}
		childTotal = cItem.Snapshot().TotalSize
		item.AddTotalSize(childTotal)
	}

	if !s.manager.GoTransfer(func(ctx context.Context) {
		var transfers sync.WaitGroup
		transfers.Add(1)
		go func() {
			defer transfers.Done()
			finishLeaseManagedTransfer(
				ctx,
				generation,
				parentLease,
				resumeLease(ctx, item, parentLease),
			)
		}()
		if cItem != nil {
			transfers.Add(1)
			go func() {
				defer transfers.Done()
				finishLeaseManagedTransfer(
					ctx,
					childGeneration,
					childLease,
					resumeLease(ctx, cItem, childLease),
				)
			}()
		}
		transfers.Wait()
	}) {
		if cItem != nil {
			item.AddTotalSize(-childTotal)
		}
		return common.UPDATE_RESUME, nil, errors.Join(
			warplib.ErrManagerShuttingDown,
			closeReconstructionLease(childLease),
			closeReconstructionLease(parentLease),
		)
	}
	admitted = true

	maxConn, _ := item.GetMaxConnections()
	maxParts, _ := item.GetMaxParts()
	itemSnapshot = item.Snapshot()
	return common.UPDATE_RESUME, &common.ResumeResponse{
		ChildHash:         itemSnapshot.ChildHash,
		ContentLength:     itemSnapshot.TotalSize,
		Downloaded:        itemSnapshot.Downloaded,
		FileName:          itemSnapshot.Name,
		SavePath:          item.GetSavePath(),
		DownloadDirectory: itemSnapshot.DownloadLocation,
		AbsoluteLocation:  itemSnapshot.AbsoluteLocation,
		MaxConnections:    maxConn,
		MaxSegments:       maxParts,
	}, nil
}
