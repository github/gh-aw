package splitlinesonnewline

import (
	"strings"
)

type fake struct{}

func (fake) Split(s, sep string) []string { return nil }

const nl = "\n"

func bad(s string) {
	for _, line := range strings.Split(s, "\n") { // want `strings.Split\(s, "\\n"\) leaves a trailing empty element`
		_ = line
	}
	_ = strings.Split(s, nl) // want `strings.Split\(s, "\\n"\) leaves a trailing empty element`
}

func suppressed(s string) {
	_ = strings.Split(s, "\n") //nolint:splitlinesonnewline // intentional
}

func good(s string) {
	_ = strings.Split(s, ",")
	_ = strings.Split(s, "\r\n")
	_ = strings.Split(s, "\n\n")
	for line := range strings.Lines(s) {
		_ = line
	}
	for line := range strings.SplitSeq(s, "\n") {
		_ = line
	}
	var f fake
	_ = f.Split(s, "\n")
	sep := ","
	_ = strings.Split(s, sep)
}
