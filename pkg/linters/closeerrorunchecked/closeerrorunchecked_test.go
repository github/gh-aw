//go:build !integration

package closeerrorunchecked_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/closeerrorunchecked"
)

func TestCloseErrorUnchecked(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, closeerrorunchecked.Analyzer, "closeerrorunchecked")
	analysistest.Run(t, testdata, closeerrorunchecked.Analyzer, "closeerrorunchecked/customerror")
}
