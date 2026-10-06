//go:build !integration

// Package reflectdeepequalusage_test provides tests for the reflect-deepequal-usage analyzer.
package reflectdeepequalusage_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/reflect-deepequal-usage"
)

func TestReflectDeepEqualUsage(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), reflectdeepequalusage.Analyzer, "a", "b")
}
