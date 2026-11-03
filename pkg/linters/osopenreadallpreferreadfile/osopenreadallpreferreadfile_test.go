//go:build !integration

package osopenreadallpreferreadfile_test

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/osopenreadallpreferreadfile"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	if err := analysis.Validate([]*analysis.Analyzer{osopenreadallpreferreadfile.Analyzer}); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), osopenreadallpreferreadfile.Analyzer, "osopenreadallpreferreadfile")
}
