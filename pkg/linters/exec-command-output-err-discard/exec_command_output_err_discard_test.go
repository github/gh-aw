//go:build !integration

// Package execcommandoutputerrdiscard_test provides tests for the execcommandoutputerrdiscard analyzer.
package execcommandoutputerrdiscard_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	execcommandoutputerrdiscard "github.com/github/gh-aw/pkg/linters/exec-command-output-err-discard"
)

func TestExecCommandOutputErrDiscard(t *testing.T) {
	t.Parallel()
	analysistest.Run(t, analysistest.TestData(), execcommandoutputerrdiscard.Analyzer, "exec_command_output_err_discard")
}
