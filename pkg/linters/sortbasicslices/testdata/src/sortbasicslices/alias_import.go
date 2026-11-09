package sortbasicslices

import s "sort"

func BadAliased(v []int) {
	s.Ints(v) // want `sort\.Ints can be replaced with slices\.Sort`
}
