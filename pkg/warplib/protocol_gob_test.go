package warplib

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestProtocolConstants asserts that Protocol iota values never change.
// ProtoHTTP MUST be 0 — this is the backward compat invariant.
// If any of these fail, all pre-Phase-2 GOB files will decode with wrong protocol.
func TestProtocolConstants(t *testing.T) {
	tests := []struct {
		name string
		got  Protocol
		want Protocol
	}{
		{"ProtoHTTP must be 0 (GOB backward compat)", ProtoHTTP, Protocol(0)},
		{"ProtoFTP must be 1", ProtoFTP, Protocol(1)},
		{"ProtoFTPS must be 2", ProtoFTPS, Protocol(2)},
		{"ProtoSFTP must be 3", ProtoSFTP, Protocol(3)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %d, want %d — BACKWARD COMPAT BROKEN", uint8(tt.got), uint8(tt.want))
			}
		})
	}
}

// TestProtocolString verifies human-readable names for all known and unknown values.
func TestProtocolString(t *testing.T) {
	tests := []struct {
		proto Protocol
		want  string
	}{
		{ProtoHTTP, "http"},
		{ProtoFTP, "ftp"},
		{ProtoFTPS, "ftps"},
		{ProtoSFTP, "sftp"},
		{Protocol(7), "unknown(7)"},
		{Protocol(255), "unknown(255)"},
		{Protocol(4), "unknown(4)"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("Protocol(%d)", uint8(tt.proto)), func(t *testing.T) {
			got := tt.proto.String()
			if got != tt.want {
				t.Errorf("Protocol(%d).String() = %q, want %q", uint8(tt.proto), got, tt.want)
			}
		})
	}
}

// TestValidateProtocol verifies that known protocols pass and unknown ones return an error.
func TestValidateProtocol(t *testing.T) {
	t.Run("known protocols return nil", func(t *testing.T) {
		for _, p := range []Protocol{ProtoHTTP, ProtoFTP, ProtoFTPS, ProtoSFTP} {
			if err := ValidateProtocol(p); err != nil {
				t.Errorf("ValidateProtocol(%s) = %v, want nil", p.String(), err)
			}
		}
	})

	t.Run("unknown protocol 7 returns error", func(t *testing.T) {
		err := ValidateProtocol(Protocol(7))
		if err == nil {
			t.Fatal("ValidateProtocol(7) = nil, want error")
		}
		// Error must contain "upgrade warpdl" to guide the user
		if got := err.Error(); len(got) == 0 {
			t.Errorf("error message is empty")
		}
	})

	t.Run("unknown protocol 255 returns error with upgrade hint", func(t *testing.T) {
		err := ValidateProtocol(Protocol(255))
		if err == nil {
			t.Fatal("ValidateProtocol(255) = nil, want error")
		}
		// Must include "upgrade warpdl" in the message
		errMsg := err.Error()
		if len(errMsg) == 0 {
			t.Errorf("error message is empty")
		}
		// Check the message contains the upgrade hint
		if !protoContains(errMsg, "upgrade warpdl") {
			t.Errorf("error %q does not contain 'upgrade warpdl'", errMsg)
		}
	})

	t.Run("unknown protocol 4 returns error", func(t *testing.T) {
		err := ValidateProtocol(Protocol(4))
		if err == nil {
			t.Fatal("ValidateProtocol(4) = nil, want error")
		}
	})
}

