//go:build !integration

package bufioscannererunchecked_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/bufioscannererunchecked"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, bufioscannererunchecked.Analyzer, "basic")
}
