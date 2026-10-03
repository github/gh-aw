//go:build !integration

package rawloginlib_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/rawloginlib"
)

func TestRawLogInLib(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, rawloginlib.Analyzer, "rawloginlib", "entrypoint")
}
