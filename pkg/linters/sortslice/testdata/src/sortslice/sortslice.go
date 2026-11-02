package sortslice

import "sort"

func BadSlice(items []string) {
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] }) // want `sort\.Slice is not type-safe`
}

func BadSliceStable(items []string) {
	sort.SliceStable(items, func(i, j int) bool { return items[i] < items[j] }) // want `sort\.SliceStable is not type-safe`
}

func BadParenSlice(items []string) {
	(sort.Slice)(items, func(i, j int) bool { return items[i] < items[j] }) // want `sort\.Slice is not type-safe`
}

func GoodSortStrings(items []string) {
	sort.Strings(items)
}

func GoodSortInts(items []int) {
	sort.Ints(items)
}
