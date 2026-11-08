//go:build !integration

package filepathwalkpreferwalkdir_test

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/filepathwalkpreferwalkdir"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	if err := analysis.Validate([]*analysis.Analyzer{filepathwalkpreferwalkdir.Analyzer}); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), filepathwalkpreferwalkdir.Analyzer, "filepathwalkpreferwalkdir")
}
