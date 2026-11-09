package sortbasicslices

import s "sort"

func BadAliasedSortStrings(items []string) {
	s.Strings(items) // want `sort\.Strings can be replaced with slices\.Sort`
}

func BadAliasedSortInts(items []int) {
	s.Ints(items) // want `sort\.Ints can be replaced with slices\.Sort`
}

func BadAliasedSortFloat64s(items []float64) {
	s.Float64s(items) // want `sort\.Float64s can be replaced with slices\.Sort`
}
