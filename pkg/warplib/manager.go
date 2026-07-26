package warplib

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Default download data directory
var __USERDATA_FILE_NAME string

const managerDataFileMode os.FileMode = 0600

var renameManagerFile = WarpRename
var syncManagerParentDirectory = syncManagerDirectory

// managerStoreCommittedError reports a failure after the new snapshot became
// the live userdata file. Callers must keep the corresponding in-memory
// mutation instead of rolling it back to a state that no longer exists on
// disk.
type managerStoreCommittedError struct {
	err error
}

func (e *managerStoreCommittedError) Error() string {
	return e.err.Error()
}

func (e *managerStoreCommittedError) Unwrap() error {
	return e.err
}

func managerStoreCommitSucceeded(err error) bool {
	var committedErr *managerStoreCommittedError
	return errors.As(err, &committedErr)
}

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
	mu    *sync.RWMutex
	// dataPath is retained after the initial decode. Persistence uses
	// same-directory temporary files and atomic replacement rather than
	// keeping the destination inode open and truncating it in place.
	dataPath string
	// closed is guarded by mu. Post-Close updates remain valid in memory but
	// are intentionally not written to disk.
	closed bool
	// queue manages concurrent download limits (nil if disabled). It is
	// published atomically because completion/progress callbacks read it while
	// daemon startup or reconfiguration may install/disable the queue.
	queue atomic.Pointer[QueueManager]
	// queueState stores persisted queue state until queue is initialized
	queueState *QueueState
	// schemeRouter dispatches URL schemes to protocol factories during resume.
	schemeRouter atomic.Pointer[SchemeRouter]
	// persister coalesces high-frequency UpdateItem calls into bounded
	// disk writes. See persist.go.
	//
	// Accessed through an atomic pointer so Close() can swap it to nil
	// without racing the hot UpdateItem/UpdateItemAsync paths on
	// concurrent goroutines.
	persister atomic.Pointer[persister]
	// transferMu guards admission and the active synchronous/goroutine count
	// for Manager-owned transfer work. transferCond uses the same mutex so
	// closing admission and observing a drained count are one atomic protocol.
	transferMu      sync.Mutex
	transferCond    *sync.Cond
	transferCtx     context.Context
	transferCancel  context.CancelFunc
	transferActive  int
	transferClosing bool
}

// SetSchemeRouter sets the scheme router for protocol dispatch during resume.
// Used by daemon startup to provide the router to the Manager.
func (m *Manager) SetSchemeRouter(r *SchemeRouter) {
	m.schemeRouter.Store(r)
}

// InitManager creates a new manager instance.
func InitManager() (m *Manager, err error) {
	m = &Manager{
		items:    make(ItemsMap),
		mu:       new(sync.RWMutex),
		dataPath: __USERDATA_FILE_NAME,
	}
	m.initTransferLifetime()
	f, err := WarpOpenFile(
		m.dataPath,
		os.O_RDWR|os.O_CREATE,
		managerDataFileMode,
	)
	if err != nil {
		m = nil
		return
	}
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	if chmodErr := WarpChmod(m.dataPath, managerDataFileMode); chmodErr != nil {
		return nil, fmt.Errorf("secure userdata permissions: %w", chmodErr)
	}

	// Attempt to decode existing data. Try new format first, fall back to legacy.
	var data ManagerData
	if decErr := gob.NewDecoder(f).Decode(&data); decErr != nil {
		if decErr != io.EOF {
			// Try legacy format (ItemsMap only)
			if _, seekErr := f.Seek(0, 0); seekErr != nil {
				log.Printf("warplib: warning: failed to seek for legacy decode: %v", seekErr)
			} else if legacyErr := gob.NewDecoder(f).Decode(&m.items); legacyErr != nil {
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
				return nil, fmt.Errorf("item %s: %w", hash, err)
			}
		}
	}
	if closeErr := f.Close(); closeErr != nil {
		return nil, fmt.Errorf("close userdata after decode: %w", closeErr)
	}
	f = nil
	m.populateMemPart()
	// Start persistence only after decoding and validation succeeds. This
	// avoids leaking a writer goroutine when initialization returns an error.
	m.persister.Store(newPersister(
		m.encodeLocked,
		DefaultPersistInterval,
		func(format string, args ...any) {
			log.Printf("warplib: "+format, args...)
		},
	))
	return
}

func (m *Manager) populateMemPart() {
	for _, item := range m.items {
		if item == nil {
			continue
		}
		item.mu = m.mu
		if item.memPart == nil {
			item.memPart = make(map[string]int64)
		}
		for ioff, part := range item.Parts {
			if part != nil {
				item.memPart[part.Hash] = ioff
			}
		}
	}
}

// reconcileQueueState removes queue entries that cannot be runnable after a
// restart. A crash may leave an item in QueueState.Active even though the
// completion snapshot (Downloaded == TotalSize) reached disk first; restoring
// that stale slot would download the completed file again. Missing items are
// equally non-runnable and must not consume queue capacity.
func (m *Manager) reconcileQueueState(state QueueState) (QueueState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keep := func(hash string) bool {
		item := m.items[hash]
		if item == nil {
			return false
		}
		switch {
		case item.TotalSize < 0:
			// Unknown-size downloads remain runnable until their completion
			// wrapper records the final size.
			return true
		case item.TotalSize == 0:
			// New zero-byte candidates have a non-nil empty parts map.
			// Completion wrappers clear Parts, which gives persisted empty
			// files an unambiguous terminal representation.
			return item.Parts != nil
		default:
			return item.Downloaded < item.TotalSize
		}
	}
	filter := func(items []QueuedItemState) ([]QueuedItemState, bool) {
		filtered := make([]QueuedItemState, 0, len(items))
		changed := false
		for _, item := range items {
			if !keep(item.Hash) {
				changed = true
				continue
			}
			filtered = append(filtered, item)
		}
		return filtered, changed
	}

	active, activeChanged := filter(state.Active)
	waiting, waitingChanged := filter(state.Waiting)
	if !activeChanged && !waitingChanged {
		return state, false
	}
	state.Active = active
	state.Waiting = waiting
	return state, true
}

// SetMaxConcurrentDownloads enables the download queue with a concurrency limit.
// When a slot becomes available for a queued download, onStartDownload is called
// with the hash. The callback should start the download (e.g., via ResumeDownload
// or by getting the item's downloader and calling Start).
// Zero means unlimited concurrency while retaining durable queue lifecycle
// state. A negative value disables the queue.
// If queue state was persisted, it will be restored (waiting items preserved).
func (m *Manager) SetMaxConcurrentDownloads(maxConcurrent int, onStartDownload func(hash string)) {
	var onStart func(QueueActivation)
	if onStartDownload != nil {
		onStart = func(activation QueueActivation) {
			onStartDownload(activation.Hash())
		}
	}
	m.setMaxConcurrentDownloads(maxConcurrent, onStart)
}

