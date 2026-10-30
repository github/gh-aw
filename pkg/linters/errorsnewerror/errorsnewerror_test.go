//go:build !integration

// Package errorsnewerror_test provides tests for the errorsnewerror analyzer.
package errorsnewerror_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/errorsnewerror"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, errorsnewerror.Analyzer, "errorsnewerror")
}
