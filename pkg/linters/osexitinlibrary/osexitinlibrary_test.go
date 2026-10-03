//go:build !integration

package osexitinlibrary_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/osexitinlibrary"
)

func TestOsExitInLibrary(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, osexitinlibrary.Analyzer, "osexitinlibrary", "entrypoint")
}
