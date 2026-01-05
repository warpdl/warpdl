package warplib

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"log"
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
		DefaultFileMode,
	)
	if err != nil {
		m = nil
		return
	}
	// Attempt to decode existing data. If file is empty or corrupt,
	// start fresh with empty items map.
	if decErr := gob.NewDecoder(m.f).Decode(&m.items); decErr != nil {
		if decErr != io.EOF {
			// Log warning for non-empty but corrupt file
			log.Printf("warplib: warning: failed to decode userdata, starting fresh: %v", decErr)
		}
		// Reset to empty map (already initialized, but be explicit)
		m.items = make(ItemsMap)
	}
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
	item.setDAlloc(d)
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
		off, part, err := item.getPartWithError(hash)
		if err != nil {
			d.handlers.ErrorHandler(hash, fmt.Errorf("compile complete: %w", err))
			return
		}
		if part == nil {
			d.handlers.ErrorHandler(hash, fmt.Errorf("compile complete: part not found for hash %q", hash))
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

// persistItems writes items to disk using buffer-first approach.
// Called by encode() which handles locking, or directly by Flush()/Close()
// which must hold m.mu write lock.
// Does NOT call Sync() - caller decides if durability is needed.
func (m *Manager) persistItems() error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(m.items); err != nil {
		return fmt.Errorf("encode items: %w", err)
	}
	if err := m.f.Truncate(0); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	if _, err := m.f.Seek(0, 0); err != nil {
		return fmt.Errorf("seek: %w", err)
	}
	if _, err := m.f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// encode persists items to disk.
// This is a high-frequency operation (called on every progress update),
// so it does NOT call Sync() for performance reasons.
func (m *Manager) encode() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.persistItems()
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
	m.encode()
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
	item.setDAlloc(d)
	// m.UpdateItem(item)
	return
}

// Flush flushes away all the inactive download items.
func (m *Manager) Flush() error {
	// add a write lock to prevent data modification while flushing
	m.mu.Lock()
	defer m.mu.Unlock()
	for hash, item := range m.items {
		// Since item.mu == m.mu, we already hold the lock.
		// Read fields directly without additional locking.
		totalSize := item.TotalSize
		downloaded := item.Downloaded

		// Use getDAlloc() for synchronized access to dAlloc
		dAlloc := item.getDAlloc()

		if totalSize != downloaded && dAlloc != nil {
			continue
		}
		delete(m.items, hash)
		_ = WarpRemoveAll(GetPath(DlDataDir, hash))
	}
	if err := m.persistItems(); err != nil {
		return fmt.Errorf("flush persist: %w", err)
	}
	// Sync to ensure durability - Flush is an explicit user action
	if err := m.f.Sync(); err != nil {
		return fmt.Errorf("flush sync: %w", err)
	}
	return nil
}

// FlushOne flushes away the download item with the given hash.
// Fixed Race 6: Uses write lock for entire operation to prevent TOCTOU.
func (m *Manager) FlushOne(hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, found := m.items[hash]
	if !found {
		return ErrFlushHashNotFound
	}

	// Check download state atomically under manager lock
	if item.TotalSize != item.Downloaded && item.getDAlloc() != nil {
		return ErrFlushItemDownloading
	}

	// Delete from map while holding lock
	delete(m.items, hash)

	if err := m.persistItems(); err != nil {
		// Restore on error
		m.items[hash] = item
		return fmt.Errorf("flush one persist: %w", err)
	}

	if err := m.f.Sync(); err != nil {
		return fmt.Errorf("flush one sync: %w", err)
	}

	// Directory removal is safe after persist (item can't be resumed)
	return WarpRemoveAll(GetPath(DlDataDir, hash))
}

// Close closes the manager safely, ensuring all data is persisted.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Final persist and sync before closing
	if err := m.persistItems(); err != nil {
		// Log but don't fail - still need to close file
		log.Printf("warplib: warning: failed to persist on close: %v", err)
	}
	if err := m.f.Sync(); err != nil {
		log.Printf("warplib: warning: failed to sync on close: %v", err)
	}
	return m.f.Close()
}
