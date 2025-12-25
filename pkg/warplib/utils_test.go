package warplib

import (
	"bytes"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestContentLengthString(t *testing.T) {
	if got := ContentLength(1024).String(); got != "1KB" {
		t.Fatalf("expected 1KB, got %q", got)
	}
	if got := ContentLength(0).String(); got != "undefined" {
		t.Fatalf("expected undefined, got %q", got)
	}
}

func TestContentLengthUnknown(t *testing.T) {
	cl := ContentLength(-1)
	if !cl.IsUnknown() {
		t.Fatalf("expected unknown content length")
	}
}

func TestSizeOptionString(t *testing.T) {
	if got := SizeOptionMB.StringFrom(2); got != "2MB" {
		t.Fatalf("expected 2MB, got %q", got)
	}
}

func TestSizeOptionGetFromAndString(t *testing.T) {
	siz, rem := SizeOptionKB.GetFrom(2048)
	if siz != 2 || rem != 0 {
		t.Fatalf("unexpected GetFrom result: %d %d", siz, rem)
	}
	if got := SizeOptionKB.String(ContentLength(2048)); got != "2KB" {
		t.Fatalf("expected 2KB, got %q", got)
	}
}

func TestSortInt64s(t *testing.T) {
	vals := []int64{5, 2, 7, 1}
	SortInt64s(vals)
	for i := 1; i < len(vals); i++ {
		if vals[i-1] > vals[i] {
			t.Fatalf("values not sorted: %v", vals)
		}
	}
}

func TestItemSliceSort(t *testing.T) {
	items := ItemSlice{
		&Item{Name: "b", DateAdded: time.Now()},
		&Item{Name: "a", DateAdded: time.Now().Add(-time.Hour)},
	}
	sort.Sort(items)
	if items[0].Name != "a" {
		t.Fatalf("expected items to be sorted by DateAdded")
	}
}

func TestVMap(t *testing.T) {
	vm := NewVMap[string, int]()
	vm.Set("a", 1)
	vm.Set("b", 2)
	if got := vm.Get("a"); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
	keys, vals := vm.Dump()
	if len(keys) != 2 || len(vals) != 2 {
		t.Fatalf("unexpected dump sizes: %d %d", len(keys), len(vals))
	}
}

func TestCallbackProxyReader(t *testing.T) {
	buf := bytes.NewBufferString("hello")
	var total int
	reader := NewCallbackProxyReader(buf, func(n int) {
		total += n
	})
	out := make([]byte, 5)
	if _, err := reader.Read(out); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected callback total 5, got %d", total)
	}
}

func TestAsyncCallbackProxyReader(t *testing.T) {
	buf := bytes.NewBufferString("hello")
	var total int
	var mu sync.Mutex
	ch := make(chan struct{}, 1)
	reader := NewAsyncCallbackProxyReader(buf, func(n int) {
		mu.Lock()
		defer mu.Unlock()
		total += n
		ch <- struct{}{}
	}, nil)
	out := make([]byte, 5)
	if _, err := reader.Read(out); err != nil {
		t.Fatalf("Read: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("callback not invoked")
	}
	mu.Lock()
	defer mu.Unlock()
	if total != 5 {
		t.Fatalf("expected callback total 5, got %d", total)
	}
}
