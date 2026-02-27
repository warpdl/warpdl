package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"
	"github.com/creachadair/jrpc2/jhttp"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// Custom JSON-RPC error codes for download operations.
const (
	codeDownloadNotFound  = jrpc2.Code(-32001)
	codeDownloadNotActive = jrpc2.Code(-32002)
	codeInvalidParams     = jrpc2.Code(-32602)
)

// RPCConfig holds configuration for the JSON-RPC endpoint.
type RPCConfig struct {
	Secret    string // Auth token (required -- empty means RPC disabled)
	ListenAll bool   // If true, bind to 0.0.0.0 instead of 127.0.0.1
	Version   string // Daemon version
	Commit    string // Git commit
	BuildType string // Build type
}

// RPCServer manages the JSON-RPC 2.0 bridge and method handlers.
type RPCServer struct {
	bridge       jhttp.Bridge
	secret       string
	version      string
	commit       string
	buildType    string
	manager      *warplib.Manager
	client       *http.Client
	pool         *Pool
	schemeRouter *warplib.SchemeRouter
}

// VersionResult is the response for system.getVersion.
type VersionResult struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildType string `json:"buildType,omitempty"`
}

// AddParams is the input for download.add.
type AddParams struct {
	URL         string          `json:"url"`
	FileName    string          `json:"fileName,omitempty"`
	Dir         string          `json:"dir,omitempty"`
	Headers     warplib.Headers `json:"headers,omitempty"`
	Connections int32           `json:"connections,omitempty"`
	SSHKeyPath  string          `json:"sshKeyPath,omitempty"`
}

// AddResult is the response for download.add.
type AddResult struct {
	GID string `json:"gid"`
}

// GIDParam is a common input with just a download GID.
type GIDParam struct {
	GID string `json:"gid"`
}

// StatusResult is the response for download.status.
type StatusResult struct {
	GID             string `json:"gid"`
	Status          string `json:"status"`
	TotalLength     int64  `json:"totalLength"`
	CompletedLength int64  `json:"completedLength"`
	Percentage      int64  `json:"percentage"`
	FileName        string `json:"fileName"`
}

// ListParams is the input for download.list.
type ListParams struct {
	Status string `json:"status,omitempty"` // "active", "waiting", "complete", "all" (default)
}

// ListItem is a single entry in the download.list response.
type ListItem struct {
	GID             string `json:"gid"`
	Status          string `json:"status"`
	TotalLength     int64  `json:"totalLength"`
	CompletedLength int64  `json:"completedLength"`
	FileName        string `json:"fileName"`
}

// ListResult is the response for download.list.
type ListResult struct {
	Downloads []*ListItem `json:"downloads"`
}

// EmptyResult is a placeholder for methods that return no data.
type EmptyResult struct{}

// NewRPCServer creates a new RPCServer with method handlers and HTTP bridge.
func NewRPCServer(cfg *RPCConfig, m *warplib.Manager, client *http.Client, pool *Pool, router *warplib.SchemeRouter) *RPCServer {
	rs := &RPCServer{
		secret:       cfg.Secret,
		version:      cfg.Version,
		commit:       cfg.Commit,
		buildType:    cfg.BuildType,
		manager:      m,
		client:       client,
		pool:         pool,
		schemeRouter: router,
	}

	methods := handler.Map{
		"system.getVersion": handler.New(rs.systemGetVersion),
		"download.add":     handler.New(rs.downloadAdd),
		"download.pause":   handler.New(rs.downloadPause),
		"download.resume":  handler.New(rs.downloadResume),
		"download.remove":  handler.New(rs.downloadRemove),
		"download.status":  handler.New(rs.downloadStatus),
		"download.list":    handler.New(rs.downloadList),
	}

	rs.bridge = jhttp.NewBridge(methods, nil)
	return rs
}

func (rs *RPCServer) systemGetVersion(_ context.Context) (*VersionResult, error) {
	return &VersionResult{
		Version:   rs.version,
		Commit:    rs.commit,
		BuildType: rs.buildType,
	}, nil
}

