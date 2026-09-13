package lenstringsplit

import "strings"

func overlapLenSplit(s string) int {
	return len(strings.Split(s /* keep */, ",")) // want `len\(strings\.Split\(\.\.\.`
}