// SetMaxConcurrentDownloadsWithActivation configures the queue and passes an
// exact activation lease to each start callback. Daemon lifecycle code uses
// this form to reject cancellation/re-add ABA races during slow reconstruction.
func (m *Manager) SetMaxConcurrentDownloadsWithActivation(
	maxConcurrent int,
	onStartDownload func(QueueActivation),
) {
	m.setMaxConcurrentDownloads(maxConcurrent, onStartDownload)
}

func (m *Manager) setMaxConcurrentDownloads(
	maxConcurrent int,
	onStartDownload func(QueueActivation),
) {
	if maxConcurrent < 0 {
		m.queue.Store(nil)
		if p := m.persister.Load(); p != nil {
			p.markDirty()
			if err := p.flush(); err != nil {
				log.Printf("warplib: warning: persist disabled queue state: %v", err)
			}
		}
		return
	}
	queue := newQueueManagerWithActivation(maxConcurrent, onStartDownload)
	queue.SetOnChange(func() {
		if p := m.persister.Load(); p != nil {
			p.markDirty()
			// Queue transitions are infrequent lifecycle events, not progress
			// hot-path updates. Persist them synchronously so pause/order/slot
			// changes are not lost if the daemon exits inside the debounce
			// window.
			if err := p.flush(); err != nil {
				log.Printf("warplib: warning: persist queue state: %v", err)
			}
		}
	})
	m.queue.Store(queue)

	// Restore persisted queue state if available
	if m.queueState != nil {
		restoredState, _ := m.reconcileQueueState(*m.queueState)
		// Override maxConcurrent with persisted value if it was set
		// (but keep the new onStartDownload callback)
		// LoadState persists the reconciled waiting snapshot synchronously
		// before Resume can promote any surviving entry.
		queue.LoadState(restoredState)
		// Override with the new maxConcurrent if different from persisted
		// (user may have changed the flag)
		if maxConcurrent != restoredState.MaxConcurrent {
			queue.mu.Lock()
			queue.maxConcurrent = maxConcurrent
			queue.mu.Unlock()
			queue.notifyChange()
		}
		m.queueState = nil // Clear after restoring

		// LoadState converts prior active items to waiting. If the queue was
		// running before shutdown, immediately fill available slots so the
		// caller can reconstruct and restart those downloads.
		if !queue.IsPaused() {
			queue.Resume()
		}
		return
	}
	queue.notifyChange()
}

// GetQueue returns the QueueManager if enabled, or nil if disabled.
func (m *Manager) GetQueue() *QueueManager {
	return m.queue.Load()
}

// ReleaseQueueSlot removes hash from the active or waiting queue. Active
// removal starts the next waiting item when capacity becomes available.
func (m *Manager) ReleaseQueueSlot(hash string) bool {
	queue := m.queue.Load()
	if queue == nil {
		return false
	}
	return queue.Remove(hash)
}

// CloseWaitingDownloader closes and clears an item's live downloader only if
// it is still waiting for a queue slot. Queue membership is held stable for
// the duration of CloseDownloader, so a concurrent slot release cannot start
// the downloader while it is being detached. The item remains queued and is
// freshly reconstructed by the queue's onStart callback when promoted.
func (m *Manager) CloseWaitingDownloader(hash string) (bool, error) {
	queue := m.queue.Load()
	if queue == nil {
		return false, nil
	}
	item := m.GetItem(hash)
	if item == nil {
		return false, ErrDownloadNotFound
	}
	return queue.runIfWaiting(hash, item.CloseDownloader)
}

// RemoveWaitingDownloader atomically removes a waiting queue entry and closes
// its allocation. If promotion already won, it returns false without touching
// the now-active allocation so the caller can request a normal active stop.
func (m *Manager) RemoveWaitingDownloader(hash string) (bool, error) {
	queue := m.queue.Load()
	if queue == nil {
		return false, nil
	}
	item := m.GetItem(hash)
	if item == nil {
		return false, ErrDownloadNotFound
	}
	return queue.removeIfWaiting(hash, item.CloseDownloader)
}

// AddDownloadOpts contains optional parameters for AddDownload.
type AddDownloadOpts struct {
	IsHidden         bool
	IsChildren       bool
	ChildHash        string
	AbsoluteLocation string
	Priority         Priority
	SkipQueue        bool
	// ExcludePersistedHeaders lists request-header names that must remain
	// available to the live downloader but must not be copied into Item or
	// userdata.warp. Header names are matched case-insensitively.
	//
	// The API uses this for browser-imported Cookie values: the initial
	// request still receives the cookie, while only CookieSourcePath is
	// persisted so later runs can safely re-import it.
	ExcludePersistedHeaders []string
	// SSHKeyPath is the SSH key path to persist in Item for SFTP resume.
	// Empty means default key paths are tried on resume.
	SSHKeyPath string
	// TransferConfig carries non-secret reconstruction settings that cannot
	// be recovered from the ProtocolDownloader interface (notably proxy and
	// credential requirements).
	TransferConfig TransferConfig
}

