//go:build !integration

package osstatignoredeerrorderef_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/os-stat-ignored-error-deref"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), osstatignoredeerrorderef.Analyzer, "osstatignoredeerrorderef")
}
