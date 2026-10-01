//go:build !integration

package typeassertionnil_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/typeassertionnil"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, typeassertionnil.Analyzer, "typeassertionnil")
}