func filterPersistedHeaders(headers Headers, excluded []string) Headers {
	if len(headers) == 0 {
		return nil
	}
	excludedNames := make(map[string]struct{}, len(excluded))
	for _, key := range excluded {
		excludedNames[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}

	filtered := make(Headers, 0, len(headers))
	for _, header := range headers {
		if _, omit := excludedNames[strings.ToLower(strings.TrimSpace(header.Key))]; omit {
			continue
		}
		filtered = append(filtered, header)
	}
	return filtered
}

func transferConfigFromDownloader(d *Downloader) TransferConfig {
	config := TransferConfig{
		ForceParts:          d.force,
		NumBaseParts:        d.numBaseParts,
		MaxConnections:      d.maxConn,
		MaxSegments:         d.maxParts,
		Overwrite:           d.overwrite,
		LockFileName:        d.lockFileName,
		RequestTimeout:      d.requestTimeout,
		MaxFileSize:         d.maxFileSize,
		SpeedLimit:          d.speedLimit,
		DisableWorkStealing: !d.enableWorkStealing,
	}
	if d.retryConfig != nil {
		retryConfig := *d.retryConfig
		config.RetryConfig = &retryConfig
	}
	if d.checksumConfig != nil {
		checksumConfig := *d.checksumConfig
		config.ChecksumConfig = &checksumConfig
	}
	return config
}

// AddDownload adds a new download item entry.
// If the queue is enabled, the download is registered with the queue.
// The queue's onStart callback will be invoked when a slot is available
// (immediately if under capacity, or when another download completes).
// The *Downloader is wrapped in an httpProtocolDownloader adapter and stored
// in item.dAlloc as a ProtocolDownloader.
func (m *Manager) AddDownload(d *Downloader, opts *AddDownloadOpts) (err error) {
	_, done, admitted := m.admitTransfer()
	if !admitted {
		return ErrManagerShuttingDown
	}
	defer done()
	if opts == nil {
		opts = &AddDownloadOpts{}
	}
	transferConfig := transferConfigFromDownloader(d)
	safeProxyURL, proxyCredentialsRequired, err := SanitizeProxyURLForPersistence(opts.TransferConfig.ProxyURL)
	if err != nil {
		return fmt.Errorf("persist proxy configuration: %w", err)
	}
	transferConfig.ProxyURL = safeProxyURL
	transferConfig.ProxyCredentialsRequired =
		opts.TransferConfig.ProxyCredentialsRequired || proxyCredentialsRequired
	item, err := newItem(
		m.mu,
		d.fileName,
		d.persistedURL(),
		d.dlLoc,
		d.hash,
		d.contentLength,
		d.resumable,
		&itemOpts{
			AbsoluteLocation:  opts.AbsoluteLocation,
			Child:             opts.IsChildren,
			Hide:              opts.IsHidden,
			ChildHash:         opts.ChildHash,
			Headers:           filterPersistedHeaders(d.persistedHeaders(), opts.ExcludePersistedHeaders),
			PluginHeaderNames: sortedPluginHeaderNames(d.pluginHeaderNames),
			ResourceETag:      d.resourceETag,
			TransferConfig:    transferConfig,
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
		rawURL: d.persistedURL(),
		probed: true, // fetchInfo was already called by NewDownloader
	}
	item.setDAlloc(adapter)
	m.UpdateItem(item)

	// Register with queue if enabled
	if queue := m.queue.Load(); queue != nil && !opts.SkipQueue {
		queue.Add(d.hash, opts.Priority)
	}
	return
}

// patchHandlers patches the handlers of the downloader to update the item.
func (m *Manager) patchHandlers(d *Downloader, item *Item) {
	oDClaimH := d.handlers.DestinationClaimedHandler
	d.handlers.DestinationClaimedHandler = func() error {
		if err := m.mutateItem(item.Hash, func(managedItem *Item) {
			managedItem.DestinationClaimed = true
		}); err != nil {
			return err
		}
		if oDClaimH != nil {
			return oDClaimH()
		}
		return nil
	}
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
		// Hot path: coalesce writes via the background persister.
		m.UpdateItemAsync(item)
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
		if item.TotalSize < 0 {
			item.TotalSize = ContentLength(tread)
		}
		item.Downloaded = item.TotalSize
		item.DestinationClaimed = false
		item.mu.Unlock()
		m.UpdateItem(item)

		// Notify queue that download is complete (use item.Hash, not part hash)
		if queue := m.queue.Load(); queue != nil {
			queue.OnComplete(item.Hash)
		}

		oDCH(hash, tread)
	}
	oDSH := d.handlers.DownloadStoppedHandler
	d.handlers.DownloadStoppedHandler = func() {
		if queue := m.queue.Load(); queue != nil {
			queue.OnStopped(item.Hash)
		}
		oDSH()
	}
}

// AddProtocolDownload adds a new download item for a non-HTTP protocol downloader.
// cleanURL is the URL with credentials stripped — safe for GOB persistence.
// proto identifies the protocol (ProtoFTP, ProtoFTPS, ProtoSFTP).
func (m *Manager) AddProtocolDownload(pd ProtocolDownloader, probe ProbeResult, cleanURL string, proto Protocol, handlers *Handlers, opts *AddDownloadOpts) error {
	_, done, admitted := m.admitTransfer()
	if !admitted {
		return ErrManagerShuttingDown
	}
	defer done()
	if opts == nil {
		opts = &AddDownloadOpts{}
	}
	transferConfig := cloneTransferConfig(opts.TransferConfig)
	safeProxyURL, proxyCredentialsRequired, err := SanitizeProxyURLForPersistence(transferConfig.ProxyURL)
	if err != nil {
		return fmt.Errorf("persist proxy configuration: %w", err)
	}
	transferConfig.ProxyURL = safeProxyURL
	transferConfig.ProxyCredentialsRequired =
		transferConfig.ProxyCredentialsRequired || proxyCredentialsRequired
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
			TransferConfig:   transferConfig,
		},
	)
	if err != nil {
		return err
	}
	item.Protocol = proto
	item.SSHKeyPath = opts.SSHKeyPath
	item.TransferConfig.MaxConnections = pd.GetMaxConnections()
	item.TransferConfig.MaxSegments = pd.GetMaxParts()

	// Wrap handlers with item-update callbacks
	m.patchProtocolHandlers(handlers, item)

	item.setResumeHandlers(handlers)
	item.setDAlloc(pd)
	m.UpdateItem(item)

	if queue := m.queue.Load(); queue != nil && !opts.SkipQueue {
		queue.Add(pd.GetHash(), opts.Priority)
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
		// Hot path: coalesce writes via the background persister.
		m.UpdateItemAsync(item)
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
		if queue := m.queue.Load(); queue != nil {
			queue.OnComplete(item.Hash)
		}
		if oDCH != nil {
			oDCH(hash, tread)
		}
	}
	oDSH := h.DownloadStoppedHandler
	h.DownloadStoppedHandler = func() {
		if queue := m.queue.Load(); queue != nil {
			queue.OnStopped(item.Hash)
		}
		if oDSH != nil {
			oDSH()
		}
	}
}

// persistItems writes a complete snapshot using a same-directory temporary
// file, fsync, and atomic rename. The previous userdata file remains intact
// unless the replacement snapshot has been fully encoded and synced.
// The caller must hold m.mu for writing.
func (m *Manager) persistItems() error {
	var queueState *QueueState
	if queue := m.queue.Load(); queue != nil {
		state := queue.GetState()
		queueState = &state
	}
	return m.persistItemsWithQueueState(queueState)
}

// persistItemsWithQueueState writes a snapshot using a queue state already
// captured by a caller holding queue.mu. This avoids re-locking the queue while
// manager deletion and queue-member removal commit as one snapshot.
// The caller must hold m.mu for writing.
func (m *Manager) persistItemsWithQueueState(queueState *QueueState) error {
	if m.closed {
		return nil
	}
	data := ManagerData{
		Items:      m.items,
		QueueState: queueState,
	}
	if err := writeManagerDataAtomic(m.dataPath, data); err != nil {
		return fmt.Errorf("persist manager data: %w", err)
	}
	return nil
}

func writeManagerDataAtomic(path string, data ManagerData) (err error) {
	if path == "" {
		return errors.New("userdata path is empty")
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(NormalizePath(dir), "."+base+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary snapshot: %w", err)
	}
	tmpPath := tmp.Name()
	keepTemp := true
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
		}
		if keepTemp {
			_ = WarpRemove(tmpPath)
		}
	}()

	if err = WarpChmod(tmpPath, managerDataFileMode); err != nil {
		return fmt.Errorf("secure temporary snapshot: %w", err)
	}
	if err = gob.NewEncoder(tmp).Encode(data); err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary snapshot: %w", err)
	}
	if err = tmp.Close(); err != nil {
		tmp = nil
		return fmt.Errorf("close temporary snapshot: %w", err)
	}
	tmp = nil

	if err = renameManagerFile(tmpPath, path); err != nil {
		return fmt.Errorf("replace userdata: %w", err)
	}
	keepTemp = false

	if err = syncManagerParentDirectory(dir); err != nil {
		return &managerStoreCommittedError{
			err: fmt.Errorf("sync userdata directory: %w", err),
		}
	}
	return nil
}

