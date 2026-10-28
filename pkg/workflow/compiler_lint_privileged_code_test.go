package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilerLintPrivilegedCodeDiagnostics(t *testing.T) {
	t.Parallel()
	const pin = "0123456789abcdef0123456789abcdef01234567"
	const job = "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n"
	for _, tt := range []struct {
		name, source, safe string
	}{
		{
			name: "unsafe checkout opt out",
			source: "on: issue_comment\n" + job +
				"      - uses: actions/checkout@" + pin + "\n        with: {persist-credentials: false, allow-unsafe-pr-checkout: true}\n",
			safe: "allow-unsafe-pr-checkout: true",
		},
		{
			name: "cross-run execution",
			source: "on: {workflow_run: {workflows: [CI], branches: [main]}}\n" + job +
				"      - uses: actions/download-artifact@" + pin + "\n        with: {github-token: '${{ github.token }}', run-id: '${{ github.event.workflow_run.id }}', path: incoming}\n" +
				"      - run: bash incoming/run.sh\n",
			safe: "bash incoming/run.sh",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithLint(true))
			require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{}, tt.source, "test.md"))
			require.Equal(t, 1, compiler.GetWarningCount(), "default compiler lint must report the new warning")
			replacement := "allow-unsafe-pr-checkout: false"
			if tt.name == "cross-run execution" {
				replacement = "cat incoming/run.sh"
			}
			compiler = NewCompiler(WithLint(true))
			require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{}, strings.ReplaceAll(tt.source, tt.safe, replacement), "test.md"))
			require.Zero(t, compiler.GetWarningCount(), "safe base configuration and data-only consumers remain accepted")
			compiler = NewCompiler(WithLint(false))
			require.NoError(t, compiler.lintCompiledWorkflow(&WorkflowData{}, tt.source, "test.md"))
			require.Zero(t, compiler.GetWarningCount(), "lint opt-out is not a security guarantee")
		})
	}
}
