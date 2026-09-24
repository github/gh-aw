//go:build !integration

package loopappendinefficency_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/loopappendinefficency"
)

func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, loopappendinefficency.Analyzer, "loopappendinefficency")
}

// TestCoverageGatingNoProfile verifies the permissive-fallback contract: with
// GH_AW_LINT_COVERAGE_PROFILE unset the analyzer reports all findings.
func TestCoverageGatingNoProfile(t *testing.T) {
	t.Setenv("GH_AW_LINT_COVERAGE_PROFILE", "")
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, loopappendinefficency.Analyzer, "loopappendinefficency")
}

// TestCoverageGatingThresholdZero verifies that setting hot-threshold=0
// disables the coverage gate entirely: all findings are reported regardless of
// any loaded profile. The four contractual cases (no profile, threshold=0,
// below threshold, at/above threshold) are unit-tested in
// pkg/linters/internal/coverage/coverage_test.go; these integration tests
// confirm the analyzer correctly wires the gate at the report site.
func TestCoverageGatingThresholdZero(t *testing.T) {
	if err := loopappendinefficency.Analyzer.Flags.Set("hot-threshold", "0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = loopappendinefficency.Analyzer.Flags.Set("hot-threshold", "1")
	})
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, loopappendinefficency.Analyzer, "loopappendinefficency")
}