func syncManagerDirectory(dir string) error {
	// MoveFileEx with WRITE_THROUGH is used by WarpRename on Windows; opening
	// a directory for fsync is not portable there.
	if runtime.GOOS == "windows" {
		return nil
	}
	dirFile, err := WarpOpen(dir)
	if err != nil {
		return err
	}
	return errors.Join(dirFile.Sync(), dirFile.Close())
}

// encodeLocked persists items to disk under the manager's write lock.
// Used by the background persister; call sites that already hold the lock
// should use persistItems directly.
func (m *Manager) encodeLocked() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.persistItems()
}

// encode persists items to disk immediately, bypassing the background
// persister. Kept for callers that need guaranteed durability.
func (m *Manager) encode() error {
	if p := m.persister.Load(); p != nil {
		// Synchronously drain any pending dirty state so the call returns
		// with a fully persisted view.
		return p.flush()
	}
	return m.encodeLocked()
}

// mapItem maps the item to the manager's items map.
func (m *Manager) mapItem(item *Item) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Item.mu is initialized once, before the item is published in m.items.
	// Rewriting the pointer on every progress UpdateItem races getters that
	// first read i.mu to acquire the item lock.
	if item.mu == nil {
		item.mu = m.mu
	}
	if item.memPart == nil {
		item.memPart = make(map[string]int64)
		for offset, part := range item.Parts {
			if part != nil {
				item.memPart[part.Hash] = offset
			}
		}
	}
	m.items[item.Hash] = item
}

// UpdateItem updates the item in the manager's items map and persists the
// state synchronously. Callers on a hot path (progress handler) should
// prefer UpdateItemAsync which defers persistence to the background writer.
//
// Safe to call after Close: the update is applied to the in-memory map,
// but persistence becomes a no-op because the persister has been shut
// down. No panic.
func (m *Manager) UpdateItem(item *Item) {
	m.mapItem(item)
	m.persistCurrentItems()
}

// persistCurrentItems synchronously snapshots the manager's current map
// without publishing a caller-supplied Item pointer. Reconstruction commit
// uses this after releasing Item lifecycle locks: if a same-hash replacement
// won meanwhile, persistence must capture that newer mapping rather than
// resurrecting the stale Item.
func (m *Manager) persistCurrentItems() {
	if p := m.persister.Load(); p != nil {
		// Ensure any pending async dirt is flushed first so callers that
		// expect synchronous durability (AddDownload, completion handlers)
		// still get it. markDirty + flush is equivalent to a direct encode.
		p.markDirty()
		if err := p.flush(); err != nil {
			log.Printf("warplib: warning: persister flush in UpdateItem: %v", err)
		}
		return
	}
	// After Close or when the Manager was constructed without a
	// persister - fall back to a best-effort direct write. The open-file
	// check must happen under m.mu (Close sets m.f = nil while holding
	// it), so it lives inside persistItems rather than here; a bare read
	// of m.f at this point races shutdown.
	_ = m.encodeLocked()
}

// UpdateItemAsync records that the given item is dirty but defers the
// actual disk write to the background persister. Bursts of updates
// (e.g. progress callbacks on every 32 KB chunk) are coalesced into at
// most one write per DefaultPersistInterval.
//
// Safe to call after Close: the in-memory update happens, persistence
// becomes a no-op. No panic.
func (m *Manager) UpdateItemAsync(item *Item) {
	m.mapItem(item)
	if p := m.persister.Load(); p != nil {
		p.markDirty()
		return
	}
	// No persister available - best-effort direct encode. persistItems
	// quietly drops the write if the file is already closed; checking
	// m.f here without the lock would race Close (which nils it under
	// m.mu).
	_ = m.encodeLocked()
}

// GetScheduledItems returns all items with ScheduleState == "scheduled".
// Thread-safe: acquires read lock on the manager.
func (m *Manager) GetScheduledItems() []*Item {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var items []*Item
	for _, item := range m.items {
		if item.ScheduleState == ScheduleStateScheduled {
			items = append(items, item)
		}
	}
	return items
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*Item, 0, len(m.items))
	for _, item := range m.items {
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
	return m.items[hash]
}

// ScheduleInfo is a lock-safe snapshot of fields used by scheduler and API
// orchestration. It prevents callers from racing the manager's GOB encoder by
// reading mutable Item fields directly.
type ScheduleInfo struct {
	Hash             string
	Name             string
	URL              string
	CookieSourcePath string
	ScheduledAt      time.Time
	CronExpr         string
	State            ScheduleState
	Downloaded       ContentLength
	TotalSize        ContentLength
}

// GetScheduleInfo returns a consistent scheduling snapshot.
func (m *Manager) GetScheduleInfo(hash string) (ScheduleInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item := m.items[hash]
	if item == nil {
		return ScheduleInfo{}, false
	}
	return ScheduleInfo{
		Hash:             item.Hash,
		Name:             item.Name,
		URL:              item.Url,
		CookieSourcePath: item.CookieSourcePath,
		ScheduledAt:      item.ScheduledAt,
		CronExpr:         item.CronExpr,
		State:            item.ScheduleState,
		Downloaded:       item.Downloaded,
		TotalSize:        item.TotalSize,
	}, true
}

func (m *Manager) mutateItem(hash string, mutate func(*Item)) error {
	m.mu.Lock()
	item := m.items[hash]
	if item == nil {
		m.mu.Unlock()
		return ErrDownloadNotFound
	}
	mutate(item)
	m.mu.Unlock()

	if p := m.persister.Load(); p != nil {
		p.markDirty()
		if err := p.flush(); err != nil {
			return fmt.Errorf("persist item mutation: %w", err)
		}
		return nil
	}
	return m.encodeLocked()
}

// ConfigureSchedule atomically updates all persisted schedule fields.
func (m *Manager) ConfigureSchedule(hash string, scheduledAt time.Time, cronExpr string, state ScheduleState) error {
	return m.mutateItem(hash, func(item *Item) {
		item.ScheduledAt = scheduledAt
		item.CronExpr = cronExpr
		item.ScheduleState = state
	})
}

// SetScheduleState atomically transitions a schedule without changing its
// expression or next occurrence.
func (m *Manager) SetScheduleState(hash string, state ScheduleState) error {
	return m.mutateItem(hash, func(item *Item) {
		item.ScheduleState = state
	})
}

