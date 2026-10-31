package stringscompare

import (
	"bytes"
	"cmp"
	"slices"
	"strings"
)

func bad(a, b string) int {
	return strings.Compare(a, b) // want `use cmp\.Compare instead of strings\.Compare`
}

func badSort(xs []string) {
	slices.SortFunc(xs, func(a, b string) int {
		return strings.Compare(a, b) // want `use cmp\.Compare instead of strings\.Compare`
	})
}

func suppressed(a, b string) int {
	return strings.Compare(a, b) //nolint:stringscompare
}

func goodCmp(a, b string) int {
	return cmp.Compare(a, b)
}

func goodBytes(a, b []byte) int {
	return bytes.Compare(a, b)
}
