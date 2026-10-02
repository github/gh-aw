//go:build !integration

package fprintferrorunchecked_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/fprintferrorunchecked"
)

func TestFprintfErrorUnchecked(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, fprintferrorunchecked.Analyzer, "fprintferrorunchecked")
}
