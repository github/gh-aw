//go:build !integration

package prefercutprefix_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/prefercutprefix"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), prefercutprefix.Analyzer, "prefercutprefix")
}
