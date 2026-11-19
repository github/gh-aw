package dotimports

import (
	. "slices"
	"strings"
)

func example(values []string) {
	SortFunc(values, func(a, b string) int { return strings.Compare(a, b) }) // want "strings.Compare in a slices comparator"
}
