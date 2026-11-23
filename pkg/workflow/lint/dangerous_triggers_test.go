package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDangerousTriggersIntegrityFiltering(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, source string
		paths        []string
	}{
		{"scalar", "on: issue_comment\n", []string{"on"}},
		{"list", "on: [push, issue_comment]\n", []string{"on"}},
		{"mapping", "on: {issue_comment: {types: [created]}}\n", []string{"on.issue_comment"}},
		{"review comment", "on: {pull_request_review_comment: {types: [created]}}\n", []string{"on.pull_request_review_comment"}},
		{"review", "on: pull_request_review\n", []string{"on"}},
		{"mixed reviews", "on: {pull_request_review: null, pull_request_review_comment: null}\n", []string{"on.pull_request_review", "on.pull_request_review_comment"}},
		{"filtered review comment", "on: pull_request_review_comment\ntools: {github: {min-integrity: approved}}\n", nil},
		{"filtered review", "on: pull_request_review\ntools: {github: {min-integrity: merged}}\n", nil},
		{"unapproved review comment", "on: pull_request_review_comment\ntools: {github: {min-integrity: unapproved}}\n", []string{"on"}},
		{"disabled review comment", "on: pull_request_review_comment\ntools: {github: false}\n", nil},
		{"command", "on: {command: review}\n", []string{"on.command"}},
		{"slash command", "on: {slash_command: review}\n", []string{"on.slash_command"}},
		{"none", "on: issue_comment\ntools: {github: {min-integrity: none}}\n", []string{"on"}},
		{"unapproved", "on: issue_comment\ntools: {github: {min-integrity: unapproved}}\n", []string{"on"}},
		{"approved", "on: issue_comment\ntools: {github: {min-integrity: approved}}\n", nil},
		{"merged", "on: issue_comment\ntools: {github: {min-integrity: merged}}\n", nil},
		{"gateway filtering without proxy", "on: issue_comment\ntools: {github: {min-integrity: approved, integrity-proxy: false}}\n", nil},
		{"disabled", "on: issue_comment\ntools: {github: false}\n", nil},
		{"boolean enabled", "on: issue_comment\ntools: {github: true}\n", []string{"on"}},
		{"null enabled", "on: issue_comment\ntools: {github: null}\n", []string{"on"}},
		{"lockdown overrides", "on: issue_comment\ntools: {github: {min-integrity: approved, lockdown: true}}\n", []string{"on"}},
		{"lockdown false", "on: issue_comment\ntools: {github: {min-integrity: approved, lockdown: false}}\n", nil},
		{"roles and checkout not input filtering", "on: {issue_comment: null, roles: [write]}\ncheckout: false\n", []string{"on.issue_comment"}},
		{"unresolved imports not proof", "on: issue_comment\nimports: [shared.md]\n", []string{"on"}},
		{"safe triggers", "on: [push, pull_request, release, schedule, repository_dispatch, workflow_dispatch]\n", nil},
		{"exact names", "on: {issue_comment_staging: null}\n", nil},
		{"target not exempted by filtering", "on: pull_request_target\ntools: {github: {min-integrity: approved}}\n", []string{"on"}},
		{"target acknowledged", "on: {pull_request_target: {acknowledge-risk: true}}\n", nil},
		{"target false acknowledgment", "on: {pull_request_target: {acknowledge-risk: false}}\n", []string{"on.pull_request_target"}},
		{"target acknowledgment does not exempt comments", "on: {pull_request_target: {acknowledge-risk: true}, issue_comment: null}\n", []string{"on.issue_comment"}},
		{"field suppression", "# gh-aw: ignore[dangerous-triggers] -- Reviewed untrusted triage inputs.\non: issue_comment\n", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, nonstrict := range []bool{false, true} {
				input := ruleContractInput(t, tt.source, AWAST, nonstrict)
				runner, err := Default().Select("dangerous-triggers")
				require.NoError(t, err)
				diagnostics, err := runner.Run(context.Background(), input)
				require.NoError(t, err)
				var paths []string
				for _, diagnostic := range diagnostics {
					paths = append(paths, diagnostic.Path)
					assert.Equal(t, Warning, diagnostic.Severity)
					assert.Contains(t, []string{"CTR-008", "CTR-039"}, diagnostic.ThreatRuleID)
				}
				assert.Equal(t, tt.paths, paths)
			}
		})
	}
}

func TestDangerousTriggersActionsAndResolvedPolicy(t *testing.T) {
	t.Parallel()
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		input := ruleContractInput(t, "on: {issue_comment: null}\n"+contractJob+contractSafeSteps, target, false)
		input.ResolvedTools = map[string]any{"github": map[string]any{"min-integrity": "approved"}}
		runner, err := Default().Select("dangerous-triggers")
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), input)
		require.NoError(t, err)
		assert.Empty(t, diagnostics, "resolved imported filtering is used")
		input.ResolvedTools = nil
		diagnostics, err = runner.Run(context.Background(), input)
		require.NoError(t, err)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, "CTR-039", diagnostics[0].ThreatRuleID)
	}
	input := ruleContractInput(t, "on: {pull_request_target: {acknowledge-risk: true}}\n", AWAST, false)
	graph, err := actions.Parse([]byte("on: [pull_request_target, issue_comment]\n" + contractJob + contractSafeSteps))
	require.NoError(t, err)
	input.Workflow = graph
	runner, err := Default().Select("dangerous-triggers")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1, "AW acknowledgment applies only to the target event in the compiled graph")
	assert.Equal(t, "CTR-039", diagnostics[0].ThreatRuleID)
	input.Document = nil
	diagnostics, err = runner.Run(context.Background(), input)
	require.NoError(t, err)
	require.Len(t, diagnostics, 2, "standalone Actions does not inherit an AW acknowledgment")
}

func TestDangerousTriggersMalformedPolicyAndCancellation(t *testing.T) {
	t.Parallel()
	runner, err := Default().Select("dangerous-triggers")
	require.NoError(t, err)
	for _, policy := range []string{
		"tools: PRIVATE_VALUE",
		"tools: {github: PRIVATE_VALUE}",
		"tools: {github: {min-integrity: PRIVATE_VALUE}}",
		"tools: {github: {min-integrity: [PRIVATE_VALUE]}}",
		"tools: {github: {lockdown: PRIVATE_VALUE}}",
	} {
		input := ruleContractInput(t, "on: issue_comment\n"+policy+"\n", AWAST, false)
		_, err := runner.Run(context.Background(), input)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
	}
	input := ruleContractInput(t, "on: {pull_request_target: {acknowledge-risk: PRIVATE_VALUE}}\n", AWAST, false)
	_, err = runner.Run(context.Background(), input)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
}
