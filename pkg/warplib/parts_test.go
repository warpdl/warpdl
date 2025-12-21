package warplib

import (
	"net/http"
	"testing"
)

func TestSetRange(t *testing.T) {
	header := http.Header{}
	setRange(header, 5, 10)
	if header.Get("Range") != "bytes=5-10" {
		t.Fatalf("unexpected range: %s", header.Get("Range"))
	}
	setRange(header, 0, 0)
	if header.Get("Range") != "bytes=0-" {
		t.Fatalf("unexpected range for open end: %s", header.Get("Range"))
	}
}
