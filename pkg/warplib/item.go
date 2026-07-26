// Item is a struct that represents a download.
// It contains all the necessary information about a download.
// Package warplib provides core structures and utilities for managing download items
// and their associated metadata in the WarpDL application.
package warplib

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// ScheduleState represents the lifecycle state of a scheduled download.
// The zero value ("") means the item is not scheduled (normal download).
type ScheduleState string

const (
	// ScheduleStateNone is the zero value — item is not scheduled.
	ScheduleStateNone ScheduleState = ""
	// ScheduleStateScheduled means the item is waiting for its trigger time.
	ScheduleStateScheduled ScheduleState = "scheduled"
	// ScheduleStateTriggered means the trigger time was reached and the item
	// has been enqueued for download.
	ScheduleStateTriggered ScheduleState = "triggered"
	// ScheduleStateMissed means the trigger time passed while the daemon was down.
	// Missed items are enqueued immediately on daemon restart.
	ScheduleStateMissed ScheduleState = "missed"
	// ScheduleStateCancelled means the user cancelled the schedule before it fired.
	// This is a terminal state — no transitions out.
	ScheduleStateCancelled ScheduleState = "cancelled"
)

// TransferConfig is the non-secret operational contract required to recreate
// a downloader after daemon restart. Secret-bearing proxy/protocol credentials
// are never stored; the corresponding flags make an unreconstructable transfer
// fail explicitly instead of silently changing authentication behavior.
type TransferConfig struct {
	ForceParts                  bool
	NumBaseParts                int32
	MaxConnections              int32
	MaxSegments                 int32
	Overwrite                   bool
	LockFileName                bool
	ProxyURL                    string
	ProxyCredentialsRequired    bool
	ProtocolUsername            string
	ProtocolCredentialsRequired bool
	RetryConfig                 *RetryConfig
	RequestTimeout              time.Duration
	MaxFileSize                 int64
	ChecksumConfig              *ChecksumConfig
	SpeedLimit                  int64
	DisableWorkStealing         bool
}

func cloneTransferConfig(config TransferConfig) TransferConfig {
	cloned := config
	if config.RetryConfig != nil {
		retryConfig := *config.RetryConfig
		cloned.RetryConfig = &retryConfig
	}
	if config.ChecksumConfig != nil {
		checksumConfig := *config.ChecksumConfig
		cloned.ChecksumConfig = &checksumConfig
	}
	return cloned
}

