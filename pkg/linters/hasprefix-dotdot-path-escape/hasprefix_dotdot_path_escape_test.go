//go:build !integration

package hasprefixdotdotpathescape_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	hasprefixdotdotpathescape "github.com/github/gh-aw/pkg/linters/hasprefix-dotdot-path-escape"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), hasprefixdotdotpathescape.Analyzer, "hasprefixdotdotpathescape")
}
