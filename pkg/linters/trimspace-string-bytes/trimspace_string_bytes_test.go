//go:build !integration

package trimspacestringbytes_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	trimspacestringbytes "github.com/github/gh-aw/pkg/linters/trimspace-string-bytes"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), trimspacestringbytes.Analyzer, "trimspace-string-bytes")
}
