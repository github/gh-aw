//go:build !integration

package osstatignorederrorderef_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	osstatignorederrorderef "github.com/github/gh-aw/pkg/linters/os-stat-ignored-error-deref"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, osstatignorederrorderef.Analyzer, "osstatignorederrorderef")
}
