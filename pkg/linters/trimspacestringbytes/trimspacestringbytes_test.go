//go:build !integration

package trimspacestringbytes_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/trimspacestringbytes"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), trimspacestringbytes.Analyzer, "trimspacestringbytes")
}

func TestCoverageGate(t *testing.T) {
	if os.Getenv("TRIMSPACE_COVERAGE_TEST") == "1" {
		analysistest.Run(t, analysistest.TestData(), trimspacestringbytes.Analyzer, "coverage")
		return
	}
	fixture := filepath.Join(analysistest.TestData(), "src", "coverage", "coverage.go")
	profile := filepath.Join(t.TempDir(), "coverage.out")
	data := fmt.Sprintf("mode: count\n%s:6.1,6.100 1 5\n%s:7.1,7.100 1 0\n", fixture, fixture)
	if err := os.WriteFile(profile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCoverageGate$")
	cmd.Env = append(os.Environ(), "TRIMSPACE_COVERAGE_TEST=1", "GH_AW_LINT_COVERAGE_PROFILE="+profile)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("coverage-gated analyzer: %v\n%s", err, output)
	}
}
