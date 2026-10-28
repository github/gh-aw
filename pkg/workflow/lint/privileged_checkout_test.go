package lint

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivilegedCheckoutInputs(t *testing.T) {
	for _, tt := range []struct {
		name, inputs, field string
	}{
		{"enabled", "allow-unsafe-pr-checkout: true", "allow-unsafe-pr-checkout"},
		{"string enabled", "allow-unsafe-pr-checkout: 'true'", "allow-unsafe-pr-checkout"},
		{"literal enabled", "allow-unsafe-pr-checkout: '${{ true }}'", "allow-unsafe-pr-checkout"},
		{"dynamic opt out", "allow-unsafe-pr-checkout: '${{ vars.PRIVATE_VALUE }}'", "allow-unsafe-pr-checkout"},
		{"disabled", "allow-unsafe-pr-checkout: false", ""},
		{"string disabled", "allow-unsafe-pr-checkout: 'false'", ""},
		{"literal disabled", "allow-unsafe-pr-checkout: '${{ false }}'", ""},
		{"quoted literal disabled", `allow-unsafe-pr-checkout: "${{ 'false' }}"`, ""},
		{"head sha", "ref: '${{ github.event.pull_request.head.sha }}'", "ref"},
		{"bracket head sha", `ref: "${{ github['event']['pull_request']['head']['sha'] }}"`, "ref"},
		{"head ref", "ref: '${{ github.head_ref }}'", "ref"},
		{"fork repository", "repository: '${{ github.event.pull_request.head.repo.full_name }}'", "repository"},
		{"upstream head", "ref: '${{ github.event.workflow_run.head_sha }}'", "ref"},
		{"dispatch ref", "ref: '${{ github.event.client_payload.PRIVATE_VALUE }}'", "ref"},
		{"literal pull ref", "ref: refs/pull/42/head", "ref"},
		{"base sha", "ref: '${{ github.event.pull_request.base.sha }}'", ""},
		{"base ref", "ref: '${{ github.event.pull_request.base.ref }}'", ""},
		{"base repository", "repository: '${{ github.repository }}'", ""},
		{"trusted literal pair", "repository: trusted/tools, ref: main", ""},
		{"literal text not source", `ref: "${{ 'github.event.pull_request.head.sha' }}"`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				source := "on: pull_request_target\n" + contractJob +
					"    steps:\n      - uses: actions/checkout@v7\n        with: {" + tt.inputs + "}\n"
				diagnostics := integrityTestRun(t, PrivilegedCheckoutRule(), ruleContractInput(t, source, target, false))
				if tt.field == "" {
					assert.Empty(t, diagnostics)
				} else {
					require.Len(t, diagnostics, 1)
					assert.Equal(t, "jobs.check.steps[0].with."+tt.field, diagnostics[0].Path)
					assert.Equal(t, Warning, diagnostics[0].Severity)
					assert.NotContains(t, diagnostics[0].Message, "PRIVATE_VALUE")
				}
			}
		})
	}
}

func TestPrivilegedCheckoutCommands(t *testing.T) {
	for _, tt := range []struct {
		script string
		count  int
	}{
		{"gh pr checkout 42", 1},
		{`gh pr view 42 --json headRefOid`, 0},
		{`echo 'gh pr checkout 42'`, 0},
		{"# gh pr checkout 42\ngit status", 0},
		{`git fetch origin refs/pull/42/head`, 1},
		{`git checkout '${{ github.event.pull_request.head.sha }}'`, 1},
		{`git clone 'https://github.com/${{ github.event.client_payload.PRIVATE_VALUE }}'`, 1},
		{`git checkout '${{ github.event.pull_request.base.sha }}'`, 0},
		{`git show '${{ github.event.pull_request.head.sha }}'`, 0},
		{`git fetch origin main`, 0},
		{`printf '%s' 'git checkout refs/pull/42/head'`, 0},
		{`bash -c 'gh pr checkout 42'`, 0}, // Nested command strings are outside this bounded check.
	} {
		t.Run(tt.script, func(t *testing.T) {
			source := "on: issue_comment\n" + contractJob + "    steps:\n      - run: |\n          " + strings.ReplaceAll(tt.script, "\n", "\n          ") + "\n"
			for _, target := range []ASTTargets{AWAST, ActionsAST} {
				diagnostics := integrityTestRun(t, PrivilegedCheckoutRule(), ruleContractInput(t, source, target, false))
				require.Len(t, diagnostics, tt.count)
				for _, diagnostic := range diagnostics {
					assert.Equal(t, "jobs.check.steps[0].run", diagnostic.Path)
					assert.Equal(t, 1, diagnostic.Line)
					assert.NotContains(t, diagnostic.Message, "PRIVATE_VALUE")
				}
			}
		})
	}
}

