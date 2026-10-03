//go:build !integration

package panicinlibrarycode_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	panicinlibrarycode "github.com/github/gh-aw/pkg/linters/panic-in-library-code"
)

func TestPanicInLibraryCode(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, panicinlibrarycode.Analyzer, "panicinlibrarycode", "entrypoint")
}
