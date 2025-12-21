package warplib

import "sort"

// Int64Slice attaches the methods of sort.Interface to []int64, sorting in increasing order.
type Int64Slice []int64

// Len returns the number of elements in the slice.
func (x Int64Slice) Len() int { return len(x) }

// Less reports whether the element at index i should sort before the element at index j.
func (x Int64Slice) Less(i, j int) bool { return x[i] < x[j] }

// Swap exchanges the elements at indices i and j.
func (x Int64Slice) Swap(i, j int) { x[i], x[j] = x[j], x[i] }

// SortInt64s sorts a slice of int64 values in increasing order.
func SortInt64s(x []int64) { sort.Sort(Int64Slice(x)) }

// ItemSlice attaches the methods of sort.Interface to []*Item, sorting by DateAdded in chronological order.
type ItemSlice []*Item

// Len returns the number of elements in the slice.
func (x ItemSlice) Len() int { return len(x) }

// Less reports whether the element at index i was added before the element at index j.
func (x ItemSlice) Less(i, j int) bool {
	return x[i].DateAdded.Before(x[j].DateAdded)
}

// Swap exchanges the elements at indices i and j.
func (x ItemSlice) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