// SetScheduleStateIf changes a schedule state only when its current value is
// one of expected. The comparison and mutation share the manager lock, which
// lets scheduler triggers and explicit cancellation establish one durable
// winner without a read-then-write race.
func (m *Manager) SetScheduleStateIf(hash string, state ScheduleState, expected ...ScheduleState) (bool, error) {
	m.mu.Lock()
	item := m.items[hash]
	if item == nil {
		m.mu.Unlock()
		return false, ErrDownloadNotFound
	}
	matches := false
	for _, candidate := range expected {
		if item.ScheduleState == candidate {
			matches = true
			break
		}
	}
	if !matches {
		m.mu.Unlock()
		return false, nil
	}
	item.ScheduleState = state
	m.mu.Unlock()

	if p := m.persister.Load(); p != nil {
		p.markDirty()
		if err := p.flush(); err != nil {
			return true, fmt.Errorf("persist item mutation: %w", err)
		}
		return true, nil
	}
	return true, m.encodeLocked()
}

// SetCookieSourcePath records the browser cookie source used for future
// scheduled or resumed requests.
func (m *Manager) SetCookieSourcePath(hash, sourcePath string) error {
	return m.mutateItem(hash, func(item *Item) {
		item.CookieSourcePath = sourcePath
	})
}

// RenameItem updates the persisted output name under the Item lock.
func (m *Manager) RenameItem(hash, name string) error {
	return m.mutateItem(hash, func(item *Item) {
		item.Name = name
	})
}

// ResumeDownloadOpts contains optional parameters for ResumeDownload.
type ResumeDownloadOpts struct {
	ForceParts bool
	// Fresh reconstructs a downloader for a new transfer using the persisted
	// Item metadata. It is used by recurring schedules after a prior
	// occurrence completed and by restored queued/scheduled items that never
	// created part state.
	Fresh bool
	// MaxConnections sets the maximum number of parallel
	// network connections to be used for the downloading the file.
	MaxConnections int32
	// MaxSegments sets the maximum number of file segments
	// to be created for the downloading the file.
	MaxSegments int32
	Headers     Headers
	// TransientHeaders are applied to the reconstructed downloader without
	// mutating Item.Headers or userdata.warp. Use this for freshly imported
	// credentials such as browser cookies.
	TransientHeaders Headers
	Handlers         *Handlers
	// RetryConfig configures retry behavior for transient errors.
	// If nil, DefaultRetryConfig() is used.
	RetryConfig *RetryConfig
	// RequestTimeout specifies the timeout for individual HTTP requests.
	// If zero, no per-request timeout is applied.
	RequestTimeout time.Duration
	// SpeedLimit specifies the maximum download speed in bytes per second.
	// If zero, no limit is applied.
	SpeedLimit int64
	// ProxyURL reports the runtime proxy used to construct client. It is
	// sanitized before its non-secret representation is persisted.
	ProxyURL string
	// ReconstructionLease binds this call's local probe and allocation to one
	// exact Item reconstruction generation. Callers that need exact cleanup
	// should use Manager.ResumeDownloadWithLease.
	ReconstructionLease *ReconstructionLease
	// CommitGuard is evaluated at the reconstruction commit point. Queue
	// callers use it to ensure their exact activation is still current.
	CommitGuard func() bool
}

// ResumeDownload resumes a download item.
// For HTTP items, it validates segment-file integrity and creates an HTTP downloader.
// For FTP/FTPS/SFTP items, it skips segment-file checks (single-stream to dest file)
// and dispatches through SchemeRouter to create a protocol-specific downloader.
func (m *Manager) ResumeDownload(client *http.Client, hash string, opts *ResumeDownloadOpts) (item *Item, err error) {
	transferCtx, done, admitted := m.admitTransfer()
	if !admitted {
		return nil, ErrManagerShuttingDown
	}
	defer done()
	opts = cloneResumeDownloadOpts(opts)
	opts.CommitGuard = transferCommitGuard(transferCtx, opts.CommitGuard)
	return m.resumeDownload(transferCtx, client, hash, opts)
}

