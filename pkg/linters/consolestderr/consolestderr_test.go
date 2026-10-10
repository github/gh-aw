//go:build !integration

package consolestderr_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/consolestderr"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), consolestderr.Analyzer, "consolestderr")
}
