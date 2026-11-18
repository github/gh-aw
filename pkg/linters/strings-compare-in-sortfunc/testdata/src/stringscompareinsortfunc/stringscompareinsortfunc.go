package stringscompareinsortfunc

import (
	"cmp"
	"slices"
	"strings"
)

type item struct{ Name string }

func positives(xs []item, names []string) {
	slices.SortFunc(xs, func(a, b item) int {
		return strings.Compare(a.Name, b.Name) // want `use cmp\.Compare instead of strings\.Compare in sort comparator`
	})
	slices.SortStableFunc(xs, func(a, b item) int {
		return strings.Compare(a.Name, b.Name) // want `use cmp\.Compare instead of strings\.Compare in sort comparator`
	})
	_ = slices.MinFunc(xs, func(a, b item) int {
		return (strings.Compare(a.Name, b.Name)) // want `use cmp\.Compare instead of strings\.Compare in sort comparator`
	})
	_ = slices.MaxFunc(xs, (func(a, b item) int {
		return strings.Compare(a.Name, b.Name) // want `use cmp\.Compare instead of strings\.Compare in sort comparator`
	}))
	_, _ = slices.BinarySearchFunc(xs, "x", func(a item, t string) int {
		return strings.Compare(a.Name, t) // want `use cmp\.Compare instead of strings\.Compare in sort comparator`
	})
}

func negatives(xs []item) int {
	slices.SortFunc(xs, func(a, b item) int {
		return cmp.Compare(a.Name, b.Name)
	})
	slices.SortFunc(xs, func(a, b item) int {
		return len(a.Name) - len(b.Name)
	})
	slices.SortFunc(xs, func(a, b item) int {
		if a.Name == "" {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return strings.Compare("a", "b")
}
