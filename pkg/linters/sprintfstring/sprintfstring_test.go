//go:build !integration

// Package sprintfstring_test provides tests for the sprintfstring analyzer.
package sprintfstring_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/sprintfstring"
)

func TestSprintfString(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, sprintfstring.Analyzer, "sprintfstring")
}
