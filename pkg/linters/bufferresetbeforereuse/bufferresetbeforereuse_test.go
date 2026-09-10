//go:build !integration

// Package bufferresetbeforereuse_test provides tests for the bufferresetbeforereuse analyzer.
package bufferresetbeforereuse_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/bufferresetbeforereuse"
)

func TestBufferResetBeforeReuse(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), bufferresetbeforereuse.Analyzer, "a")
}
