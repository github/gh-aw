//go:build !integration

// Package unchecked_deferredclose_test provides tests for the unchecked_deferredclose analyzer.
package unchecked_deferredclose_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	unchecked_deferredclose "github.com/github/gh-aw/pkg/linters/unchecked-deferred-close"
)

func TestUncheckedDeferredClose(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), unchecked_deferredclose.Analyzer, "basic")
}
