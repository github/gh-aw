//go:build !integration

package typeassertionokdiscarded_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/typeassertionokdiscarded"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, typeassertionokdiscarded.Analyzer, "typeassertionokdiscarded")
}
