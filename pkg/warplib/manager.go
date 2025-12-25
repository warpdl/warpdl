package warplib

import (
	"encoding/gob"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"
)

// Default download data directory
var __USERDATA_FILE_NAME string

// Manager is a struct that manages the download items
// and their respective downloaders.
type Manager struct {
	// items is a map of download items
	items ItemsMap
	f     *os.File
	mu    *sync.RWMutex
}

// InitManager creates a new manager instance.
func InitManager() (m *Manager, err error) {
	m = &Manager{
		items: make(ItemsMap),
		mu:    new(sync.RWMutex),
	}
	m.f, err = WarpOpenFile(
		__USERDATA_FILE_NAME,
		os.O_RDWR|os.O_CREATE,
		0644,
	)
	if err != nil {
		m = nil
		return
	}
	_ = gob.NewDecoder(m.f).Decode(&m.items)
	m.populateMemPart()
	return
}

func (m *Manager) populateMemPart() {
	for _, item := range m.items {
		if item.memPart == nil {
			item.memPart = make(map[string]int64)
		}
		for ioff, part := range item.Parts {
			item.memPart[part.Hash] = ioff
		}
	}
}

// AddDownloadOpts contains optional parameters for AddDownload.
type AddDownloadOpts struct {
	IsHidden         bool
	IsChildren       bool
	ChildHash        string
	AbsoluteLocation string
}

// AddDownload adds a new download item entry.
func (m *Manager) AddDownload(d *Downloader, opts *AddDownloadOpts) (err error) {
	if opts == nil {
		opts = &AddDownloadOpts{}
	}
	item, err := newItem(
		m.mu,
		d.fileName,
		d.url,
		d.dlLoc,
		d.hash,
		d.contentLength,
		d.resumable,
		&itemOpts{
			AbsoluteLocation: opts.AbsoluteLocation,
			Child:            opts.IsChildren,
			Hide:             opts.IsHidden,
			ChildHash:        opts.ChildHash,
			Headers:          d.headers,
		},
	)
	if err != nil {
		return err
	}
	item.dAlloc = d
	m.UpdateItem(item)
	m.patchHandlers(d, item)
	return
}

// patchHandlers patches the handlers of the downloader to update the item.
func (m *Manager) patchHandlers(d *Downloader, item *Item) {
	oSPH := d.handlers.SpawnPartHandler
	d.handlers.SpawnPartHandler = func(hash string, ioff, foff int64) {
		item.addPart(hash, ioff, foff)
		m.UpdateItem(item)
		oSPH(hash, ioff, foff)
	}
	oRPH := d.handlers.RespawnPartHandler
	d.handlers.RespawnPartHandler = func(hash string, partIoff, ioffNew, foffNew int64) {
		item.addPart(hash, partIoff, foffNew)
		m.UpdateItem(item)
		oRPH(hash, partIoff, ioffNew, foffNew)
	}
	oPH := d.handlers.DownloadProgressHandler
	d.handlers.DownloadProgressHandler = func(hash string, nread int) {
		item.mu.Lock()
		item.Downloaded += ContentLength(nread)
		item.mu.Unlock()
		m.UpdateItem(item)
		oPH(hash, nread)
	}
	oCCH := d.handlers.CompileCompleteHandler
	d.handlers.CompileCompleteHandler = func(hash string, tread int64) {
		off, part := item.getPart(hash)
		if part == nil {
			d.handlers.ErrorHandler(hash, errors.New("manager part item is nil"))
			return
		}
		part.Compiled = true
		item.savePart(off, part)
		oCCH(hash, tread)
	}
	oDCH := d.handlers.DownloadCompleteHandler
	d.handlers.DownloadCompleteHandler = func(hash string, tread int64) {
		if hash != MAIN_HASH {
			return
		}
		item.mu.Lock()
		item.Parts = nil
		item.Downloaded = item.TotalSize
		item.mu.Unlock()
		m.UpdateItem(item)
		oDCH(hash, tread)
	}
}

// encode is an internal function of Manager
// which encodes its state and stores them as a file.
func (m *Manager) encode(e any) (err error) {
	m.mu.Lock()
	m.f.Seek(0, 0)
	defer m.mu.Unlock()
	return gob.NewEncoder(m.f).Encode(e)
}

// mapItem maps the item to the manager's items map.
func (m *Manager) mapItem(item *Item) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[item.Hash] = item
}

// deleteItem deletes the item from the manager's items map.
func (m *Manager) deleteItem(hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, hash)
}

// UpdateItem updates the item in the manager's items map.
func (m *Manager) UpdateItem(item *Item) {
	m.mapItem(item)
	m.encode(m.items)
}

// GetItems returns all the items in the manager.
func (m *Manager) GetItems() []*Item {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*Item, len(m.items))
	var i int
	for _, item := range m.items {
		items[i] = item
		i++
	}
	return items
}

