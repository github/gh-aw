package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
	"github.com/stretchr/testify/require"
)

func TestCompilerLintRetainedCheckoutException(t *testing.T) {
	t.Parallel()
	const authored = "      - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567\n        with:\n          persist-credentials: true\n"
	for _, tc := range []struct {
		name, job, before, after string
		provenance, modify       bool
		warnings, exemptions     int
	}{
		{"generated", "safe_outputs", "", "", true, false, 1, 1},
		{"authored before", "safe_outputs", authored, "", true, false, 2, 1},
		{"authored after", "safe_outputs", "", authored, true, false, 2, 1},
		{"agent", "agent", "", "", true, false, 1, 0},
		{"custom job", "custom", "", "", true, false, 1, 0},
		{"no provenance", "safe_outputs", "", "", false, false, 1, 0},
		{"modified block", "safe_outputs", "", "", true, true, 1, 0},
		{"ambiguous copy", "safe_outputs", "", "", true, false, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			compiler := NewCompiler()
			data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}}}
			block := strings.Join(compiler.buildSharedPRCheckoutSteps(data), "")
			if !tc.provenance {
				compiler.prCheckoutLintBlock = ""
			}
			if tc.modify {
				block = strings.Replace(block, "Configure Git credentials", "Authored replacement", 1)
			}
			if tc.name == "ambiguous copy" {
				block += block
			}
			source := fmt.Sprintf("jobs:\n  %s:\n    runs-on: ubuntu-latest\n    steps:\n%s%s%s", tc.job, tc.before, block, tc.after)
			graph, err := actions.Parse([]byte(source))
			require.NoError(t, err)
			exempt, err := compiler.safeOutputRetainedCheckoutLintPaths(graph)
			require.NoError(t, err)
			require.Len(t, exempt, tc.exemptions)
			runner, err := workflowlint.New(workflowlint.ArtipackedRule())
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), workflowlint.Input{Workflow: graph})
			require.NoError(t, err)
			require.Len(t, diagnostics, tc.warnings, "the standalone artipacked rule must remain unchanged")
			exemptions := 0
			for _, diagnostic := range diagnostics {
				if _, exists := exempt[diagnostic.Path]; exists {
					exemptions++
				}
			}
			require.Equal(t, tc.exemptions, exemptions)
		})
	}
}

func TestCompilerLintRetainedMultiRepoCheckouts(t *testing.T) {
	t.Parallel()
	compiler := NewCompiler()
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}},
		CheckoutConfigs: []*CheckoutConfig{
			{Path: "./control"},
			{Repository: "example/one", Path: "./one", GitHubToken: "${{ secrets.ONE_TOKEN }}"},
			{Repository: "example/two", Path: "./two", GitHubToken: "${{ secrets.TWO_TOKEN }}", CleanGitCredentials: true},
			{Repository: "example/docs", Path: "./wiki", Wiki: true},
		},
	}
	block := strings.Join(compiler.buildSharedPRCheckoutSteps(data), "")
	require.Contains(t, block, "token: ${{ secrets.ONE_TOKEN }}")
	require.Contains(t, block, "token: ${{ secrets.TWO_TOKEN }}")
	require.NotContains(t, block, "persist-credentials: false")
	block, err := finalizeRunnerTempSafety(renderStepForRunner(block, compiler.formatFrameworkJobRunsOn(data)))
	require.NoError(t, err)
	graph, err := actions.Parse([]byte("jobs:\n  safe_outputs:\n    runs-on: ubuntu-latest\n    steps:\n" + block))
	require.NoError(t, err)
	exempt, err := compiler.safeOutputRetainedCheckoutLintPaths(graph)
	require.NoError(t, err)
	require.Len(t, exempt, strings.Count(block, "persist-credentials: true"))
}
