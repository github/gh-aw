//go:build !integration

package osopenreadallpreferreadfile_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	osopenreadallpreferreadfile "github.com/github/gh-aw/pkg/linters/osopen-readall-prefer-readfile"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), osopenreadallpreferreadfile.Analyzer, "osopen-readall-prefer-readfile")
}
