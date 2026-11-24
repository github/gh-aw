//go:build !integration

package writefilepublicpermsensitive_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	writefilepublicpermsensitive "github.com/github/gh-aw/pkg/linters/writefilepublicperm-sensitive"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), writefilepublicpermsensitive.Analyzer, "writefilepublicperm_sensitive")
}