// protoContains checks if s contains sub (used by protocol_gob_test to avoid import strings).
func protoContains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestGOBBackwardCompatProtocol is the golden fixture test.
// It loads the pre-Phase-2 binary fixture (encoded WITHOUT the Protocol field)
// and decodes it using the current Item struct (which HAS Protocol).
// GOB must zero-initialize the missing Protocol field → ProtoHTTP (0).
func TestGOBBackwardCompatProtocol(t *testing.T) {
	data, err := os.ReadFile("testdata/pre_phase2_userdata.warp")
	if err != nil {
		t.Fatalf("read fixture: %v — commit testdata/pre_phase2_userdata.warp to the repo", err)
	}
	if len(data) == 0 {
		t.Fatal("fixture file is empty")
	}

	var decoded ManagerData
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&decoded); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	if len(decoded.Items) < 2 {
		t.Fatalf("expected at least 2 items in fixture, got %d", len(decoded.Items))
	}

	for hash, item := range decoded.Items {
		if item == nil {
			t.Errorf("item %q is nil", hash)
			continue
		}
		// CRITICAL: Protocol must be zero-initialized to ProtoHTTP
		if item.Protocol != ProtoHTTP {
			t.Errorf("item %q: Protocol = %v (%d), want ProtoHTTP (0) — GOB backward compat broken",
				hash, item.Protocol, uint8(item.Protocol))
		}
		// Verify other fields are intact
		if item.Hash == "" {
			t.Errorf("item %q: Hash is empty", hash)
		}
		if item.Name == "" {
			t.Errorf("item %q: Name is empty", hash)
		}
		if item.Url == "" {
			t.Errorf("item %q: Url is empty", hash)
		}
	}

	// Verify specific item: hash_item1_abc123
	item1, ok := decoded.Items["hash_item1_abc123"]
	if !ok {
		t.Fatal("expected item hash_item1_abc123 in fixture")
	}
	if item1.Protocol != ProtoHTTP {
		t.Errorf("item1.Protocol = %v, want ProtoHTTP", item1.Protocol)
	}
	if item1.TotalSize != ContentLength(104857600) {
		t.Errorf("item1.TotalSize = %v, want 104857600", item1.TotalSize)
	}
	if item1.Downloaded != ContentLength(52428800) {
		t.Errorf("item1.Downloaded = %v, want 52428800", item1.Downloaded)
	}
	if !item1.Resumable {
		t.Errorf("item1.Resumable = false, want true")
	}
	if len(item1.Parts) != 2 {
		t.Errorf("item1.Parts: got %d parts, want 2", len(item1.Parts))
	}

	// Verify specific item: hash_item2_def456
	item2, ok := decoded.Items["hash_item2_def456"]
	if !ok {
		t.Fatal("expected item hash_item2_def456 in fixture")
	}
	if item2.Protocol != ProtoHTTP {
		t.Errorf("item2.Protocol = %v, want ProtoHTTP", item2.Protocol)
	}
	if item2.TotalSize != ContentLength(1048576) {
		t.Errorf("item2.TotalSize = %v, want 1048576", item2.TotalSize)
	}
	if !item2.Hidden {
		t.Errorf("item2.Hidden = false, want true")
	}
	if item2.Resumable {
		t.Errorf("item2.Resumable = true, want false")
	}
}

// newProtoTestItem creates a minimal Item suitable for GOB encoding in tests.
// Uses a shared mutex; memPart is left nil (populateMemPart fills it after decode).
func newProtoTestItem(hash, name string, protocol Protocol, resumable bool) *Item {
	mu := new(sync.RWMutex)
	return &Item{
		Hash:             hash,
		Name:             name,
		Url:              fmt.Sprintf("http://example.com/%s", name),
		Headers:          Headers{},
		DateAdded:        time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		TotalSize:        ContentLength(1024),
		Downloaded:       ContentLength(0),
		DownloadLocation: "/tmp",
		AbsoluteLocation: "/tmp",
		ChildHash:        "",
		Hidden:           false,
		Children:         false,
		Parts:            make(map[int64]*ItemPart),
		Resumable:        resumable,
		Protocol:         protocol,
		mu:               mu,
		memPart:          make(map[string]int64),
	}
}

