//go:build !integration

// Package loopvarmutationinclosure_test provides tests for the loopvarmutationinclosure analyzer.
package loopvarmutationinclosure_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/loopvarmutationinclosure"
)

func TestLoopVarMutationInClosure(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), loopvarmutationinclosure.Analyzer, "loopvarmutationinclosure")
}
