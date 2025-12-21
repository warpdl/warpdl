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
