//go:build !integration

package uncheckedsliceindex_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	uncheckedsliceindex "github.com/github/gh-aw/pkg/linters/unchecked-slice-index"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, uncheckedsliceindex.Analyzer, "uncheckedsliceindex")
}