// Item represents a download item with its associated metadata and state.
// It includes information such as the item's unique identifier, name, URL,
// headers, size, download progress, and storage location.
type Item struct {
	// Hash is the unique identifier of the download item.
	Hash string `json:"hash"`
	// Name is the name of the download item.
	Name string `json:"name"`
	// Url is the download url of the download item.
	Url string `json:"url"`
	// Headers used for the download
	Headers Headers `json:"headers"`
	// PluginHeaderNames records which persisted request headers originated
	// from extension code. Values remain in Headers for authenticated resume,
	// while provenance preserves cross-origin stripping and log redaction.
	PluginHeaderNames []string `json:"-"`
	// ResourceETag is the strong HTTP representation validator captured when
	// the download was created. It binds resumed segments to the same bytes.
	ResourceETag string `json:"-"`
	// DateAdded is the time when the download item was added.
	DateAdded time.Time `json:"date_added"`
	// TotalSize is the total size of the download item.
	TotalSize ContentLength `json:"total_size"`
	// Downloaded is the total size of the download item that has been downloaded.
	Downloaded ContentLength `json:"downloaded"`
	// DownloadLocation is the location where the download item is saved.
	DownloadLocation string `json:"download_location"`
	// AbsoluteLocation is the absolute path where the download item is saved.
	AbsoluteLocation string `json:"absolute_location"`
	// ChildHash is a hash representing the child item, if applicable.
	ChildHash string `json:"child_hash"`
	// Hidden is a flag indicating whether the item is hidden.
	Hidden bool `json:"hidden"`
	// Children is a flag indicating whether this item is a child of any other download item.
	Children bool `json:"children"`
	// Parts is a map of download parts, where each part is represented by an ItemPart.
	Parts map[int64]*ItemPart `json:"parts"`
	// Resumable is a flag indicating whether the download can be resumed.
	Resumable bool `json:"resumable"`
	// Protocol identifies which download protocol to use when resuming this item.
	// Zero value is ProtoHTTP (0), ensuring backward compatibility with GOB files
	// encoded before Phase 2 added this field — GOB zero-initializes missing fields.
	// INVARIANT: ProtoHTTP must remain iota=0 or all pre-Phase-2 files will break.
	Protocol Protocol `json:"protocol"`
	// SSHKeyPath is the path to the SSH private key used for SFTP downloads.
	// Persisted so resume uses the same key as the initial download.
	// Empty means default key paths (~/.ssh/id_ed25519, ~/.ssh/id_rsa) are tried.
	// GOB backward-compatible: missing field decodes as empty string (zero value).
	SSHKeyPath string `json:"ssh_key_path,omitempty"`
	// ScheduledAt is the absolute trigger time for one-shot scheduled downloads.
	// Zero value means not scheduled. GOB backward-compatible (zero value safe).
	ScheduledAt time.Time `json:"scheduled_at,omitempty"`
	// CronExpr is the cron expression for recurring downloads (e.g., "0 2 * * *").
	// Empty string means one-shot (not recurring). GOB backward-compatible.
	CronExpr string `json:"cron_expr,omitempty"`
	// ScheduleState tracks the lifecycle of a scheduled download.
	// Zero value (ScheduleStateNone) means not scheduled. GOB backward-compatible.
	ScheduleState ScheduleState `json:"schedule_state,omitempty"`
	// CookieSourcePath is the path to the cookie file or "auto" for auto-detection.
	// Persisted so cookies can be re-imported on resume/retry/recurring (FR-024).
	// Cookie VALUES are never persisted (FR-023). Empty means no cookies.
	// GOB backward-compatible: missing field decodes as empty string (zero value).
	CookieSourcePath string `json:"cookie_source_path,omitempty"`
	// DestinationClaimed records that this manager successfully opened the
	// final HTTP destination before any part metadata was persisted. It lets a
	// crash-recovered fresh start reuse only its own empty stub, while an
	// unrelated pre-existing destination still follows strict collision rules.
	// GOB backward-compatible: older snapshots decode false.
	DestinationClaimed bool `json:"-"`
	// TransferConfig contains restart-critical, non-secret downloader options.
	// It is persisted by GOB but excluded from public JSON/list responses.
	TransferConfig TransferConfig `json:"-"`
	// mu is a mutex for synchronizing access to the item's fields.
	mu *sync.RWMutex
	// dAllocMu protects access to dAlloc field (value type, not pointer, for GOB serialization)
	dAllocMu sync.RWMutex
	// dAlloc is the ProtocolDownloader managing this item.
	// Type is ProtocolDownloader to allow HTTP, FTP, SFTP backends.
	dAlloc      ProtocolDownloader
	dAllocOwner *allocationOwner
	// reconstructionMu serializes short begin/commit/close transitions without
	// being held across network probing. reconstructionGeneration invalidates
	// older probes; dAllocGeneration identifies the exact allocation committed
	// by a ReconstructionLease.
	reconstructionMu         sync.Mutex
	reconstructionTransition sync.Mutex
	reconstructionGeneration uint64
	dAllocGeneration         uint64
	// activeRuns and runsDrained form the invocation side of the reconstruction
	// lease. A run claim is acquired while reconstructionMu still proves the
	// allocation identity, then retained across Download/Resume. Replacement
	// waits for runsDrained before it may return a new reconstruction lease.
	activeRuns  int
	runsDrained chan struct{}
	// memPart is an internal map for managing memory allocation of parts.
	memPart map[string]int64
	// resumeHandlers holds patched handler callbacks for the protocol resume path.
	// Set by Manager.ResumeDownload for FTP/FTPS/SFTP after patchProtocolHandlers.
	// Unexported to prevent GOB serialization (func values cannot be GOB-encoded).
	// nil for HTTP items — HTTP uses patchHandlers on *Downloader struct field.
	resumeHandlers *Handlers
}

