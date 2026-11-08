//go:build !integration

package blankassignedcleanuperror_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/blankassignedcleanuperror"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), blankassignedcleanuperror.Analyzer, "blankassignedcleanuperror")
}
