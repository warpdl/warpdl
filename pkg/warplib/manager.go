package warplib

import (
	"bytes"
	"context"
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

// ManagerData is the persistent state of the Manager.
// It wraps items and optional queue state for GOB encoding.
type ManagerData struct {
	Items      ItemsMap
	QueueState *QueueState
}

// Manager is a struct that manages the download items
// and their respective downloaders.
type Manager struct {
	// items is a map of download items
	items ItemsMap
	f     *os.File
	mu    *sync.RWMutex
	// queue manages concurrent download limits (nil if disabled)
	queue *QueueManager
	// queueState stores persisted queue state until queue is initialized
	queueState *QueueState
	// schemeRouter dispatches URL schemes to protocol factories during resume.
	schemeRouter *SchemeRouter
}

// SetSchemeRouter sets the scheme router for protocol dispatch during resume.
// Used by daemon startup to provide the router to the Manager.
func (m *Manager) SetSchemeRouter(r *SchemeRouter) {
	m.schemeRouter = r
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
	// Attempt to decode existing data. Try new format first, fall back to legacy.
	var data ManagerData
	if decErr := gob.NewDecoder(m.f).Decode(&data); decErr != nil {
		if decErr != io.EOF {
			// Try legacy format (ItemsMap only)
			if _, seekErr := m.f.Seek(0, 0); seekErr != nil {
				log.Printf("warplib: warning: failed to seek for legacy decode: %v", seekErr)
			} else if legacyErr := gob.NewDecoder(m.f).Decode(&m.items); legacyErr != nil {
				if legacyErr != io.EOF {
					// Log warning for non-empty but corrupt file
					log.Printf("warplib: warning: failed to decode userdata, starting fresh: %v", legacyErr)
				}
				// Reset to empty map (already initialized, but be explicit)
				m.items = make(ItemsMap)
			}
		} else {
			// Empty file - start fresh
			m.items = make(ItemsMap)
		}
	} else {
		// New format decoded successfully
		m.items = data.Items
		if m.items == nil {
			m.items = make(ItemsMap)
		}
		m.queueState = data.QueueState
		// Validate protocol values for all decoded items.
		// Unknown values indicate the file was created by a newer warpdl version.
		for hash, item := range m.items {
			if item == nil {
				continue
			}
			if err := ValidateProtocol(item.Protocol); err != nil {
				m.f.Close()
				return nil, fmt.Errorf("item %s: %w", hash, err)
			}
		}
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

// SetMaxConcurrentDownloads enables the download queue with a concurrency limit.
// When a slot becomes available for a queued download, onStartDownload is called
// with the hash. The callback should start the download (e.g., via ResumeDownload
// or by getting the item's downloader and calling Start).
// If maxConcurrent is 0 or negative, the queue is disabled.
// If queue state was persisted, it will be restored (waiting items preserved).
func (m *Manager) SetMaxConcurrentDownloads(maxConcurrent int, onStartDownload func(hash string)) {
	if maxConcurrent <= 0 {
		m.queue = nil
		return
	}
	m.queue = NewQueueManager(maxConcurrent, onStartDownload)

	// Restore persisted queue state if available
	if m.queueState != nil {
		// Override maxConcurrent with persisted value if it was set
		// (but keep the new onStartDownload callback)
		m.queue.LoadState(*m.queueState)
		// Override with the new maxConcurrent if different from persisted
		// (user may have changed the flag)
		if maxConcurrent != m.queueState.MaxConcurrent {
			m.queue.mu.Lock()
			m.queue.maxConcurrent = maxConcurrent
			m.queue.mu.Unlock()
		}
		m.queueState = nil // Clear after restoring
	}
}

// GetQueue returns the QueueManager if enabled, or nil if disabled.
func (m *Manager) GetQueue() *QueueManager {
	return m.queue
}

// AddDownloadOpts contains optional parameters for AddDownload.
type AddDownloadOpts struct {
	IsHidden         bool
	IsChildren       bool
	ChildHash        string
	AbsoluteLocation string
	Priority         Priority
}

// AddDownload adds a new download item entry.
// If the queue is enabled, the download is registered with the queue.
// The queue's onStart callback will be invoked when a slot is available
// (immediately if under capacity, or when another download completes).
// The *Downloader is wrapped in an httpProtocolDownloader adapter and stored
// in item.dAlloc as a ProtocolDownloader.
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
	// Wrap the concrete *Downloader in an httpProtocolDownloader adapter.
	// patchHandlers operates on the concrete *Downloader directly, so we
	// patch first, then wrap.
	m.patchHandlers(d, item)

	adapter := &httpProtocolDownloader{
		inner:  d,
		rawURL: d.url,
		probed: true, // fetchInfo was already called by NewDownloader
	}
	item.setDAlloc(adapter)
	m.UpdateItem(item)

	// Register with queue if enabled
	if m.queue != nil {
		m.queue.Add(d.hash, opts.Priority)
	}
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
		// Set Compiled under the item lock to avoid a race with the GOB
		// encoder in persistItems which reads Part fields under the same lock.
		item.mu.Lock()
		part.Compiled = true
		item.mu.Unlock()
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

		// Notify queue that download is complete (use item.Hash, not part hash)
		if m.queue != nil {
			m.queue.OnComplete(item.Hash)
		}

		oDCH(hash, tread)
	}
}

// AddProtocolDownload adds a new download item for a non-HTTP protocol downloader.
// cleanURL is the URL with credentials stripped — safe for GOB persistence.
// proto identifies the protocol (ProtoFTP, ProtoFTPS, ProtoSFTP).
func (m *Manager) AddProtocolDownload(pd ProtocolDownloader, probe ProbeResult, cleanURL string, proto Protocol, handlers *Handlers, opts *AddDownloadOpts) error {
	if opts == nil {
		opts = &AddDownloadOpts{}
	}
	item, err := newItem(
		m.mu,
		pd.GetFileName(),
		cleanURL, // credential-stripped URL — safe to persist
		pd.GetDownloadDirectory(),
		pd.GetHash(),
		ContentLength(probe.ContentLength),
		probe.Resumable,
		&itemOpts{
			AbsoluteLocation: opts.AbsoluteLocation,
			Child:            opts.IsChildren,
			Hide:             opts.IsHidden,
			ChildHash:        opts.ChildHash,
		},
	)
	if err != nil {
		return err
	}
	item.Protocol = proto

	// Wrap handlers with item-update callbacks
	m.patchProtocolHandlers(handlers, item)

	item.setDAlloc(pd)
	m.UpdateItem(item)

	if m.queue != nil {
		m.queue.Add(pd.GetHash(), opts.Priority)
	}
	return nil
}

// patchProtocolHandlers wraps handler callbacks to update Item state.
// This is the protocol-agnostic equivalent of patchHandlers for non-HTTP downloaders.
// The wrapped handlers are mutated in-place (same as patchHandlers pattern).
//
// FTP-relevant wrappers:
//   - SpawnPartHandler: FTP calls this once with [0, fileSize-1]
//   - DownloadProgressHandler: FTP calls this on every Write
//   - DownloadCompleteHandler: FTP calls this with MAIN_HASH on success
//
// HTTP-only wrappers (included for future protocol support, never called by FTP):
//   - RespawnPartHandler: HTTP work-stealing only (FTP has single stream)
//   - CompileCompleteHandler: HTTP part compilation only (FTP has no parts to compile)
func (m *Manager) patchProtocolHandlers(h *Handlers, item *Item) {
	if h == nil {
		return
	}
	oSPH := h.SpawnPartHandler
	h.SpawnPartHandler = func(hash string, ioff, foff int64) {
		item.addPart(hash, ioff, foff)
		m.UpdateItem(item)
		if oSPH != nil {
			oSPH(hash, ioff, foff)
		}
	}
	oRPH := h.RespawnPartHandler
	h.RespawnPartHandler = func(hash string, partIoff, ioffNew, foffNew int64) {
		item.addPart(hash, partIoff, foffNew)
		m.UpdateItem(item)
		if oRPH != nil {
			oRPH(hash, partIoff, ioffNew, foffNew)
		}
	}
	oPH := h.DownloadProgressHandler
	h.DownloadProgressHandler = func(hash string, nread int) {
		item.mu.Lock()
		item.Downloaded += ContentLength(nread)
		item.mu.Unlock()
		m.UpdateItem(item)
		if oPH != nil {
			oPH(hash, nread)
		}
	}
	oCCH := h.CompileCompleteHandler
	h.CompileCompleteHandler = func(hash string, tread int64) {
		off, part, err := item.getPartWithError(hash)
		if err != nil {
			if h.ErrorHandler != nil {
				h.ErrorHandler(hash, fmt.Errorf("compile complete: %w", err))
			}
			return
		}
		if part == nil {
			if h.ErrorHandler != nil {
				h.ErrorHandler(hash, fmt.Errorf("compile complete: part not found for hash %q", hash))
			}
			return
		}
		// Set Compiled under the item lock to avoid a race with the GOB
		// encoder in persistItems which reads Part fields under the same lock.
		item.mu.Lock()
		part.Compiled = true
		item.mu.Unlock()
		item.savePart(off, part)
		if oCCH != nil {
			oCCH(hash, tread)
		}
	}
	oDCH := h.DownloadCompleteHandler
	h.DownloadCompleteHandler = func(hash string, tread int64) {
		if hash != MAIN_HASH {
			return
		}
		item.mu.Lock()
		item.Parts = nil
		item.Downloaded = item.TotalSize
		item.mu.Unlock()
		m.UpdateItem(item)
		if m.queue != nil {
			m.queue.OnComplete(item.Hash)
		}
		if oDCH != nil {
			oDCH(hash, tread)
		}
	}
}

// persistItems writes items to disk using buffer-first approach.
// Called by encode() which handles locking, or directly by Flush()/Close()
// which must hold m.mu write lock.
// Does NOT call Sync() - caller decides if durability is needed.
func (m *Manager) persistItems() error {
	data := ManagerData{
		Items: m.items,
	}
	// Include queue state if queue is enabled
	if m.queue != nil {
		state := m.queue.GetState()
		data.QueueState = &state
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(data); err != nil {
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
// Uses thread-safe getters for Downloaded/TotalSize to avoid data races.
func (m *Manager) GetIncompleteItems() []*Item {
	var items = []*Item{}
	for _, item := range m.GetItems() {
		if item.GetTotalSize() == item.GetDownloaded() {
			continue
		}
		items = append(items, item)
	}
	return items
}

// GetCompletedItems returns all the completed items in the manager.
// Uses thread-safe getters for Downloaded/TotalSize to avoid data races.
func (m *Manager) GetCompletedItems() []*Item {
	var items = []*Item{}
	for _, item := range m.GetItems() {
		if item.GetTotalSize() != item.GetDownloaded() {
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
	// SpeedLimit specifies the maximum download speed in bytes per second.
	// If zero, no limit is applied.
	SpeedLimit int64
}

// ResumeDownload resumes a download item.
// For HTTP items, it validates segment-file integrity and creates an HTTP downloader.
// For FTP/FTPS/SFTP items, it skips segment-file checks (single-stream to dest file)
// and dispatches through SchemeRouter to create a protocol-specific downloader.
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

	// Protocol guard: validate integrity differently per protocol.
	// HTTP uses segment directories + part files; FTP writes directly to dest file.
	switch item.Protocol {
	case ProtoHTTP:
		// HTTP: validate segment directory + part files (existing behavior)
		if err = validateDownloadIntegrity(item); err != nil {
			return
		}
	case ProtoFTP, ProtoFTPS, ProtoSFTP:
		// FTP/SFTP: no segment files exist. Only verify destination file if download started.
		if item.Downloaded > 0 {
			mainFile := item.GetAbsolutePath()
			if !fileExists(mainFile) {
				err = fmt.Errorf("%w: destination file missing for %s resume: %s", ErrDownloadDataMissing, item.Protocol, mainFile)
				return
			}
		}
	default:
		err = fmt.Errorf("resume not supported for protocol %s", item.Protocol)
		return
	}

	// Dispatch based on protocol
	switch item.Protocol {
	case ProtoFTP, ProtoFTPS, ProtoSFTP:
		// FTP/FTPS/SFTP resume via SchemeRouter
		if m.schemeRouter == nil {
			err = fmt.Errorf("scheme router not initialized for %s resume", item.Protocol)
			return
		}
		var pd ProtocolDownloader
		pd, err = m.schemeRouter.NewDownloader(item.Url, &DownloaderOpts{
			FileName:          item.Name,
			DownloadDirectory: item.DownloadLocation,
		})
		if err != nil {
			return
		}
		// Probe to get file metadata (size, etc.)
		if _, err = pd.Probe(context.Background()); err != nil {
			return
		}
		// Patch handlers for item state updates
		if opts.Handlers == nil {
			opts.Handlers = &Handlers{}
		}
		m.patchProtocolHandlers(opts.Handlers, item)
		item.setDAlloc(pd)
		m.UpdateItem(item)

	default:
		// HTTP resume path (existing behavior — unchanged)
		if item.Headers == nil {
			item.Headers = make(Headers, 0)
		}
		if opts.Headers != nil {
			for _, newHeader := range opts.Headers {
				item.Headers.Update(newHeader.Key, newHeader.Value)
			}
		}
		var d *Downloader
		d, err = initDownloader(client, hash, item.Url, item.TotalSize, &DownloaderOpts{
			ForceParts:        opts.ForceParts,
			MaxConnections:    opts.MaxConnections,
			MaxSegments:       opts.MaxSegments,
			Handlers:          opts.Handlers,
			FileName:          item.Name,
			DownloadDirectory: item.DownloadLocation,
			Headers:           item.Headers,
			RetryConfig:       opts.RetryConfig,
			RequestTimeout:    opts.RequestTimeout,
			SpeedLimit:        opts.SpeedLimit,
		})
		if err != nil {
			return
		}
		m.patchHandlers(d, item)
		// Wrap the concrete *Downloader in an httpProtocolDownloader adapter.
		adapter := &httpProtocolDownloader{
			inner:  d,
			rawURL: item.Url,
			probed: true, // initDownloader already has the state
		}
		item.setDAlloc(adapter)
		m.UpdateItem(item)
	}
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