// ItemSnapshot is an immutable copy of the mutable metadata required by API
// and scheduler callers. Slice fields are cloned by Snapshot.
type ItemSnapshot struct {
	Hash               string
	Name               string
	URL                string
	Headers            Headers
	PluginHeaderNames  []string
	ResourceETag       string
	DateAdded          time.Time
	TotalSize          ContentLength
	Downloaded         ContentLength
	DownloadLocation   string
	AbsoluteLocation   string
	ChildHash          string
	Hidden             bool
	Children           bool
	Parts              map[int64]*ItemPart
	Resumable          bool
	Protocol           Protocol
	SSHKeyPath         string
	ScheduledAt        time.Time
	CronExpr           string
	ScheduleState      ScheduleState
	CookieSourcePath   string
	DestinationClaimed bool
	TransferConfig     TransferConfig
}

// Snapshot returns a race-free copy of the item's mutable metadata.
func (i *Item) Snapshot() ItemSnapshot {
	if i == nil {
		return ItemSnapshot{}
	}
	if i.mu != nil {
		i.mu.RLock()
		defer i.mu.RUnlock()
	}
	headers := append(Headers(nil), i.Headers...)
	pluginHeaderNames := append([]string(nil), i.PluginHeaderNames...)
	parts := make(map[int64]*ItemPart, len(i.Parts))
	for offset, part := range i.Parts {
		if part == nil {
			parts[offset] = nil
			continue
		}
		partCopy := *part
		parts[offset] = &partCopy
	}
	return ItemSnapshot{
		Hash:               i.Hash,
		Name:               i.Name,
		URL:                i.Url,
		Headers:            headers,
		PluginHeaderNames:  pluginHeaderNames,
		ResourceETag:       i.ResourceETag,
		DateAdded:          i.DateAdded,
		TotalSize:          i.TotalSize,
		Downloaded:         i.Downloaded,
		DownloadLocation:   i.DownloadLocation,
		AbsoluteLocation:   i.AbsoluteLocation,
		ChildHash:          i.ChildHash,
		Hidden:             i.Hidden,
		Children:           i.Children,
		Parts:              parts,
		Resumable:          i.Resumable,
		Protocol:           i.Protocol,
		SSHKeyPath:         i.SSHKeyPath,
		ScheduledAt:        i.ScheduledAt,
		CronExpr:           i.CronExpr,
		ScheduleState:      i.ScheduleState,
		CookieSourcePath:   i.CookieSourcePath,
		DestinationClaimed: i.DestinationClaimed,
		TransferConfig:     cloneTransferConfig(i.TransferConfig),
	}
}

// UpdateHeader updates a persisted request header under the item lock.
func (i *Item) UpdateHeader(key, value string) {
	if i.mu != nil {
		i.mu.Lock()
		defer i.mu.Unlock()
	}
	i.Headers.Update(key, value)
}

// AddTotalSize atomically adds size to the item's aggregate total.
func (i *Item) AddTotalSize(size ContentLength) {
	if i.mu != nil {
		i.mu.Lock()
		defer i.mu.Unlock()
	}
	i.TotalSize += size
}

// MarkComplete records a terminal successful download using the final on-disk
// size. The caller must pass the item back to Manager.UpdateItem to persist the
// mutation.
func (i *Item) MarkComplete(finalSize ContentLength) {
	if i.mu != nil {
		i.mu.Lock()
		defer i.mu.Unlock()
	}
	i.TotalSize = finalSize
	i.Downloaded = finalSize
	i.Parts = nil
	i.DestinationClaimed = false
}

