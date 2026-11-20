//go:build !integration

package readallunboundedhttpbody_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/readallunboundedhttpbody"
)

func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, readallunboundedhttpbody.Analyzer, "readallunboundedhttpbody")
}
