//go:build ignore

// gen_fixture.go generates testdata/pre_phase2_userdata.warp
// Run ONCE before adding Protocol field to Item:
//
//	go run ./pkg/warplib/testdata/gen_fixture.go
//
// Then commit the generated testdata/pre_phase2_userdata.warp file.
// Do NOT run again after Protocol field is added — the fixture would include Protocol.
//
// IMPORTANT: This file must use types that exactly match the warplib package's
// exported GOB-encoded types. Mismatches cause "type mismatch in decoder" errors.
package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"time"
)

// Minimal copies of warplib types needed for fixture generation.
// These MUST match the exported fields of the actual warplib structs exactly.
// GOB field matching is by name; type compatibility must also match.

type ContentLength int64

type Headers []Header

type Header struct {
	Key   string
	Value string
}

type ItemPart struct {
	Hash        string `json:"hash"`
	FinalOffset int64  `json:"final_offset"`
	Compiled    bool   `json:"compiled"`
}

type ItemsMap map[string]*Item

// Priority mirrors warplib.Priority for QueuedItemState compatibility.
type Priority int

// QueuedItemState mirrors warplib.QueuedItemState exactly.
// QueueState.Waiting is []QueuedItemState (NOT []string).
type QueuedItemState struct {
	Hash     string
	Priority Priority
}

// QueueState mirrors warplib.QueueState exactly.
type QueueState struct {
	MaxConcurrent int
	Waiting       []QueuedItemState
	Paused        bool
}

type ManagerData struct {
	Items      ItemsMap
	QueueState *QueueState
}

// Item mirrors warplib.Item WITHOUT the Protocol field (pre-Phase-2 state).
// All exported fields must match exactly for GOB to work correctly.
// Unexported fields (mu, dAllocMu, dAlloc, memPart) are NOT GOB-encoded.
type Item struct {
	Hash             string            `json:"hash"`
	Name             string            `json:"name"`
	Url              string            `json:"url"`
	Headers          Headers           `json:"headers"`
	DateAdded        time.Time         `json:"date_added"`
	TotalSize        ContentLength     `json:"total_size"`
	Downloaded       ContentLength     `json:"downloaded"`
	DownloadLocation string            `json:"download_location"`
	AbsoluteLocation string            `json:"absolute_location"`
	ChildHash        string            `json:"child_hash"`
	Hidden           bool              `json:"hidden"`
	Children         bool              `json:"children"`
	Parts            map[int64]*ItemPart `json:"parts"`
	Resumable        bool              `json:"resumable"`
	// NOTE: NO Protocol field — this is the pre-Phase-2 schema.
	// After adding Protocol to Item, this generator should NOT be re-run.
}

func main() {
	// Item 1: a resumable partial HTTP download with 2 parts
	item1 := &Item{
		Hash:             "hash_item1_abc123",
		Name:             "bigfile.zip",
		Url:              "http://example.com/bigfile.zip",
		Headers:          Headers{},
		DateAdded:        time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		TotalSize:        ContentLength(104857600), // 100 MB
		Downloaded:       ContentLength(52428800),  // 50 MB downloaded
		DownloadLocation: "/downloads",
		AbsoluteLocation: "/home/user/downloads",
		ChildHash:        "",
		Hidden:           false,
		Children:         false,
		Resumable:        true,
		Parts: map[int64]*ItemPart{
			0: {
				Hash:        "part1_hash_001",
				FinalOffset: 26214399,
				Compiled:    true,
			},
			26214400: {
				Hash:        "part2_hash_002",
				FinalOffset: 52428799,
				Compiled:    false,
			},
		},
	}

	// Item 2: a completed non-resumable download, no parts (nil), with a header
	item2 := &Item{
		Hash:             "hash_item2_def456",
		Name:             "smallfile.pdf",
		Url:              "http://example.com/smallfile.pdf",
		Headers:          Headers{{Key: "Authorization", Value: "Bearer token123"}},
		DateAdded:        time.Date(2024, 12, 1, 8, 0, 0, 0, time.UTC),
		TotalSize:        ContentLength(1048576), // 1 MB
		Downloaded:       ContentLength(1048576), // fully downloaded
		DownloadLocation: "/tmp/downloads",
		AbsoluteLocation: "/tmp/downloads",
		ChildHash:        "",
		Hidden:           true,
		Children:         false,
		Resumable:        false,
		Parts:            nil, // nil after completion
	}

	data := ManagerData{
		Items: ItemsMap{
			item1.Hash: item1,
			item2.Hash: item2,
		},
		QueueState: nil,
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(data); err != nil {
		fmt.Fprintf(os.Stderr, "encode ManagerData: %v\n", err)
		os.Exit(1)
	}

	outPath := "pkg/warplib/testdata/pre_phase2_userdata.warp"
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write fixture: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s (%d bytes)\n", outPath, buf.Len())
	fmt.Println("Fixture encodes ManagerData WITHOUT Protocol field (pre-Phase-2 schema).")
	fmt.Println("After Phase 2 adds Protocol to Item, decoding this fixture must yield Protocol==0 (ProtoHTTP).")
}
