//go:build !integration

package ignoredlocalerror_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	ignoredlocalerror "github.com/github/gh-aw/pkg/linters/ignored-local-error"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), ignoredlocalerror.Analyzer, "ignoredlocalerror")
}
