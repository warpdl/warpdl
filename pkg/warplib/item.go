package warplib

import (
	"path/filepath"
	"sync"
	"time"
)

type Item struct {
	Hash             string
	Name             string
	Url              string
	Headers          Headers
	DateAdded        time.Time
	TotalSize        ContentLength
	Downloaded       ContentLength
	DownloadLocation string
	AbsoluteLocation string
	ChildHash        string
	Hidden           bool
	Children         bool
	Parts            map[int64]*ItemPart
	mu               *sync.RWMutex
	dAlloc           *Downloader
	memPart          map[string]int64
}

type ItemPart struct {
	Hash        string
	FinalOffset int64
	Compiled    bool
}

type ItemsMap map[string]*Item

type itemOpts struct {
	Hide, Child      bool
	ChildHash        string
	AbsoluteLocation string
	Headers          []Header
}

func newItem(mu *sync.RWMutex, name, url, dlloc, hash string, totalSize ContentLength, opts *itemOpts) (i *Item, err error) {
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

func (i *Item) Resume() error {
	return i.dAlloc.Resume(i.Parts)
}
