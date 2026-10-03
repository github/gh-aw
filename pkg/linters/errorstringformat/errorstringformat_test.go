//go:build !integration

// Package errorstringformat_test provides tests for the errorstringformat analyzer.
package errorstringformat_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/errorstringformat"
)

func TestErrorStringFormat(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.RunWithSuggestedFixes(t, testdata, errorstringformat.Analyzer, "errorstringformat")
}
