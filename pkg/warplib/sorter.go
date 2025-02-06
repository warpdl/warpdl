package warplib

import "sort"

type Int64Slice []int64

func (x Int64Slice) Len() int           { return len(x) }
func (x Int64Slice) Less(i, j int) bool { return x[i] < x[j] }
func (x Int64Slice) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }

func SortInt64s(x []int64) { sort.Sort(Int64Slice(x)) }

type ItemSlice []*Item

func (x ItemSlice) Len() int { return len(x) }
func (x ItemSlice) Less(i, j int) bool {
	return x[i].DateAdded.Before(x[j].DateAdded)
}
func (x ItemSlice) Swap(i, j int) { x[i], x[j] = x[j], x[i] }
