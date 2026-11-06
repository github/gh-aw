//go:build !integration

package blankassignedcleanuperror_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	blankassignedcleanuperror "github.com/github/gh-aw/pkg/linters/blank-assigned-cleanup-error"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), blankassignedcleanuperror.Analyzer, "blankassignedcleanuperror")
}

func TestAnalyzerMetadata(t *testing.T) {
	t.Parallel()
	if blankassignedcleanuperror.Analyzer.Name != "blankassignedcleanuperror" {
		t.Fatalf("unexpected name %q", blankassignedcleanuperror.Analyzer.Name)
	}
	const url = "https://github.com/github/gh-aw/tree/main/pkg/linters/blank-assigned-cleanup-error"
	if blankassignedcleanuperror.Analyzer.URL != url {
		t.Fatalf("unexpected URL %q", blankassignedcleanuperror.Analyzer.URL)
	}
}
