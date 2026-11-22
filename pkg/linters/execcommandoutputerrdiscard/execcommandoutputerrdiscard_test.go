//go:build !integration

package execcommandoutputerrdiscard_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/github/gh-aw/pkg/linters/execcommandoutputerrdiscard"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), execcommandoutputerrdiscard.Analyzer, "execcommandoutputerrdiscard")
}
