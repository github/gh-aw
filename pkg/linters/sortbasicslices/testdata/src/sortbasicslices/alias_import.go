package sortbasicslices

import s "sort"

func BadAliasedSortStrings(items []string) {
	s.Strings(items) // want `sort\.Strings is not type-safe`
}

func BadAliasedSortInts(items []int) {
	s.Ints(items) // want `sort\.Ints is not type-safe`
}

func BadAliasedSortFloat64s(items []float64) {
	s.Float64s(items) // want `sort\.Float64s is not type-safe`
}