func (m *Manager) resumeDownload(
	transferCtx context.Context,
	client *http.Client,
	hash string,
	opts *ResumeDownloadOpts,
) (item *Item, err error) {
	if opts == nil {
		opts = &ResumeDownloadOpts{}
	}
	item = m.GetItem(hash)
	if item == nil {
		err = ErrDownloadNotFound
		return
	}
	lease := opts.ReconstructionLease
	if lease == nil {
		lease, err = m.beginReconstruction(hash, opts.CommitGuard)
		if err != nil {
			return
		}
	} else if !lease.belongsTo(item) {
		err = ErrReconstructionSuperseded
		return
	}
	snapshot := item.Snapshot()
	fresh := opts.Fresh
	if !fresh && !snapshot.Resumable {
		err = ErrDownloadNotResumable
		return
	}
	// Protocol guard: validate integrity differently per protocol.
	// HTTP uses segment directories + part files; FTP writes directly to dest file.
	switch snapshot.Protocol {
	case ProtoHTTP:
		// HTTP: validate segment directory + part files (existing behavior)
		if !fresh {
			err = validateDownloadIntegritySnapshot(snapshot)
		}
		if err != nil {
			return
		}
	case ProtoFTP, ProtoFTPS, ProtoSFTP:
		// FTP/SFTP: no segment files exist. Only verify destination file if download started.
		if !fresh && snapshot.Downloaded > 0 {
			mainFile := GetPath(snapshot.AbsoluteLocation, snapshot.Name)
			if !fileExists(mainFile) {
				err = fmt.Errorf("%w: destination file missing for %s resume: %s", ErrDownloadDataMissing, snapshot.Protocol, mainFile)
				return
			}
		}
	default:
		err = fmt.Errorf("resume not supported for protocol %s", snapshot.Protocol)
		return
	}

	// Dispatch based on protocol
	switch snapshot.Protocol {
	case ProtoFTP, ProtoFTPS, ProtoSFTP:
		if snapshot.TransferConfig.ProtocolCredentialsRequired {
			err = ErrProtocolCredentialsRequired
			return
		}
		// FTP/FTPS/SFTP resume via SchemeRouter
		schemeRouter := m.schemeRouter.Load()
		if schemeRouter == nil {
			err = fmt.Errorf("scheme router not initialized for %s resume", snapshot.Protocol)
			return
		}
		protocolURL := snapshot.URL
		if snapshot.TransferConfig.ProtocolUsername != "" {
			var parsedURL *url.URL
			parsedURL, err = url.Parse(protocolURL)
			if err != nil {
				err = fmt.Errorf("restore protocol username: %w", sanitizeHTTPError(err))
				return
			}
			parsedURL.User = url.User(snapshot.TransferConfig.ProtocolUsername)
			protocolURL = parsedURL.String()
		}
		var pd ProtocolDownloader
		pd, err = schemeRouter.NewDownloader(protocolURL, &DownloaderOpts{
			Context:           transferCtx,
			FileName:          snapshot.Name,
			DownloadDirectory: snapshot.DownloadLocation,
			SSHKeyPath:        snapshot.SSHKeyPath,
			MaxConnections:    snapshot.TransferConfig.MaxConnections,
			MaxSegments:       snapshot.TransferConfig.MaxSegments,
			Overwrite:         snapshot.TransferConfig.Overwrite,
		})
		if err != nil {
			return
		}
		// Probe to get file metadata (size, etc.)
		var probe ProbeResult
		if probe, err = pd.Probe(transferCtx); err != nil {
			_ = pd.Close()
			return
		}
		var restartPart *ItemPart
		if !fresh {
			// FTP and SFTP do not expose a strong representation identity that
			// can be persisted and revalidated. Their Resume implementations
			// therefore restart from byte zero instead of combining an old
			// local prefix with the newly probed remote object. Publish the
			// matching zero-progress state before exposing the downloader.
			//
			// Keep a noncompiled marker even for a zero-length object. Besides
			// describing the pending restart, it ensures crash recovery takes
			// the protocol Resume path rather than treating this as a new
			// collision-prone destination.
			finalOffset := int64(probe.ContentLength) - 1
			if finalOffset < 0 {
				finalOffset = 0
			}
			restartPart = &ItemPart{
				Hash:        pd.GetHash(),
				FinalOffset: finalOffset,
				Compiled:    false,
			}
		}
		// Patch handlers for item state updates
		if opts.Handlers == nil {
			opts.Handlers = &Handlers{}
		}
		m.patchProtocolHandlers(opts.Handlers, item)
		err = lease.commit(m, pd, opts.Handlers, opts.CommitGuard, func(item *Item) {
			item.Downloaded = 0
			item.TotalSize = ContentLength(probe.ContentLength)
			item.Resumable = probe.Resumable
			if fresh {
				item.Parts = make(map[int64]*ItemPart)
				item.memPart = make(map[string]int64)
				if probe.FileName != "" {
					item.Name = probe.FileName
				}
				return
			}
			item.Parts = map[int64]*ItemPart{0: restartPart}
			item.memPart = map[string]int64{restartPart.Hash: 0}
		})
		if err != nil {
			_ = pd.Close()
			return
		}

	default:
		// HTTP resume path.
		config := cloneTransferConfig(snapshot.TransferConfig)
		runtimeProxy := opts.ProxyURL != ""
		if runtimeProxy {
			var safeProxyURL string
			var proxyCredentialsRequired bool
			safeProxyURL, proxyCredentialsRequired, err =
				SanitizeProxyURLForPersistence(opts.ProxyURL)
			if err != nil {
				err = fmt.Errorf("persist runtime proxy: %w", err)
				return
			}
			config.ProxyURL = safeProxyURL
			config.ProxyCredentialsRequired = proxyCredentialsRequired
		}
		if config.ProxyCredentialsRequired && !runtimeProxy {
			err = ErrProxyCredentialsRequired
			return
		}
		if !runtimeProxy && config.ProxyURL != "" {
			var proxyClient *http.Client
			proxyClient, err = NewHTTPClientWithProxy(config.ProxyURL)
			if err != nil {
				err = fmt.Errorf("restore proxy: %w", err)
				return
			}
			if client != nil && client.Jar != nil {
				proxyClient.Jar = client.Jar
			}
			client = proxyClient
		}
		if fresh {
			if mkdirErr := WarpMkdirAll(GetPath(DlDataDir, hash), PrivateDirMode); mkdirErr != nil {
				err = fmt.Errorf("create download state directory: %w", mkdirErr)
				return
			}
		}
		persistedHeaders := append(Headers(nil), snapshot.Headers...)
		if persistedHeaders == nil {
			persistedHeaders = make(Headers, 0)
		}
		for _, newHeader := range opts.Headers {
			persistedHeaders.Update(newHeader.Key, newHeader.Value)
		}
		requestHeaders := append(Headers(nil), persistedHeaders...)
		for _, transientHeader := range opts.TransientHeaders {
			requestHeaders.Update(transientHeader.Key, transientHeader.Value)
		}
		var d *Downloader
		var downloaderOptFuncs []DownloaderOptsFunc
		if !fresh {
			downloaderOptFuncs = append(downloaderOptFuncs, withResumable(snapshot.Resumable))
		}
		if fresh && snapshot.DestinationClaimed {
			downloaderOptFuncs = append(downloaderOptFuncs, withClaimedEmptyDestination())
		}
		if opts.ForceParts {
			config.ForceParts = true
		}
		if opts.MaxConnections != 0 {
			config.MaxConnections = opts.MaxConnections
		}
		if opts.MaxSegments != 0 {
			config.MaxSegments = opts.MaxSegments
		}
		if opts.RetryConfig != nil {
			retryConfig := *opts.RetryConfig
			config.RetryConfig = &retryConfig
		}
		if opts.RequestTimeout != 0 {
			config.RequestTimeout = opts.RequestTimeout
		}
		if opts.SpeedLimit != 0 {
			config.SpeedLimit = opts.SpeedLimit
		}
		if config.NumBaseParts <= 0 {
			config.NumBaseParts = 1
		}
		downloaderOpts := &DownloaderOpts{
			Context:             transferCtx,
			ForceParts:          config.ForceParts,
			NumBaseParts:        config.NumBaseParts,
			MaxConnections:      config.MaxConnections,
			MaxSegments:         config.MaxSegments,
			Handlers:            opts.Handlers,
			FileName:            snapshot.Name,
			DownloadDirectory:   snapshot.DownloadLocation,
			Headers:             requestHeaders,
			PluginHeaderNames:   snapshot.PluginHeaderNames,
			ResourceETag:        snapshot.ResourceETag,
			RetryConfig:         config.RetryConfig,
			Overwrite:           config.Overwrite,
			LockFileName:        config.LockFileName,
			RequestTimeout:      config.RequestTimeout,
			MaxFileSize:         config.MaxFileSize,
			ChecksumConfig:      config.ChecksumConfig,
			SpeedLimit:          config.SpeedLimit,
			DisableWorkStealing: config.DisableWorkStealing,
		}
		if fresh {
			// A fresh occurrence is a new representation, not a resume. Probe
			// the live resource so a changed size, range capability or ETag
			// cannot be replaced with stale Item metadata. Recurring names and
			// manager-owned empty destinations are already claimed identities
			// and must not be changed by Content-Disposition or uniquification.
			persistedDestinationExists := snapshot.Name != "" &&
				fileExists(GetPath(snapshot.DownloadLocation, snapshot.Name))
			preserveName := config.LockFileName || snapshot.CronExpr != "" || snapshot.DestinationClaimed ||
				persistedDestinationExists
			if !preserveName {
				downloaderOpts.FileName = ""
			}
			downloaderOpts.LockFileName = preserveName
			downloaderOpts.ResourceETag = ""
			downloaderOpts.SkipSetup = true
			d, err = NewDownloader(client, snapshot.URL, downloaderOpts, downloaderOptFuncs...)
			if err == nil {
				err = finishFreshHTTPDownloader(d, hash, downloaderOpts)
			}
		} else {
			d, err = initDownloader(client, hash, snapshot.URL, snapshot.TotalSize, downloaderOpts, downloaderOptFuncs...)
		}
		if err != nil {
			if d != nil {
				_ = d.Close()
			}
			return
		}
		m.patchHandlers(d, item)
		// Wrap the concrete *Downloader in an httpProtocolDownloader adapter.
		adapter := &httpProtocolDownloader{
			inner:  d,
			rawURL: snapshot.URL,
			probed: true, // initDownloader already has the state
		}
		err = lease.commit(m, adapter, nil, opts.CommitGuard, func(item *Item) {
			item.Headers = persistedHeaders
			if fresh {
				// Publish the new representation metadata and reset progress
				// atomically with the matching allocation.
				item.Downloaded = 0
				item.Parts = make(map[int64]*ItemPart)
				item.memPart = make(map[string]int64)
				item.Name = d.fileName
				item.Url = d.persistedURL()
				item.TotalSize = d.contentLength
				item.Resumable = d.resumable
				item.ResourceETag = d.resourceETag
			}
			item.TransferConfig = transferConfigFromDownloader(d)
			item.TransferConfig.ProxyURL = config.ProxyURL
			item.TransferConfig.ProxyCredentialsRequired = config.ProxyCredentialsRequired
		})
		if err != nil {
			_ = d.Close()
			return
		}
	}
	return
}

