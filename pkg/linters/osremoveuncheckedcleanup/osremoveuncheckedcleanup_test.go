//go:build !integration

package osremoveuncheckedcleanup_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/osremoveuncheckedcleanup"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), osremoveuncheckedcleanup.Analyzer, "osremoveuncheckedcleanup")
}
