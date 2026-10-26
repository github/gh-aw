//go:build !integration

package splitindexzero_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/splitindexzero"
)

func TestSplitIndexZero(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, splitindexzero.Analyzer, "splitindexzero")
}