// finishFreshHTTPDownloader attaches a freshly probed downloader to an
// existing manager hash. NewDownloader's SkipSetup mode deliberately avoids
// allocating a new hash/state directory; restart reconstruction reuses the
// persisted identity after the live metadata probe succeeds.
func finishFreshHTTPDownloader(d *Downloader, hash string, opts *DownloaderOpts) error {
	d.hash = hash
	d.dlPath = filepath.Join(DlDataDir, hash)
	if err := d.setupLogger(); err != nil {
		return err
	}
	d.l.Println("GET:", logSafeURL(d.url))
	d.l.Println("CONTENT-LENGTH:", d.contentLength.v(), "(", d.contentLength, ")")
	d.l.Println("FILE-NAME:", d.fileName)
	d.handlers.setDefault(d.l)
	if !d.resumable {
		d.numBaseParts = 1
	} else if opts.NumBaseParts != 0 {
		d.numBaseParts = opts.NumBaseParts
	}
	if d.numBaseParts <= 0 {
		d.numBaseParts = 1
	}
	if d.maxParts != 0 && d.maxConn > d.maxParts {
		d.maxConn = d.maxParts
	}
	if d.numBaseParts > d.maxConn {
		d.numBaseParts = d.maxConn
	}
	if d.maxParts != 0 && d.numBaseParts > d.maxParts {
		d.numBaseParts = d.maxParts
	}
	return nil
}

// itemHasPendingSchedule reports whether deleting item would discard work
// that is still expected to run. Scheduled and missed entries are directly
// runnable; a triggered item remains pending until its current occurrence
// reaches a terminal byte count.
//
// The caller must hold m.mu.
func itemHasPendingSchedule(item *Item) bool {
	switch item.ScheduleState {
	case ScheduleStateScheduled, ScheduleStateMissed:
		return true
	case ScheduleStateTriggered:
		return item.TotalSize < 0 || item.Downloaded < item.TotalSize
	default:
		return false
	}
}

// Flush flushes away all inactive history. Runnable queued or scheduled
// entries are preserved.
func (m *Manager) Flush() error {
	// Drain any pending background writes first so this operation is
	// applied on top of the latest persisted state.
	if p := m.persister.Load(); p != nil {
		if err := p.flush(); err != nil {
			return fmt.Errorf("flush persister: %w", err)
		}
	}
	// Stage metadata deletion under the lock, but leave resume directories
	// intact until the replacement snapshot has committed.
	m.mu.Lock()
	removed := make(ItemsMap)
	candidates := make(map[string]struct{})
	for hash, item := range m.items {
		// Since item.mu == m.mu, we already hold the lock.
		// Read fields directly without additional locking.
		totalSize := item.TotalSize
		downloaded := item.Downloaded

		// Use getDAlloc() for synchronized access to dAlloc
		dAlloc := item.getDAlloc()

		if itemHasPendingSchedule(item) || (totalSize != downloaded && dAlloc != nil) {
			continue
		}
		candidates[hash] = struct{}{}
	}

	var (
		finalizeQueueRemoval func()
		persistErr           error
	)
	if queue := m.queue.Load(); queue != nil {
		finalizeQueueRemoval, persistErr = queue.removeForManagerPersistence(
			candidates,
			true,
			func(queueState *QueueState, removable map[string]struct{}) (bool, error) {
				for hash := range removable {
					if item := m.items[hash]; item != nil {
						removed[hash] = item
						delete(m.items, hash)
					}
				}
				err := m.persistItemsWithQueueState(queueState)
				if err != nil && !managerStoreCommitSucceeded(err) {
					for hash, item := range removed {
						m.items[hash] = item
					}
					return false, err
				}
				return true, err
			},
		)
	} else {
		for hash := range candidates {
			removed[hash] = m.items[hash]
			delete(m.items, hash)
		}
		persistErr = m.persistItems()
		if persistErr != nil && !managerStoreCommitSucceeded(persistErr) {
			for hash, item := range removed {
				m.items[hash] = item
			}
		}
	}
	if persistErr != nil && !managerStoreCommitSucceeded(persistErr) {
		m.mu.Unlock()
		return fmt.Errorf("flush persist: %w", persistErr)
	}
	m.mu.Unlock()
	if finalizeQueueRemoval != nil {
		finalizeQueueRemoval()
	}

	var cleanupErr error
	for hash := range removed {
		if err := WarpRemoveAll(GetPath(DlDataDir, hash)); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove download data for %s: %w", hash, err))
		}
	}
	if persistErr != nil {
		persistErr = fmt.Errorf("flush persist: %w", persistErr)
	}
	return errors.Join(persistErr, cleanupErr)
}

