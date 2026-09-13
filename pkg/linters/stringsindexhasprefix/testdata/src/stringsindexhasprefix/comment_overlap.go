package stringsindexhasprefix

import "strings"

func overlapIndexHasPrefix(s, sub string) bool {
	return strings.Index(s /* keep */, sub) == 0 // want `use strings\.HasPrefix\(s, sub\) instead of strings\.Index comparison`
}
