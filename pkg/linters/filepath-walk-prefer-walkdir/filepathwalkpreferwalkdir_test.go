//go:build !integration

package filepathwalkpreferwalkdir_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	filepathwalkpreferwalkdir "github.com/github/gh-aw/pkg/linters/filepath-walk-prefer-walkdir"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, filepathwalkpreferwalkdir.Analyzer, "filepathwalkpreferwalkdir")
}
