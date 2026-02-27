//go:build ignore

// Package warplib provides this file as documentation of the fixture generation process.
// The actual generator is pkg/warplib/testdata/gen_fixture.go (also build:ignore).
// To regenerate (only needed if wiping the repo before Phase 2 Protocol field):
//
//	go run ./pkg/warplib/testdata/gen_fixture.go
//
// Then commit testdata/pre_phase2_userdata.warp.
// Do NOT run again after Protocol field is added to Item — the fixture would include Protocol.
package warplib

import (
	"bytes"
	"encoding/gob"
	"os"
	"sync"
	"testing"
	"time"
)

// TestGeneratePrePhase2Fixture creates a GOB-encoded ManagerData without the Protocol
// field (which does not exist yet when this runs). This fixture is the "golden" binary
// used by TestGOBBackwardCompatProtocol to verify backward compat after Phase 2 adds Protocol.
func TestGeneratePrePhase2Fixture(t *testing.T) {
	mu := new(sync.RWMutex)

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
		mu:      mu,
		memPart: map[string]int64{"part1_hash_001": 0, "part2_hash_002": 26214400},
	}

	// Item 2: a non-resumable completed download, no parts (nil)
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
		mu:               mu,
		memPart:          map[string]int64{},
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
		t.Fatalf("encode ManagerData: %v", err)
	}

	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}

	if err := os.WriteFile("testdata/pre_phase2_userdata.warp", buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Logf("Generated testdata/pre_phase2_userdata.warp (%d bytes)", buf.Len())
	t.Log("Verify: the fixture encodes Item without Protocol field — after adding Protocol, this fixture should decode with Protocol==0 (ProtoHTTP)")
}
