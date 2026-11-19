package dotimports

import (
	"slices"
	. "strings"
)

func other(values []string) {
	slices.SortFunc(values, func(a, b string) int { return Compare(a, b) }) // want "strings.Compare in a slices comparator"
}
