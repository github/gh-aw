package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilerLintUntrustedDataFlows(t *testing.T) {
	const job = "on: issue_comment\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n"
	for _, tt := range []struct {
		name, steps, rule string
	}{
		{
			"environment file",
			"      - env: {BODY: '${{ github.event.comment.body }}'}\n        run: printf 'VALUE=%s\\n' \"$BODY\" >> \"$GITHUB_ENV\"\n",
			"environment-file-injection",
		},
		{
			"output interpolation",
			"      - id: capture\n        env: {BODY: '${{ github.event.comment.body }}'}\n        run: printf 'value=%s\\n' \"$BODY\" >> \"$GITHUB_OUTPUT\"\n" +
				"      - run: echo '${{ steps.capture.outputs.value }}'\n",
			"template-injection",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithLint(true))
			err := compiler.lintCompiledWorkflow(&WorkflowData{}, job+tt.steps, "test.md")
			require.ErrorContains(t, err, "["+tt.rule+"]")
			require.NotContains(t, err.Error(), "github.event.comment.body")
		})
	}
	compiler := NewCompiler(WithLint(true))
	data := &WorkflowData{Tools: map[string]any{"github": map[string]any{"min-integrity": "approved"}}}
	require.NoError(t, compiler.lintCompiledWorkflow(data, job+"      - shell: pwsh\n        run: Write-Output public\n", "test.md"))
	require.Zero(t, compiler.GetWarningCount(), "flow coverage remains available through gh aw lint, not compiler warning counts")
}
