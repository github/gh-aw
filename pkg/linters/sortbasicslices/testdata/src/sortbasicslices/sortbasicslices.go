package sortbasicslices

import (
	"slices"
	"sort"
)

func BadStrings(v []string) {
	sort.Strings(v) // want `sort\.Strings can be replaced with slices\.Sort`
}

func BadInts(v []int) {
	sort.Ints(v) // want `sort\.Ints can be replaced with slices\.Sort`
}

func BadFloat64s(v []float64) {
	sort.Float64s(v) // want `sort\.Float64s can be replaced with slices\.Sort`
}

func Suppressed(v []string) {
	sort.Strings(v) //nolint:sortbasicslices
}

func GoodSlicesSort(v []string) {
	slices.Sort(v)
}

func GoodSlice(v []string) {
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
}

func GoodSort(v sort.StringSlice) {
	sort.Sort(v)
	v.Sort()
}