// MarshalJSON returns a race-free public representation of an Item. Request
// headers and local credential/key source paths are deliberately excluded:
// callers need lifecycle metadata, not reusable authentication material.
func (i *Item) MarshalJSON() ([]byte, error) {
	if i == nil {
		return []byte("null"), nil
	}
	if i.mu != nil {
		i.mu.RLock()
		defer i.mu.RUnlock()
	}
	type publicItem struct {
		Hash             string              `json:"hash"`
		Name             string              `json:"name"`
		URL              string              `json:"url"`
		Headers          Headers             `json:"headers"`
		DateAdded        time.Time           `json:"date_added"`
		TotalSize        ContentLength       `json:"total_size"`
		Downloaded       ContentLength       `json:"downloaded"`
		DownloadLocation string              `json:"download_location"`
		AbsoluteLocation string              `json:"absolute_location"`
		ChildHash        string              `json:"child_hash"`
		Hidden           bool                `json:"hidden"`
		Children         bool                `json:"children"`
		Parts            map[int64]*ItemPart `json:"parts"`
		Resumable        bool                `json:"resumable"`
		Protocol         Protocol            `json:"protocol"`
		ScheduledAt      time.Time           `json:"scheduled_at,omitempty"`
		CronExpr         string              `json:"cron_expr,omitempty"`
		ScheduleState    ScheduleState       `json:"schedule_state,omitempty"`
	}
	return json.Marshal(publicItem{
		Hash:             i.Hash,
		Name:             i.Name,
		URL:              logSafeURL(i.Url),
		Headers:          nil,
		DateAdded:        i.DateAdded,
		TotalSize:        i.TotalSize,
		Downloaded:       i.Downloaded,
		DownloadLocation: i.DownloadLocation,
		AbsoluteLocation: i.AbsoluteLocation,
		ChildHash:        i.ChildHash,
		Hidden:           i.Hidden,
		Children:         i.Children,
		Parts:            i.Parts,
		Resumable:        i.Resumable,
		Protocol:         i.Protocol,
		ScheduledAt:      i.ScheduledAt,
		CronExpr:         i.CronExpr,
		ScheduleState:    i.ScheduleState,
	})
}

// ItemPart represents a part of a download item.
// It contains metadata about a specific segment of the download,
// including its unique hash, final offset, and compilation status.
type ItemPart struct {
	// Hash is the unique identifier for this part of the download.
	Hash string `json:"hash"`
	// FinalOffset is the ending byte offset of this part in the download.
	FinalOffset int64 `json:"final_offset"`
	// Compiled indicates whether this part has been successfully compiled or merged.
	Compiled bool `json:"compiled"`
}

// ValidateItemParts validates a map of ItemParts for nil values and invalid
// inclusive ranges. FinalOffset == start is a valid one-byte segment.
func ValidateItemParts(parts map[int64]*ItemPart) error {
	for ioff, part := range parts {
		if part == nil {
			return fmt.Errorf("%w: nil part at offset %d", ErrItemPartNil, ioff)
		}
		if part.FinalOffset < ioff {
			return fmt.Errorf("%w: part %q at offset %d has FinalOffset %d",
				ErrItemPartInvalidRange, part.Hash, ioff, part.FinalOffset)
		}
	}
	return nil
}

// ItemsMap is a map of download items, where each item is indexed by its unique identifier.
type ItemsMap map[string]*Item

type itemOpts struct {
	Hide, Child       bool
	ChildHash         string
	AbsoluteLocation  string
	Headers           []Header
	PluginHeaderNames []string
	ResourceETag      string
	TransferConfig    TransferConfig
}

