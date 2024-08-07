package warplib

import (
	"path/filepath"
	"sync"
	"time"
)

type Item struct {
	Hash             string              `json:"hash"`
	Name             string              `json:"name"`
	Url              string              `json:"url"`
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
	mu               *sync.RWMutex
	dAlloc           *Downloader
	memPart          map[string]int64
}

type ItemPart struct {
	Hash        string `json:"hash"`
	FinalOffset int64  `json:"final_offset"`
	Compiled    bool   `json:"compiled"`
}

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

func (i *Item) GetPercentage() int64 {
	p := (i.Downloaded * 100) / i.TotalSize
	return p.v()
}

func (i *Item) GetSavePath() (svPath string) {
	svPath = GetPath(i.DownloadLocation, i.Name)
	return
}

func (i *Item) GetAbsolutePath() (aPath string) {
	aPath = GetPath(i.AbsoluteLocation, i.Name)
	return
}

func (i *Item) GetMaxConnections() (int32, error) {
	if i.dAlloc == nil {
		return 0, ErrItemDownloaderNotFound
	}
	return i.dAlloc.GetMaxConnections(), nil
}

func (i *Item) GetMaxParts() (int32, error) {
	if i.dAlloc == nil {
		return 0, ErrItemDownloaderNotFound
	}
	return i.dAlloc.GetMaxParts(), nil
}

func (i *Item) Resume() error {
	if i.dAlloc == nil {
		return ErrItemDownloaderNotFound
	}
	return i.dAlloc.Resume(i.Parts)
}

func (i *Item) StopDownload() error {
	if i.dAlloc == nil {
		return ErrItemDownloaderNotFound
	}
	i.dAlloc.Stop()
	return nil
}
