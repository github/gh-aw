//go:build !integration

package jsondecoderunknownfields_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/jsondecoderunknownfields"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), jsondecoderunknownfields.Analyzer, "jsondecoderunknownfields")
}
