//go:build !integration

package logfatallibrary_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/logfatallibrary"
)

func TestLogFatalLibrary(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, logfatallibrary.Analyzer, "logfatallibrary", "entrypoint")
}
