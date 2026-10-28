package splitindexzero

import (
	"strings"
	str "strings"
)

const slash = "/"

func flagged(s string) {
	_ = strings.Split(s, "/")[0]                         // want `strings\.Split\(\.\.\.\)\[0\] allocates`
	_ = strings.SplitN(s, "@", 2)[0]                     // want `strings\.SplitN\(\.\.\.\)\[0\] allocates`
	_ = (strings.Split(s, slash))[0]                     // want `strings\.Split\(\.\.\.\)\[0\] allocates`
	_ = str.Split(s, ",")[0]                             // want `strings\.Split\(\.\.\.\)\[0\] allocates`
	_ = strings.TrimSpace(strings.SplitN(s, " #", 2)[0]) // want `strings\.SplitN\(\.\.\.\)\[0\] allocates`
}

func notFlagged(s, sep string) {
	_ = strings.Split(s, "/")[1]
	_ = strings.Split(s, "")[0]
	_ = strings.Split(s, sep)[0]
	_ = strings.SplitN(s, "@", 3)[0]
	_ = strings.SplitN(s, "@", -1)[0]
	parts := strings.Split(s, "/")
	_ = parts[0]
	_, _, _ = strings.Cut(s, "/")
}

func suppressed(s string) {
	_ = strings.Split(s, "/")[0] //nolint:splitindexzero
}

type fakeStrings struct{}

func (fakeStrings) Split(s, sep string) []string { return nil }

func shadowed(s string) {
	strings := fakeStrings{}
	_ = strings.Split(s, "/")[0]
}
