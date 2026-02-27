// Item is a struct that represents a download.
// It contains all the necessary information about a download.
// Package warplib provides core structures and utilities for managing download items
// and their associated metadata in the WarpDL application.
package warplib

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

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
	// mu is a mutex for synchronizing access to the item's fields.
	mu *sync.RWMutex
	// dAllocMu protects access to dAlloc field (value type, not pointer, for GOB serialization)
	dAllocMu sync.RWMutex
	// dAlloc is the ProtocolDownloader managing this item.
	// Type is ProtocolDownloader to allow HTTP, FTP, SFTP backends.
	dAlloc ProtocolDownloader
	// memPart is an internal map for managing memory allocation of parts.
	memPart map[string]int64
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

// ValidateItemParts validates a map of ItemParts for nil values and invalid ranges.
func ValidateItemParts(parts map[int64]*ItemPart) error {
	for ioff, part := range parts {
		if part == nil {
			return fmt.Errorf("%w: nil part at offset %d", ErrItemPartNil, ioff)
		}
		if part.FinalOffset <= ioff {
			return fmt.Errorf("%w: part %q at offset %d has FinalOffset %d",
				ErrItemPartInvalidRange, part.Hash, ioff, part.FinalOffset)
		}
	}
	return nil
}

// ItemsMap is a map of download items, where each item is indexed by its unique identifier.
type ItemsMap map[string]*Item

type itemOpts struct {
	Hide, Child      bool
	ChildHash        string
	AbsoluteLocation string
	Headers          []Header
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
		Hash:             hash,
		Name:             name,
		Url:              url,
		Headers:          opts.Headers,
		DateAdded:        time.Now(),
		TotalSize:        totalSize,
		DownloadLocation: dlloc,
		AbsoluteLocation: opts.AbsoluteLocation,
		ChildHash:        opts.ChildHash,
		Hidden:           opts.Hide,
		Children:         opts.Child,
		Resumable:        resumable,
		Parts:            make(map[int64]*ItemPart),
		memPart:          make(map[string]int64),
		mu:               mu,
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

// setDAlloc sets the downloader with proper synchronization.
func (i *Item) setDAlloc(d ProtocolDownloader) {
	i.dAllocMu.Lock()
	defer i.dAllocMu.Unlock()
	i.dAlloc = d
}

// clearDAlloc clears the downloader with proper synchronization.
func (i *Item) clearDAlloc() {
	i.dAllocMu.Lock()
	defer i.dAllocMu.Unlock()
	i.dAlloc = nil
}

// GetPercentage returns the download progress as a percentage.
func (i *Item) GetPercentage() int64 {
	p := (i.Downloaded * 100) / i.TotalSize
	return p.v()
}

// GetSavePath returns the save path for the download item.
func (i *Item) GetSavePath() (svPath string) {
	svPath = GetPath(i.DownloadLocation, i.Name)
	return
}

// GetAbsolutePath returns the absolute path for the download item.
func (i *Item) GetAbsolutePath() (aPath string) {
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
// Handlers are not passed here because they were already installed by Manager.patchHandlers.
func (i *Item) Resume() error {
	// Take snapshot of Parts under Item lock first
	i.mu.RLock()
	partsCopy := make(map[int64]*ItemPart, len(i.Parts))
	for k, v := range i.Parts {
		partsCopy[k] = v
	}
	i.mu.RUnlock()

	// Then get downloader under dAllocMu lock
	i.dAllocMu.RLock()
	d := i.dAlloc
	i.dAllocMu.RUnlock()

	if d == nil {
		return ErrItemDownloaderNotFound
	}
	// Pass nil handlers — Manager.patchHandlers already installed them on the inner downloader.
	return d.Resume(context.Background(), partsCopy, nil)
}

// StopDownload pauses the download of the item.
func (i *Item) StopDownload() error {
	i.dAllocMu.Lock()
	defer i.dAllocMu.Unlock()
	if i.dAlloc == nil {
		return ErrItemDownloaderNotFound
	}
	i.dAlloc.Stop()
	i.dAlloc = nil
	return nil
}

// CloseDownloader closes the downloader and releases all file handles.
// Use this when a download is aborted before Start()/Resume() completes.
func (i *Item) CloseDownloader() error {
	i.dAllocMu.Lock()
	defer i.dAllocMu.Unlock()
	if i.dAlloc == nil {
		return nil
	}
	err := i.dAlloc.Close()
	i.dAlloc = nil
	return err
}

// IsDownloading returns true if the item is currently being downloaded.
func (i *Item) IsDownloading() bool {
	i.dAllocMu.RLock()
	defer i.dAllocMu.RUnlock()
	return i.dAlloc != nil
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