func newItem(mu *sync.RWMutex, name, url, dlloc, hash string, totalSize ContentLength, resumable bool, opts *itemOpts) (i *Item, err error) {
	if opts == nil {
		opts = &itemOpts{}
	}
	opts.AbsoluteLocation, err = filepath.Abs(
		opts.AbsoluteLocation,
	)
	if err != nil {
		return
	}
	i = &Item{
		Hash:              hash,
		Name:              name,
		Url:               url,
		Headers:           opts.Headers,
		PluginHeaderNames: append([]string(nil), opts.PluginHeaderNames...),
		ResourceETag:      opts.ResourceETag,
		TransferConfig:    cloneTransferConfig(opts.TransferConfig),
		DateAdded:         time.Now(),
		TotalSize:         totalSize,
		DownloadLocation:  dlloc,
		AbsoluteLocation:  opts.AbsoluteLocation,
		ChildHash:         opts.ChildHash,
		Hidden:            opts.Hide,
		Children:          opts.Child,
		Resumable:         resumable,
		Parts:             make(map[int64]*ItemPart),
		memPart:           make(map[string]int64),
		mu:                mu,
	}
	return
}

func (i *Item) addPart(hash string, ioff, foff int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Parts[ioff] = &ItemPart{
		Hash:        hash,
		FinalOffset: foff,
	}
	i.memPart[hash] = ioff
}

func (i *Item) savePart(offset int64, part *ItemPart) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Parts[offset] = part
}

func (i *Item) getPart(hash string) (offset int64, part *ItemPart) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	offset = i.memPart[hash]
	part = i.Parts[offset]
	return
}

func (i *Item) getPartWithError(hash string) (offset int64, part *ItemPart, err error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	offset, exists := i.memPart[hash]
	if !exists {
		return 0, nil, nil // Hash not found (normal case)
	}

	part = i.Parts[offset]
	if part == nil {
		return 0, nil, fmt.Errorf("%w: hash %q maps to offset %d", ErrPartDesync, hash, offset)
	}
	return offset, part, nil
}

// getDAlloc returns the current downloader with proper synchronization.
func (i *Item) getDAlloc() ProtocolDownloader {
	i.dAllocMu.RLock()
	defer i.dAllocMu.RUnlock()
	return i.dAlloc
}

func (i *Item) claimDAllocLocked(
	generation uint64,
	exact bool,
) (ProtocolDownloader, *Handlers, func(), error) {
	if exact && (i.reconstructionGeneration != generation ||
		i.dAllocGeneration != generation) {
		return nil, nil, nil, ErrReconstructionSuperseded
	}
	i.dAllocMu.RLock()
	d := i.dAlloc
	h := i.resumeHandlers
	i.dAllocMu.RUnlock()
	if d == nil {
		if exact {
			return nil, nil, nil, ErrReconstructionSuperseded
		}
		return nil, nil, nil, ErrItemDownloaderNotFound
	}
	return d, h, i.claimRunLocked(), nil
}

func (i *Item) claimRunLocked() func() {
	if i.activeRuns == 0 {
		i.runsDrained = make(chan struct{})
	}
	i.activeRuns++
	var once sync.Once
	release := func() {
		once.Do(func() {
			i.reconstructionMu.Lock()
			i.activeRuns--
			if i.activeRuns == 0 {
				close(i.runsDrained)
			}
			i.reconstructionMu.Unlock()
		})
	}
	return release
}

func (i *Item) claimDAlloc() (ProtocolDownloader, *Handlers, func(), error) {
	i.reconstructionMu.Lock()
	defer i.reconstructionMu.Unlock()
	return i.claimDAllocLocked(0, false)
}

// runsDrainedLocked returns a signal for all Download/Resume calls that
// already claimed an allocation. The caller must hold reconstructionMu.
func (i *Item) runsDrainedLocked() <-chan struct{} {
	if i.activeRuns != 0 {
		return i.runsDrained
	}
	drained := make(chan struct{})
	close(drained)
	return drained
}

// setDAlloc sets the downloader with proper synchronization.
func (i *Item) setDAlloc(d ProtocolDownloader) {
	i.reconstructionTransition.Lock()
	defer i.reconstructionTransition.Unlock()
	i.reconstructionMu.Lock()
	defer i.reconstructionMu.Unlock()
	i.dAllocMu.Lock()
	defer i.dAllocMu.Unlock()
	i.reconstructionGeneration++
	i.dAllocGeneration = i.reconstructionGeneration
	i.dAlloc = d
	i.dAllocOwner = newAllocationOwner(d)
}

