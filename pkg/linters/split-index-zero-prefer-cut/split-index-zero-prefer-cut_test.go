//go:build !integration

package splitindexzeroprefercut_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	splitindexzeroprefercut "github.com/github/gh-aw/pkg/linters/split-index-zero-prefer-cut"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), splitindexzeroprefercut.Analyzer, "splitindexzeroprefercut")
}
