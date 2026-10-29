package sortbasic

import "sort"

func BadStrings(items []string) {
	sort.Strings(items) // want `sort\.Strings is a legacy helper`
}

func BadInts(items []int) {
	sort.Ints(items) // want `sort\.Ints is a legacy helper`
}

func BadFloat64s(items []float64) {
	sort.Float64s(items) // want `sort\.Float64s is a legacy helper`
}

func GoodSliceFunc(items []string) {
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
}

func GoodSearch(items []string) int {
	return sort.SearchStrings(items, "a")
}