// setResumeHandlers stores patched handlers for use during Item.Resume().
// Called by Manager.ResumeDownload after patchProtocolHandlers for FTP/FTPS/SFTP items.
func (i *Item) setResumeHandlers(h *Handlers) {
	i.dAllocMu.Lock()
	defer i.dAllocMu.Unlock()
	i.resumeHandlers = h
}

// clearDAlloc clears the downloader with proper synchronization.
func (i *Item) clearDAlloc() {
	i.reconstructionMu.Lock()
	defer i.reconstructionMu.Unlock()
	i.dAllocMu.Lock()
	defer i.dAllocMu.Unlock()
	i.reconstructionGeneration++
	i.dAllocGeneration = 0
	i.dAlloc = nil
	i.dAllocOwner = nil
	i.resumeHandlers = nil
}

// GetDownloaded returns the downloaded byte count with proper synchronization.
// Safe to call from any goroutine. Returns the raw field value when mu is nil
// (e.g., items constructed in tests without a Manager).
func (i *Item) GetDownloaded() ContentLength {
	if i.mu != nil {
		i.mu.RLock()
		defer i.mu.RUnlock()
	}
	return i.Downloaded
}

// GetTotalSize returns the total size with proper synchronization.
// Safe to call from any goroutine. Returns the raw field value when mu is nil.
func (i *Item) GetTotalSize() ContentLength {
	if i.mu != nil {
		i.mu.RLock()
		defer i.mu.RUnlock()
	}
	return i.TotalSize
}

// GetPercentage returns the download progress as a percentage.
// Uses mu for thread-safe access to Downloaded and TotalSize.
func (i *Item) GetPercentage() int64 {
	if i.mu != nil {
		i.mu.RLock()
		defer i.mu.RUnlock()
	}
	if i.TotalSize <= 0 {
		return 0
	}
	p := (i.Downloaded * 100) / i.TotalSize
	return p.v()
}

// GetSavePath returns the save path for the download item.
func (i *Item) GetSavePath() (svPath string) {
	if i.mu != nil {
		i.mu.RLock()
		defer i.mu.RUnlock()
	}
	svPath = GetPath(i.DownloadLocation, i.Name)
	return
}

// GetAbsolutePath returns the absolute path for the download item.
func (i *Item) GetAbsolutePath() (aPath string) {
	if i.mu != nil {
		i.mu.RLock()
		defer i.mu.RUnlock()
	}
	aPath = GetPath(i.AbsoluteLocation, i.Name)
	return
}

// GetMaxConnections returns the maximum number of connections for the download item.
func (i *Item) GetMaxConnections() (int32, error) {
	i.dAllocMu.RLock()
	defer i.dAllocMu.RUnlock()
	if i.dAlloc == nil {
		return 0, ErrItemDownloaderNotFound
	}
	return i.dAlloc.GetMaxConnections(), nil
}

// GetMaxParts returns the maximum number of parts for the download item.
func (i *Item) GetMaxParts() (int32, error) {
	i.dAllocMu.RLock()
	defer i.dAllocMu.RUnlock()
	if i.dAlloc == nil {
		return 0, ErrItemDownloaderNotFound
	}
	return i.dAlloc.GetMaxParts(), nil
}

// Resume resumes the download of the item.
// Fixed Race 2: Takes snapshot of Parts under Item lock before calling Resume.
// For FTP/SFTP: passes stored resumeHandlers to ProtocolDownloader.Resume().
// For HTTP: resumeHandlers is nil, preserving patchHandlers-installed struct field handlers.
func (i *Item) Resume() error {
	return i.ResumeContext(context.Background())
}

