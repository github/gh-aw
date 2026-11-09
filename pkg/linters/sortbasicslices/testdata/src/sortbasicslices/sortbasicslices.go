package sortbasicslices

import (
	"slices"
	"sort"
)

func BadSortStrings(items []string) {
	sort.Strings(items) // want `sort\.Strings is not type-safe; use slices\.Sort instead`
}

func BadSortInts(items []int) {
	sort.Ints(items) // want `sort\.Ints is not type-safe; use slices\.Sort instead`
}

func BadSortFloat64s(items []float64) {
	sort.Float64s(items) // want `sort\.Float64s is not type-safe; use slices\.Sort instead`
}

func BadParenSortStrings(items []string) {
	(sort.Strings)(items) // want `sort\.Strings is not type-safe`
}

func GoodSorts() {
	sort.Sort(nil)
	sort.Stable(nil)
	sort.Slice(nil, func(i, j int) bool { return i < j })
	sort.SliceStable(nil, func(i, j int) bool { return i < j })
	slices.Sort([]int{})
}

func SuppressedSortStrings(items []string) {
	sort.Strings(items) //nolint:sortbasicslices
}
