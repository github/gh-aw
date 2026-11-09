//go:build !integration

package sortbasicslices_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/sortbasicslices"
)

func TestSortBasicSlices(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, sortbasicslices.Analyzer, "sortbasicslices")
}