// downloadAdd creates a new download from a URL.
func (rs *RPCServer) downloadAdd(_ context.Context, p *AddParams) (*AddResult, error) {
	if p.URL == "" {
		return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "missing required param: url"}
	}

	parsed, err := url.Parse(p.URL)
	if err != nil {
		return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "invalid url: " + err.Error()}
	}

	scheme := strings.ToLower(parsed.Scheme)
	connections := p.Connections
	if connections <= 0 {
		connections = 24
	}

	opts := &warplib.DownloaderOpts{
		FileName:          p.FileName,
		DownloadDirectory: p.Dir,
		Headers:           p.Headers,
		MaxConnections:    connections,
		SSHKeyPath:        p.SSHKeyPath,
	}

	switch scheme {
	case "http", "https":
		d, err := warplib.NewDownloader(rs.client, p.URL, opts)
		if err != nil {
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: err.Error()}
		}
		if err := rs.manager.AddDownload(d, &warplib.AddDownloadOpts{
			AbsoluteLocation: d.GetDownloadDirectory(),
		}); err != nil {
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: err.Error()}
		}
		hash := d.GetHash()
		if rs.pool != nil {
			rs.pool.AddDownload(hash, nil)
		}
		go d.Start()
		return &AddResult{GID: hash}, nil

	default:
		// FTP, FTPS, SFTP -- use SchemeRouter
		if rs.schemeRouter == nil {
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "unsupported scheme: " + scheme}
		}
		pd, err := rs.schemeRouter.NewDownloader(p.URL, opts)
		if err != nil {
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: err.Error()}
		}
		probe, err := pd.Probe(context.Background())
		if err != nil {
			pd.Close()
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: err.Error()}
		}
		cleanURL := warplib.StripURLCredentials(p.URL)
		var proto warplib.Protocol
		switch scheme {
		case "ftps":
			proto = warplib.ProtoFTPS
		case "sftp":
			proto = warplib.ProtoSFTP
		default:
			proto = warplib.ProtoFTP
		}
		if err := rs.manager.AddProtocolDownload(pd, probe, cleanURL, proto, nil, &warplib.AddDownloadOpts{
			AbsoluteLocation: pd.GetDownloadDirectory(),
		}); err != nil {
			return nil, &jrpc2.Error{Code: codeInvalidParams, Message: err.Error()}
		}
		hash := pd.GetHash()
		if rs.pool != nil {
			rs.pool.AddDownload(hash, nil)
		}
		go pd.Download(context.Background(), nil)
		return &AddResult{GID: hash}, nil
	}
}

// downloadPause stops an active download.
func (rs *RPCServer) downloadPause(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, &jrpc2.Error{Code: codeDownloadNotFound, Message: "download not found"}
	}
	if rs.pool != nil && !rs.pool.HasDownload(p.GID) {
		return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: "download not running"}
	}
	item.StopDownload()
	return &EmptyResult{}, nil
}

// downloadResume resumes a paused download.
func (rs *RPCServer) downloadResume(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, &jrpc2.Error{Code: codeDownloadNotFound, Message: "download not found"}
	}
	resumedItem, err := rs.manager.ResumeDownload(rs.client, p.GID, nil)
	if err != nil {
		return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: err.Error()}
	}
	if rs.pool != nil {
		rs.pool.AddDownload(p.GID, nil)
	}
	go resumedItem.Resume()
	return &EmptyResult{}, nil
}

// downloadRemove removes a download from the manager.
func (rs *RPCServer) downloadRemove(_ context.Context, p *GIDParam) (*EmptyResult, error) {
	if err := rs.manager.FlushOne(p.GID); err != nil {
		if err == warplib.ErrFlushHashNotFound {
			return nil, &jrpc2.Error{Code: codeDownloadNotFound, Message: "download not found"}
		}
		return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: err.Error()}
	}
	return &EmptyResult{}, nil
}

// downloadStatus returns the status of a download.
func (rs *RPCServer) downloadStatus(_ context.Context, p *GIDParam) (*StatusResult, error) {
	item := rs.manager.GetItem(p.GID)
	if item == nil {
		return nil, &jrpc2.Error{Code: codeDownloadNotFound, Message: "download not found"}
	}
	return &StatusResult{
		GID:             item.Hash,
		Status:          itemStatus(item),
		TotalLength:     int64(item.TotalSize),
		CompletedLength: int64(item.Downloaded),
		Percentage:      item.GetPercentage(),
		FileName:        item.Name,
	}, nil
}

// downloadList returns a list of downloads, optionally filtered by status.
func (rs *RPCServer) downloadList(_ context.Context, p *ListParams) (*ListResult, error) {
	var items []*warplib.Item

	status := p.Status
	if status == "" {
		status = "all"
	}

	switch status {
	case "all":
		items = rs.manager.GetItems()
	case "active":
		for _, item := range rs.manager.GetItems() {
			if item.IsDownloading() {
				items = append(items, item)
			}
		}
	case "complete":
		items = rs.manager.GetCompletedItems()
	case "waiting":
		for _, item := range rs.manager.GetIncompleteItems() {
			if !item.IsDownloading() {
				items = append(items, item)
			}
		}
	default:
		items = rs.manager.GetItems()
	}

	downloads := make([]*ListItem, 0, len(items))
	for _, item := range items {
		downloads = append(downloads, &ListItem{
			GID:             item.Hash,
			Status:          itemStatus(item),
			TotalLength:     int64(item.TotalSize),
			CompletedLength: int64(item.Downloaded),
			FileName:        item.Name,
		})
	}

	return &ListResult{Downloads: downloads}, nil
}

// itemStatus returns the status string for a download item.
func itemStatus(item *warplib.Item) string {
	if item.IsDownloading() {
		return "active"
	}
	if item.Downloaded >= item.TotalSize && item.TotalSize > 0 {
		return "complete"
	}
	return "waiting"
}

// Close shuts down the jrpc2 bridge, releasing internal goroutines.
func (rs *RPCServer) Close() {
	rs.bridge.Close()
}
