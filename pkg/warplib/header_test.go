package warplib

import (
	"net/http"
	"testing"
)

func TestHeaders_Update(t *testing.T) {
	type args struct {
		key   string
		value string
	}
	tests := []struct {
		name string
		h    *Headers
		args args
	}{
		{
			"new entry", &Headers{}, args{USER_AGENT_KEY, DEF_USER_AGENT},
		},
		{
			"existing entry", &Headers{{USER_AGENT_KEY, "TestUA/12.3"}}, args{USER_AGENT_KEY, DEF_USER_AGENT},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.h.Update(tt.args.key, tt.args.value)
			i, ok := tt.h.Get(USER_AGENT_KEY)
			if !ok || (*tt.h)[i].Value != tt.args.value {
				t.Errorf("Headers.Update() did not update: %v", tt.h)
			}
		})
	}
}

func TestHeaders_InitOrUpdate(t *testing.T) {
	h := Headers{}
	h.InitOrUpdate(USER_AGENT_KEY, "ua")
	h.InitOrUpdate(USER_AGENT_KEY, "ignored")
	if len(h) != 1 || h[0].Value != "ua" {
		t.Fatalf("InitOrUpdate did not preserve initial value: %v", h)
	}
}

func TestHeaders_Get(t *testing.T) {
	h := Headers{{Key: "A", Value: "1"}}
	if idx, ok := h.Get("A"); !ok || idx != 0 {
		t.Fatalf("expected to find header")
	}
	if _, ok := h.Get("missing"); ok {
		t.Fatalf("expected missing header to be false")
	}
}

func TestHeaders_SetAndAdd(t *testing.T) {
	h := Headers{
		{Key: "X-Test", Value: "one"},
		{Key: "X-Test-2", Value: "two"},
	}
	header := http.Header{}
	h.Set(header)
	if header.Get("X-Test") != "one" || header.Get("X-Test-2") != "two" {
		t.Fatalf("Headers.Set failed: %v", header)
	}
	h.Add(header)
	if header.Values("X-Test")[1] != "one" {
		t.Fatalf("Headers.Add failed: %v", header)
	}
}

// TestHeaders_Update_Cookie tests Update with Cookie header specifically
// to ensure cookie persistence works correctly.
func TestHeaders_Update_Cookie(t *testing.T) {
	tests := []struct {
		name           string
		initial        Headers
		key            string
		value          string
		expectedLen    int
		expectedValue  string
	}{
		{
			name:          "add cookie to empty headers",
			initial:       Headers{},
			key:           "Cookie",
			value:         "session=abc123",
			expectedLen:   1,
			expectedValue: "session=abc123",
		},
		{
			name:          "update existing cookie",
			initial:       Headers{{Key: "Cookie", Value: "session=old"}},
			key:           "Cookie",
			value:         "session=new",
			expectedLen:   1,
			expectedValue: "session=new",
		},
		{
			name:          "add cookie alongside other headers",
			initial:       Headers{{Key: "User-Agent", Value: "WarpDL/1.0"}},
			key:           "Cookie",
			value:         "session=xyz",
			expectedLen:   2,
			expectedValue: "session=xyz",
		},
		{
			name:          "multiple cookies in single header",
			initial:       Headers{},
			key:           "Cookie",
			value:         "session=abc; user=xyz; token=123",
			expectedLen:   1,
			expectedValue: "session=abc; user=xyz; token=123",
		},
		{
			name:          "cookie with special characters",
			initial:       Headers{},
			key:           "Cookie",
			value:         "session=abc%3D123%3Bxyz",
			expectedLen:   1,
			expectedValue: "session=abc%3D123%3Bxyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.initial
			h.Update(tt.key, tt.value)

			if len(h) != tt.expectedLen {
				t.Errorf("expected %d headers, got %d: %+v", tt.expectedLen, len(h), h)
			}

			idx, ok := h.Get(tt.key)
			if !ok {
				t.Errorf("header %q not found", tt.key)
				return
			}

			if h[idx].Value != tt.expectedValue {
				t.Errorf("value mismatch: got %q, want %q", h[idx].Value, tt.expectedValue)
			}
		})
	}
}

// TestHeaders_Update_NoDuplication ensures Update doesn't create duplicates.
func TestHeaders_Update_NoDuplication(t *testing.T) {
	h := Headers{
		{Key: "Cookie", Value: "session=old"},
		{Key: "User-Agent", Value: "WarpDL/1.0"},
	}

	// Update same header multiple times
	h.Update("Cookie", "session=v1")
	h.Update("Cookie", "session=v2")
	h.Update("Cookie", "session=v3")

	if len(h) != 2 {
		t.Errorf("expected 2 headers (no duplicates), got %d: %+v", len(h), h)
	}

	idx, _ := h.Get("Cookie")
	if h[idx].Value != "session=v3" {
		t.Errorf("expected final value, got %q", h[idx].Value)
	}
}

// TestHeaders_Update_PreservesOrder verifies Update preserves header order
// when updating existing headers.
func TestHeaders_Update_PreservesOrder(t *testing.T) {
	h := Headers{
		{Key: "A", Value: "1"},
		{Key: "B", Value: "2"},
		{Key: "C", Value: "3"},
	}

	// Update middle header
	h.Update("B", "updated")

	if len(h) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(h))
	}

	// Verify order preserved
	if h[0].Key != "A" || h[1].Key != "B" || h[2].Key != "C" {
		t.Errorf("order not preserved: %+v", h)
	}

	if h[1].Value != "updated" {
		t.Errorf("B not updated: got %q", h[1].Value)
	}
}
