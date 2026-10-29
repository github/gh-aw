//go:build !integration

package sortbasic_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/sortbasic"
)

func TestSortBasic(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, sortbasic.Analyzer, "sortbasic")
}
