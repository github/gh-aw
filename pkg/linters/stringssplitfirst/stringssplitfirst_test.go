//go:build !integration

package stringssplitfirst_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/stringssplitfirst"
)

func TestStringsSplitFirst(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, stringssplitfirst.Analyzer, "stringssplitfirst")
}
