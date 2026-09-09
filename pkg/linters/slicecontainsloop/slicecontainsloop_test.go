//go:build !integration

package slicecontainsloop_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/slicecontainsloop"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), slicecontainsloop.Analyzer, "slicecontainsloop")
}
