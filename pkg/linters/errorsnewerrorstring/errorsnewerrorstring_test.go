//go:build !integration

package errorsnewerrorstring_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/errorsnewerrorstring"
)

func TestErrorsNewErrorString(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), errorsnewerrorstring.Analyzer, "errorsnewerrorstring")
}