// GetPublicItems returns all the public items in the manager.
// It excludes child items from the result.
func (m *Manager) GetPublicItems() []*Item {
	var items = []*Item{}
	for _, item := range m.GetItems() {
		if item.Children {
			continue
		}
		items = append(items, item)
	}
	return items
}

// GetIncompleteItems returns all the incomplete items in the manager.
func (m *Manager) GetIncompleteItems() []*Item {
	var items = []*Item{}
	for _, item := range m.GetItems() {
		if item.TotalSize == item.Downloaded {
			continue
		}
		items = append(items, item)
	}
	return items
}

// GetCompletedItems returns all the completed items in the manager.
func (m *Manager) GetCompletedItems() []*Item {
	var items = []*Item{}
	for _, item := range m.GetItems() {
		if item.TotalSize != item.Downloaded {
			continue
		}
		items = append(items, item)
	}
	return items
}

// GetItem returns the item with the given hash from the manager.
// It returns nil if the item does not exist.
func (m *Manager) GetItem(hash string) (item *Item) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item = m.items[hash]
	if item == nil {
		return
	}
	if item.memPart == nil {
		item.memPart = make(map[string]int64)
	}
	if item.mu == nil {
		item.mu = m.mu
	}
	return
}

// ResumeDownloadOpts contains optional parameters for ResumeDownload.
type ResumeDownloadOpts struct {
	ForceParts bool
	// MaxConnections sets the maximum number of parallel
	// network connections to be used for the downloading the file.
	MaxConnections int32
	// MaxSegments sets the maximum number of file segments
	// to be created for the downloading the file.
	MaxSegments int32
	Headers     Headers
	Handlers    *Handlers
	// RetryConfig configures retry behavior for transient errors.
	// If nil, DefaultRetryConfig() is used.
	RetryConfig *RetryConfig
	// RequestTimeout specifies the timeout for individual HTTP requests.
	// If zero, no per-request timeout is applied.
	RequestTimeout time.Duration
}

// ResumeDownload resumes a download item.
func (m *Manager) ResumeDownload(client *http.Client, hash string, opts *ResumeDownloadOpts) (item *Item, err error) {
	if opts == nil {
		opts = &ResumeDownloadOpts{}
	}
	item = m.GetItem(hash)
	if item == nil {
		err = ErrDownloadNotFound
		return
	}
	if !item.Resumable {
		err = ErrDownloadNotResumable
		return
	}
	// Validate integrity before attempting resume
	if err = validateDownloadIntegrity(item); err != nil {
		return
	}
	if item.Headers == nil {
		item.Headers = make(Headers, 0)
	}
	if opts.Headers != nil {
		for i, ih := range item.Headers {
			for _, oh := range opts.Headers {
				if ih != oh {
					continue
				}
				item.Headers[i] = oh
			}
		}
	}
	d, er := initDownloader(client, hash, item.Url, item.TotalSize, &DownloaderOpts{
		ForceParts:        opts.ForceParts,
		MaxConnections:    opts.MaxConnections,
		MaxSegments:       opts.MaxSegments,
		Handlers:          opts.Handlers,
		FileName:          item.Name,
		DownloadDirectory: item.DownloadLocation,
		Headers:           item.Headers,
		RetryConfig:       opts.RetryConfig,
		RequestTimeout:    opts.RequestTimeout,
	})
	if er != nil {
		err = er
		return
	}
	m.patchHandlers(d, item)
	item.dAlloc = d
	// m.UpdateItem(item)
	return
}

// Flush flushes away all the inactive download items.
func (m *Manager) Flush() {
	// add a write lock to prevent data modification while flushing
	m.mu.Lock()
	defer m.mu.Unlock()
	for hash, item := range m.items {
		if item.TotalSize != item.Downloaded && item.dAlloc != nil {
			continue
		}
		delete(m.items, hash)
		_ = WarpRemoveAll(GetPath(DlDataDir, hash))
	}
	m.f.Seek(0, 0)
	gob.NewEncoder(m.f).Encode(m.items)
}

// TODO: make FlushOne safe for flushing while the item is being downloaded

// FlushOne flushes away the download item with the given hash.
func (m *Manager) FlushOne(hash string) error {
	m.mu.RLock()
	item, found := m.items[hash]
	m.mu.RUnlock()
	if !found {
		return ErrFlushHashNotFound
	}
	if item.TotalSize != item.Downloaded && item.dAlloc != nil {
		return ErrFlushItemDownloading
	}
	m.deleteItem(hash)
	m.encode(m.items)
	return WarpRemoveAll(GetPath(DlDataDir, hash))
}

// Close closes the manager safely.
func (m *Manager) Close() error {
	return m.f.Close()
}
