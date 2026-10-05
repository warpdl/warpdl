package warplib

import (
	"bytes"
	"math"
	"slices"
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

// TestSortInt64s pins the exported SortInt64s contract: the caller's slice is
// sorted in place, ascending, and nil/empty input is tolerated.
// Boundary cases: nil, empty, single element, already sorted, reverse sorted,
// duplicates, negatives, and the int64 extremes.
func TestSortInt64s(t *testing.T) {
	tests := []struct {
		name string
		in   []int64
		want []int64
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: []int64{}, want: nil},
		{name: "single element", in: []int64{42}, want: []int64{42}},
		{name: "already sorted", in: []int64{1, 2, 3}, want: []int64{1, 2, 3}},
		{name: "reverse sorted", in: []int64{5, 2, 7, 1}, want: []int64{1, 2, 5, 7}},
		{name: "duplicates", in: []int64{3, 1, 3, 2, 1}, want: []int64{1, 1, 2, 3, 3}},
		{name: "negatives", in: []int64{0, -5, 5, -1}, want: []int64{-5, -1, 0, 5}},
		{
			name: "int64 extremes",
			in:   []int64{math.MaxInt64, 0, math.MinInt64},
			want: []int64{math.MinInt64, 0, math.MaxInt64},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SortInt64s(tt.in)
			if !slices.Equal(tt.in, tt.want) {
				t.Errorf("SortInt64s result = %v, want %v", tt.in, tt.want)
			}
		})
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
