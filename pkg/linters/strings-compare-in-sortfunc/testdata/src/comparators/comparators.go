package comparators

import (
	"cmp"
	s "slices"
	str "strings"
)

func examples(values []string) {
	s.SortFunc(values, func(a, b string) int { return str.Compare(a, b) })                           // want "strings.Compare in a slices comparator can use cmp.Compare"
	s.SortStableFunc(values, func(a, b string) int { return str.Compare(a, b) })                     // want "strings.Compare in a slices comparator"
	_ = s.MinFunc(values, func(a, b string) int { return str.Compare(a, b) })                        // want "strings.Compare in a slices comparator"
	_ = s.MaxFunc(values, func(a, b string) int { return str.Compare(a, b) })                        // want "strings.Compare in a slices comparator"
	_, _ = s.BinarySearchFunc(values, "x", func(a, b string) int { return str.Compare(a, b) })       // want "strings.Compare in a slices comparator"
	(s.SortFunc[[]string, string])(values, (func(a, b string) int { return ((str.Compare)(a, b)) })) // want "strings.Compare in a slices comparator"

	s.SortFunc(values, func(a, b string) int { return cmp.Compare(a, b) })
	s.SortFunc(values, func(a, b string) int { return -str.Compare(a, b) })
	s.SortFunc(values, func(a, b string) int {
		if a == b {
			return 0
		}
		return str.Compare(a, b)
	})
	s.SortFunc(values, str.Compare)
	_ = str.Compare("a", "b")
	s.SortFunc(values, func(a, b string) int {
		//nolint:stringscompareinsortfunc
		return str.Compare(a, b)
	})
	s.SortFunc(values, func(a, b string) int { return localCompare(a, b) })
}

func localCompare(a, b string) int { return str.Compare(a, b) }

type fakeSlices struct{}

func (fakeSlices) SortFunc(_ []string, f func(string, string) int) { _ = f }

func unrelated(values []string) {
	fakeSlices{}.SortFunc(values, func(a, b string) int { return str.Compare(a, b) })
}
