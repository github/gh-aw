//go:build !integration

package indexcomparetocontains_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/indexcomparetocontains"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.RunWithSuggestedFixes(t, testdata, indexcomparetocontains.Analyzer, "indexcomparetocontains")
}
