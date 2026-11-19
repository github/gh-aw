//go:build !integration

package stringscompareinsortfunc_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	stringscompareinsortfunc "github.com/github/gh-aw/pkg/linters/strings-compare-in-sortfunc"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), stringscompareinsortfunc.Analyzer, "comparators", "dotimports")
}