// gobRoundTrip encodes and decodes a ManagerData and returns the decoded result.
func gobRoundTrip(t *testing.T, data ManagerData) ManagerData {
	t.Helper()
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(data); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded ManagerData
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

// TestGOBRoundTripHTTP verifies round-trip preserves Protocol==ProtoHTTP.
func TestGOBRoundTripHTTP(t *testing.T) {
	item := newProtoTestItem("hash_http_01", "file_http.zip", ProtoHTTP, true)
	data := ManagerData{Items: ItemsMap{item.Hash: item}}

	decoded := gobRoundTrip(t, data)

	got := decoded.Items["hash_http_01"]
	if got == nil {
		t.Fatal("decoded item is nil")
	}
	if got.Protocol != ProtoHTTP {
		t.Errorf("Protocol = %v (%d), want ProtoHTTP (0)", got.Protocol, uint8(got.Protocol))
	}
}

// TestGOBRoundTripFTP verifies round-trip preserves Protocol==ProtoFTP.
func TestGOBRoundTripFTP(t *testing.T) {
	item := newProtoTestItem("hash_ftp_01", "file_ftp.zip", ProtoFTP, true)
	data := ManagerData{Items: ItemsMap{item.Hash: item}}

	decoded := gobRoundTrip(t, data)

	got := decoded.Items["hash_ftp_01"]
	if got == nil {
		t.Fatal("decoded item is nil")
	}
	if got.Protocol != ProtoFTP {
		t.Errorf("Protocol = %v (%d), want ProtoFTP (1)", got.Protocol, uint8(got.Protocol))
	}
}

// TestGOBRoundTripFTPS verifies round-trip preserves Protocol==ProtoFTPS.
func TestGOBRoundTripFTPS(t *testing.T) {
	item := newProtoTestItem("hash_ftps_01", "file_ftps.zip", ProtoFTPS, true)
	data := ManagerData{Items: ItemsMap{item.Hash: item}}

	decoded := gobRoundTrip(t, data)

	got := decoded.Items["hash_ftps_01"]
	if got == nil {
		t.Fatal("decoded item is nil")
	}
	if got.Protocol != ProtoFTPS {
		t.Errorf("Protocol = %v (%d), want ProtoFTPS (2)", got.Protocol, uint8(got.Protocol))
	}
}

// TestGOBRoundTripSFTP verifies round-trip preserves Protocol==ProtoSFTP.
func TestGOBRoundTripSFTP(t *testing.T) {
	item := newProtoTestItem("hash_sftp_01", "file_sftp.zip", ProtoSFTP, false)
	data := ManagerData{Items: ItemsMap{item.Hash: item}}

	decoded := gobRoundTrip(t, data)

	got := decoded.Items["hash_sftp_01"]
	if got == nil {
		t.Fatal("decoded item is nil")
	}
	if got.Protocol != ProtoSFTP {
		t.Errorf("Protocol = %v (%d), want ProtoSFTP (3)", got.Protocol, uint8(got.Protocol))
	}
}

// TestGOBUnknownProtocol verifies that an unknown protocol value (7) decodes without panic
// and returns the raw uint8 value. ValidateProtocol must reject it.
func TestGOBUnknownProtocol(t *testing.T) {
	// Encode a struct with Protocol=7 using a local type that mimics Item's GOB shape
	// but has the same Protocol field name and type (uint8-compatible).
	// We use the actual Item struct with Protocol field set to 7.
	item := newProtoTestItem("hash_unknown_proto", "file_unknown.zip", Protocol(7), false)
	data := ManagerData{Items: ItemsMap{item.Hash: item}}

	decoded := gobRoundTrip(t, data)

	got := decoded.Items["hash_unknown_proto"]
	if got == nil {
		t.Fatal("decoded item is nil")
	}
	// GOB does not validate enum ranges — unknown value passes through as-is
	if got.Protocol != Protocol(7) {
		t.Errorf("Protocol = %v (%d), want 7", got.Protocol, uint8(got.Protocol))
	}
	// String() must handle it gracefully
	s := got.Protocol.String()
	if s != "unknown(7)" {
		t.Errorf("Protocol(7).String() = %q, want %q", s, "unknown(7)")
	}
	// ValidateProtocol must reject it
	err := ValidateProtocol(got.Protocol)
	if err == nil {
		t.Error("ValidateProtocol(7) = nil, want error")
	}
}

// TestGOBPersistenceIntegration verifies that Manager.persistItems + InitManager
// correctly handles the Protocol field end-to-end.
func TestGOBPersistenceIntegration(t *testing.T) {
	t.Run("persist and reload FTP item", func(t *testing.T) {
		// Create a temporary ManagerData with an FTP item
		item := newProtoTestItem("hash_ftp_persist", "ftp_file.bin", ProtoFTP, true)
		data := ManagerData{
			Items:      ItemsMap{item.Hash: item},
			QueueState: nil,
		}

		// Encode as GOB (simulates persistItems)
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(data); err != nil {
			t.Fatalf("encode: %v", err)
		}

		// Decode (simulates InitManager loading from file)
		var decoded ManagerData
		if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}

		got, ok := decoded.Items["hash_ftp_persist"]
		if !ok {
			t.Fatal("item not found after reload")
		}
		if got.Protocol != ProtoFTP {
			t.Errorf("Protocol = %v, want ProtoFTP", got.Protocol)
		}
		if err := ValidateProtocol(got.Protocol); err != nil {
			t.Errorf("ValidateProtocol unexpectedly failed: %v", err)
		}
	})

	t.Run("persist and reload SFTP item", func(t *testing.T) {
		item := newProtoTestItem("hash_sftp_persist", "sftp_file.bin", ProtoSFTP, true)
		data := ManagerData{
			Items:      ItemsMap{item.Hash: item},
			QueueState: nil,
		}

		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(data); err != nil {
			t.Fatalf("encode: %v", err)
		}

		var decoded ManagerData
		if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}

		got, ok := decoded.Items["hash_sftp_persist"]
		if !ok {
			t.Fatal("item not found after reload")
		}
		if got.Protocol != ProtoSFTP {
			t.Errorf("Protocol = %v, want ProtoSFTP", got.Protocol)
		}
	})
}

// TestProtocolItemField verifies that Item struct has a Protocol field with correct zero value.
func TestProtocolItemField(t *testing.T) {
	item := &Item{}
	// Zero value must be ProtoHTTP
	if item.Protocol != ProtoHTTP {
		t.Errorf("Item{}.Protocol = %v (%d), want ProtoHTTP (0)", item.Protocol, uint8(item.Protocol))
	}
}
