//go:build !integration

package stringscompare_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/stringscompare"
)

func TestStringsCompare(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, stringscompare.Analyzer, "stringscompare")
}
