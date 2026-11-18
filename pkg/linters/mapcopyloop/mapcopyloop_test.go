//go:build !integration

package mapcopyloop_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/mapcopyloop"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.RunWithSuggestedFixes(t, testdata, mapcopyloop.Analyzer, "mapcopyloop")
	analysistest.Run(t, testdata, mapcopyloop.Analyzer, "mapcopyloopnoimport")
}
