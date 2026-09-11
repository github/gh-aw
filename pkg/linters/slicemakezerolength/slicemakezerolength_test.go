//go:build !integration

package slicemakezerolength_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/slicemakezerolength"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, slicemakezerolength.Analyzer, "slicemakezerolength")
}