// ResumeContext resumes the current allocation within ctx while retaining its
// exact run claim until the protocol call returns.
func (i *Item) ResumeContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	d, h, release, err := i.claimDAlloc()
	if err != nil {
		return err
	}
	defer release()
	if d.IsStopped() {
		return nil
	}

	// Take a snapshot while the run claim prevents a replacement allocation
	// from being published until this Resume call returns.
	i.mu.RLock()
	partsCopy := make(map[int64]*ItemPart, len(i.Parts))
	for k, v := range i.Parts {
		if v == nil {
			partsCopy[k] = nil
			continue
		}
		partCopy := *v
		partsCopy[k] = &partCopy
	}
	i.mu.RUnlock()
	// h is non-nil for FTP/SFTP (set by Manager.ResumeDownload), nil for HTTP.
	// FTP/SFTP Resume uses h parameter directly for callbacks.
	// HTTP Resume: nil preserves patchHandlers-installed struct field (non-nil would replace it).
	return d.Resume(ctx, partsCopy, h)
}

// Start begins a fresh download using the currently assigned downloader.
func (i *Item) Start() error {
	return i.StartContext(context.Background())
}

// StartContext starts the current allocation within ctx while retaining its
// exact run claim until the protocol call returns.
func (i *Item) StartContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	d, h, release, err := i.claimDAlloc()
	if err != nil {
		return err
	}
	defer release()
	if d.IsStopped() {
		return nil
	}
	// Protocol downloaders receive their manager-patched handlers here.
	// HTTP adapters ignore this argument and retain the handlers installed
	// directly on their concrete Downloader by Manager.patchHandlers.
	return d.Download(ctx, h)
}

// HasParts reports whether the item has any persisted part state.
func (i *Item) HasParts() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.Parts) > 0
}

// StopDownload pauses the download of the item.
func (i *Item) StopDownload() error {
	i.reconstructionMu.Lock()
	i.dAllocMu.Lock()
	allocation := i.dAlloc
	owner := i.allocationOwnerLocked()
	if allocation == nil {
		// No published allocation means a reconstruction may be probing
		// outside the locks. Invalidate that exact generation.
		i.reconstructionGeneration++
	}
	i.dAllocMu.Unlock()
	i.reconstructionMu.Unlock()
	if allocation == nil {
		return ErrItemDownloaderNotFound
	}
	// Keep the stopped allocation attached until exact cleanup. This preserves
	// ownership when Stop races an asynchronously admitted Start: a later
	// reconstruction can drain the pending run claim and close this allocation
	// exactly once instead of leaking it after detachment.
	owner.stop()
	return nil
}

// CloseDownloader closes the downloader and releases all file handles.
// Use this when a download is aborted before Start()/Resume() completes.
func (i *Item) CloseDownloader() error {
	i.reconstructionTransition.Lock()
	defer i.reconstructionTransition.Unlock()
	i.reconstructionMu.Lock()
	i.dAllocMu.Lock()
	i.reconstructionGeneration++
	allocation := i.dAlloc
	owner := i.allocationOwnerLocked()
	i.dAlloc = nil
	i.dAllocOwner = nil
	i.resumeHandlers = nil
	i.dAllocGeneration = 0
	i.dAllocMu.Unlock()
	drained := i.runsDrainedLocked()
	i.reconstructionMu.Unlock()
	if allocation == nil {
		return nil
	}
	owner.stop()
	<-drained
	return owner.close()
}

// IsDownloading returns true if the item is currently being downloaded.
func (i *Item) IsDownloading() bool {
	i.dAllocMu.RLock()
	defer i.dAllocMu.RUnlock()
	return i.dAlloc != nil && !i.dAlloc.IsStopped()
}

// IsStopped returns true if the download was intentionally stopped.
func (i *Item) IsStopped() bool {
	i.dAllocMu.RLock()
	defer i.dAllocMu.RUnlock()
	if i.dAlloc == nil {
		return true
	}
	return i.dAlloc.IsStopped()
}
