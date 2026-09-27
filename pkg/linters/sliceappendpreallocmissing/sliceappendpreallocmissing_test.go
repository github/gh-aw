//go:build !integration

// Package sliceappendpreallocmissing_test provides tests for the sliceappendpreallocmissing analyzer.
package sliceappendpreallocmissing_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/sliceappendpreallocmissing"
)

func TestSliceAppendPreallocMissing(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), sliceappendpreallocmissing.Analyzer, "a")
}