func TestPrivilegedCheckoutTriggersAndPins(t *testing.T) {
	pins := []string{
		"v6.0.2",
		"de0fac2e4500dabe0009e67214ff5f5447ce83dd",
		"3d3c42e5aac5ba805825da76410c181273ba90b1",
		"0123456789abcdef0123456789abcdef01234567",
	}
	for _, trigger := range []string{"pull_request_target", "workflow_run", "issue_comment", "repository_dispatch", "pull_request_review", "pull_request_review_comment", "push", "pull_request", "workflow_dispatch"} {
		for _, pin := range pins {
			for _, unsafe := range []bool{false, true} {
				source := fmt.Sprintf("on: %s\n%s    steps:\n      - uses: actions/checkout@%s\n        with: {allow-unsafe-pr-checkout: %t}\n", trigger, contractJob, pin, unsafe)
				diagnostics := integrityTestRun(t, PrivilegedCheckoutRule(), ruleContractInput(t, source, AWAST, false))
				want := 0
				if trigger != "push" && trigger != "pull_request" && trigger != "workflow_dispatch" && unsafe {
					want = 1
				}
				require.Len(t, diagnostics, want)
				for _, diagnostic := range diagnostics {
					assert.NotContains(t, diagnostic.Message, "vulnerable", "pins do not establish version vulnerability")
				}
			}
		}
	}
}

func TestPrivilegedCodeTraversalAndFailures(t *testing.T) {
	for _, rule := range []Rule{PrivilegedCheckoutRule(), ArtifactExecutionRule()} {
		runner, err := New(rule)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = runner.Run(ctx, Input{Document: &aw.Document{}})
		require.ErrorIs(t, err, context.Canceled)
		document, err := aw.Parse("---\non: workflow_run\nsteps:\n  - uses: actions/checkout@v7\n    with: [PRIVATE_VALUE]\n---\n")
		require.NoError(t, err)
		if rule.ID == "privileged-checkout" {
			_, err = runner.Run(context.Background(), Input{Document: document})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
		}
	}
	for _, scope := range []string{"steps", "post-steps", "safe-outputs.steps", "safe-outputs.post-steps", "safe-jobs.publish.steps", "safe-outputs.jobs.publish.steps"} {
		var fields string
		switch scope {
		case "steps", "post-steps":
			fields = scope + ": [{uses: actions/checkout@v7, with: {allow-unsafe-pr-checkout: true}}]\n"
		case "safe-outputs.steps", "safe-outputs.post-steps":
			key := "steps"
			if scope == "safe-outputs.post-steps" {
				key = "post-steps"
			}
			fields = "safe-outputs: {" + key + ": [{uses: actions/checkout@v7, with: {allow-unsafe-pr-checkout: true}}]}\n"
		case "safe-jobs.publish.steps":
			fields = "safe-jobs: {publish: {steps: [{uses: actions/checkout@v7, with: {allow-unsafe-pr-checkout: true}}]}}\n"
		default:
			fields = "safe-outputs: {jobs: {publish: {steps: [{uses: actions/checkout@v7, with: {allow-unsafe-pr-checkout: true}}]}}}\n"
		}
		input := ruleContractInput(t, "on: [push, pull_request_target]\n"+fields, AWAST, true)
		diagnostics := integrityTestRun(t, PrivilegedCheckoutRule(), input)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, scope+"[0].with.allow-unsafe-pr-checkout", diagnostics[0].Path)
		input.Workflow = &actions.Workflow{On: "push"}
		assert.Empty(t, integrityTestRun(t, PrivilegedCheckoutRule(), input), "compiled graph takes precedence")
	}
}
