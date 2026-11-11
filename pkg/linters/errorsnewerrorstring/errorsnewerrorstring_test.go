//go:build !integration

// Package errorsnewerrorstring_test provides tests for the errorsnewerrorstring analyzer.
package errorsnewerrorstring_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/errorsnewerrorstring"
)

func TestErrorsNewErrorString(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, errorsnewerrorstring.Analyzer, "errorsnewerrorstring")
}
