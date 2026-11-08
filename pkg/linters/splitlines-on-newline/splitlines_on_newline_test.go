//go:build !integration

package splitlinesonnewline_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	splitlinesonnewline "github.com/github/gh-aw/pkg/linters/splitlines-on-newline"
)

func TestSplitLinesOnNewline(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), splitlinesonnewline.Analyzer, "splitlinesonnewline")
}