// FlushOne flushes away the download item with the given hash.
// Fixed Race 6: Uses write lock for entire operation to prevent TOCTOU.
func (m *Manager) FlushOne(hash string) error {
	if p := m.persister.Load(); p != nil {
		if err := p.flush(); err != nil {
			return fmt.Errorf("flush persister: %w", err)
		}
	}
	m.mu.Lock()

	item, found := m.items[hash]
	if !found {
		m.mu.Unlock()
		return ErrFlushHashNotFound
	}

	// Check download state atomically under manager lock
	if itemHasPendingSchedule(item) ||
		(item.TotalSize != item.Downloaded && item.getDAlloc() != nil) {
		m.mu.Unlock()
		return ErrFlushItemDownloading
	}

	var (
		finalizeQueueRemoval func()
		persistErr           error
	)
	if queue := m.queue.Load(); queue != nil {
		finalizeQueueRemoval, persistErr = queue.removeForManagerPersistence(
			map[string]struct{}{hash: {}},
			true,
			func(queueState *QueueState, removable map[string]struct{}) (bool, error) {
				if _, removable := removable[hash]; !removable {
					return false, ErrFlushItemDownloading
				}
				delete(m.items, hash)
				err := m.persistItemsWithQueueState(queueState)
				if err != nil && !managerStoreCommitSucceeded(err) {
					m.items[hash] = item
					return false, err
				}
				return true, err
			},
		)
	} else {
		delete(m.items, hash)
		persistErr = m.persistItems()
		if persistErr != nil && !managerStoreCommitSucceeded(persistErr) {
			m.items[hash] = item
		}
	}
	if errors.Is(persistErr, ErrFlushItemDownloading) {
		m.mu.Unlock()
		return ErrFlushItemDownloading
	}
	if persistErr != nil && !managerStoreCommitSucceeded(persistErr) {
		m.mu.Unlock()
		return fmt.Errorf("flush one persist: %w", persistErr)
	}
	m.mu.Unlock()
	if finalizeQueueRemoval != nil {
		finalizeQueueRemoval()
	}

	// Directory removal is safe after persist (item can't be resumed)
	removeErr := WarpRemoveAll(GetPath(DlDataDir, hash))
	if persistErr != nil {
		persistErr = fmt.Errorf("flush one persist: %w", persistErr)
	}
	return errors.Join(persistErr, removeErr)
}

// PurgeFailedDownload removes a download entry that never wrote any
// bytes. Unlike FlushOne it does NOT refuse items whose dAlloc pointer
// is still set: an item that fails inside item.Start()/Resume() may
// leave dAlloc populated even though the download never made progress,
// and history hygiene requires we drop those rows anyway. Items that
// downloaded any bytes (Downloaded > 0) are KEPT so the user can
// resume — the caller is responsible for the Downloaded == 0 check.
//
// Returns nil if the hash isn't in the manager (idempotent — safe to
// call from a generic error path even if the item was never added).
func (m *Manager) PurgeFailedDownload(hash string) error {
	if p := m.persister.Load(); p != nil {
		if err := p.flush(); err != nil {
			return fmt.Errorf("flush persister: %w", err)
		}
	}
	m.mu.Lock()

	item, found := m.items[hash]
	if !found {
		m.mu.Unlock()
		return nil
	}
	var (
		finalizeQueueRemoval func()
		persistErr           error
	)
	if queue := m.queue.Load(); queue != nil {
		finalizeQueueRemoval, persistErr = queue.removeForManagerPersistence(
			map[string]struct{}{hash: {}},
			false,
			func(queueState *QueueState, _ map[string]struct{}) (bool, error) {
				delete(m.items, hash)
				err := m.persistItemsWithQueueState(queueState)
				if err != nil && !managerStoreCommitSucceeded(err) {
					m.items[hash] = item
					return false, err
				}
				return true, err
			},
		)
	} else {
		delete(m.items, hash)
		persistErr = m.persistItems()
		if persistErr != nil && !managerStoreCommitSucceeded(persistErr) {
			m.items[hash] = item
		}
	}
	if persistErr != nil && !managerStoreCommitSucceeded(persistErr) {
		m.mu.Unlock()
		return fmt.Errorf("purge failed download: %w", persistErr)
	}
	m.mu.Unlock()
	if finalizeQueueRemoval != nil {
		finalizeQueueRemoval()
	}

	removeErr := WarpRemoveAll(GetPath(DlDataDir, hash))
	if persistErr != nil {
		persistErr = fmt.Errorf("purge failed download: %w", persistErr)
	}
	return errors.Join(persistErr, removeErr)
}

// Close closes the manager safely, ensuring all data is persisted.
// Safe to call multiple times and concurrently; only the first
// caller performs the shutdown, others are no-ops.
func (m *Manager) Close() error {
	// Close admission and cancel all Manager-owned protocol contexts before
	// stopping persistence. Stop every published allocation first, including
	// stopped-but-still-owned allocations and nil in-flight reconstructions.
	// Then drain admitted work before exact allocation cleanup so no file
	// handle can outlive the final persisted snapshot.
	var shutdownErr error
	m.CancelTransfers()
	items := m.GetItems()
	for _, item := range items {
		if item == nil {
			continue
		}
		if err := item.StopDownload(); err != nil &&
			!errors.Is(err, ErrItemDownloaderNotFound) {
			shutdownErr = errors.Join(
				shutdownErr,
				fmt.Errorf("stop downloader %s: %w", item.Hash, err),
			)
		}
	}
	if err := m.WaitTransfers(context.Background()); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	// An AddDownload admitted before cancellation may publish after the first
	// snapshot. WaitTransfers proves registration is now quiescent; refresh
	// the current map and stop those exact allocations too. Keep the original
	// pointers in the cleanup set so a same-hash replacement cannot orphan
	// the allocation observed by the first snapshot.
	currentItems := m.GetItems()
	for _, item := range currentItems {
		if item == nil {
			continue
		}
		if err := item.StopDownload(); err != nil &&
			!errors.Is(err, ErrItemDownloaderNotFound) {
			shutdownErr = errors.Join(
				shutdownErr,
				fmt.Errorf("stop downloader %s: %w", item.Hash, err),
			)
		}
	}
	cleanupItems := make(map[*Item]struct{}, len(items)+len(currentItems))
	for _, item := range items {
		if item != nil {
			cleanupItems[item] = struct{}{}
		}
	}
	for _, item := range currentItems {
		if item != nil {
			cleanupItems[item] = struct{}{}
		}
	}
	for item := range cleanupItems {
		if item == nil {
			continue
		}
		if err := item.CloseDownloader(); err != nil {
			shutdownErr = errors.Join(
				shutdownErr,
				fmt.Errorf("close downloader %s: %w", item.Hash, err),
			)
		}
	}

	// Swap the persister out atomically so concurrent UpdateItem calls
	// see either the live persister or nil - never a half-closed state.
	// CompareAndSwap ensures only the first caller shuts it down.
	p := m.persister.Load()
	if p != nil && m.persister.CompareAndSwap(p, nil) {
		if err := p.shutdown(); err != nil {
			log.Printf("warplib: warning: persister shutdown error: %v", err)
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Already closed — return immediately. Close is safe to call more than
	// once; the first winning CAS above handled shutdown.
	if m.closed {
		return nil
	}

	// Final atomic persist before closing - belt-and-braces guard
	// against a dirty flag that arrived after the persister's final
	// flush.
	if err := m.persistItems(); err != nil {
		log.Printf("warplib: warning: failed to persist on close: %v", err)
		// Leave closed false so a caller can retry Close after a transient
		// filesystem failure. The persister is already shut down, so the
		// retry performs a direct final atomic snapshot.
		return errors.Join(shutdownErr, err)
	}
	m.closed = true
	return shutdownErr
}
