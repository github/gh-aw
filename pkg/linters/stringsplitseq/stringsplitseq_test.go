//go:build !integration

package stringsplitseq_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/stringsplitseq"
)

func TestStringSplitSeq(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.RunWithSuggestedFixes(t, testdata, stringsplitseq.Analyzer, "stringsplitseq")
}
