//go:build !integration

package boolliteralcompare_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/boolliteralcompare"
)

func TestBoolLiteralCompare(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.RunWithSuggestedFixes(t, testdata, boolliteralcompare.Analyzer, "boolliteralcompare")
}
