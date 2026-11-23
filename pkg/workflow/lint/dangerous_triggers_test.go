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
		{"scalar trusted default", "on: issue_comment\ntools: {github: {min-integrity: none}}\n", nil},
		{"list trusted default", "on: [push, issue_comment]\ntools: {github: {min-integrity: none}}\n", nil},
		{"mapping", "on: {issue_comment: {types: [created]}, roles: all}\ntools: {github: {min-integrity: none}}\n", []string{"on.issue_comment"}},
		{"review comment", "on: {pull_request_review_comment: {types: [created]}, roles: read}\ntools: {github: {min-integrity: none}}\n", []string{"on.pull_request_review_comment"}},
		{"mixed reviews", "on: {pull_request_review: null, pull_request_review_comment: null, roles: all}\ntools: {github: {min-integrity: none}}\n", []string{"on.pull_request_review", "on.pull_request_review_comment"}},
		{"filtered review comment", "on: {pull_request_review_comment: null, roles: all}\ntools: {github: {min-integrity: approved}}\n", nil},
		{"filtered review", "on: {pull_request_review: null, roles: all}\ntools: {github: {min-integrity: merged}}\n", nil},
		{"unapproved review comment", "on: {pull_request_review_comment: null, roles: [write, read]}\ntools: {github: {min-integrity: unapproved}}\n", []string{"on.pull_request_review_comment"}},
		{"disabled review comment", "on: {pull_request_review_comment: null, roles: all}\ntools: {github: false}\n", nil},
		{"command", "on: {command: review, roles: all}\ntools: {github: {min-integrity: none}}\n", []string{"on.command"}},
		{"slash command default roles", "on: {slash_command: review}\ntools: {github: {min-integrity: none}}\n", nil},
		{"slash command trusted roles", "on: {slash_command: review, roles: [admin, maintainer, maintain, write]}\ntools: {github: {min-integrity: none}}\n", nil},
		{"slash command triage", "on: {slash_command: review, roles: [write, triage]}\ntools: {github: {min-integrity: none}}\n", []string{"on.slash_command"}},
		{"none", "on: {issue_comment: null, roles: all}\ntools: {github: {min-integrity: none}}\n", []string{"on.issue_comment"}},
		{"unapproved", "on: {issue_comment: null, roles: read}\ntools: {github: {min-integrity: unapproved}}\n", []string{"on.issue_comment"}},
		{"approved", "on: {issue_comment: null, roles: all}\ntools: {github: {min-integrity: approved}}\n", nil},
		{"merged", "on: {issue_comment: null, roles: all}\ntools: {github: {min-integrity: merged}}\n", nil},
		{"gateway filtering without proxy", "on: {issue_comment: null, roles: all}\ntools: {github: {min-integrity: approved, integrity-proxy: false}}\n", nil},
		{"disabled", "on: {issue_comment: null, roles: all}\ntools: {github: false}\n", nil},
		{"boolean enabled unknown threshold", "on: {issue_comment: null, roles: all}\ntools: {github: true}\n", nil},
		{"null enabled unknown threshold", "on: {issue_comment: null, roles: all}\ntools: {github: null}\n", nil},
		{"lockdown overrides", "on: {issue_comment: null, roles: all}\ntools: {github: {min-integrity: approved, lockdown: true}}\n", []string{"on.issue_comment"}},
		{"lockdown trusted roles", "on: issue_comment\ntools: {github: {min-integrity: approved, lockdown: true}}\n", nil},
		{"lockdown false", "on: {issue_comment: null, roles: all}\ntools: {github: {min-integrity: approved, lockdown: false}}\n", nil},
		{"roles gate weak filtering", "on: {issue_comment: null, roles: [write]}\ntools: {github: {min-integrity: none}}\n", nil},
		{"checkout not actor authorization", "on: {issue_comment: null, roles: all}\ncheckout: false\ntools: {github: {min-integrity: none}}\n", []string{"on.issue_comment"}},
		{"unresolved imports unknown filtering", "on: {issue_comment: null, roles: all}\nimports: [shared.md]\n", nil},
		{"absent threshold unknown filtering", "on: {issue_comment: null, roles: all}\ntools: {github: {toolsets: [default]}}\n", nil},
		{"safe triggers", "on: [push, pull_request, release, schedule, repository_dispatch, workflow_dispatch]\n", nil},
		{"exact names", "on: {issue_comment_staging: null}\n", nil},
		{"target not exempted by filtering", "on: pull_request_target\ntools: {github: {min-integrity: approved}}\n", []string{"on"}},
		{"target acknowledged", "on: {pull_request_target: {acknowledge-risk: true}}\n", nil},
		{"target false acknowledgment", "on: {pull_request_target: {acknowledge-risk: false}}\n", []string{"on.pull_request_target"}},
		{"target acknowledgment does not exempt comments", "on: {pull_request_target: {acknowledge-risk: true}, issue_comment: null, roles: all}\ntools: {github: {min-integrity: none}}\n", []string{"on.issue_comment"}},
		{"field suppression", "# gh-aw: ignore[dangerous-triggers] -- Reviewed untrusted triage inputs.\non: {issue_comment: null, roles: all}\ntools: {github: {min-integrity: none}}\n", nil},
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
		input.ResolvedRoles = []string{"all"}
		runner, err := Default().Select("dangerous-triggers")
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), input)
		require.NoError(t, err)
		assert.Empty(t, diagnostics, "resolved imported filtering is used")
		input.ResolvedTools["github"] = map[string]any{"min-integrity": "none"}
		diagnostics, err = runner.Run(context.Background(), input)
		require.NoError(t, err)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, "CTR-039", diagnostics[0].ThreatRuleID)
		input.ResolvedRoles = []string{"write"}
		diagnostics, err = runner.Run(context.Background(), input)
		require.NoError(t, err)
		assert.Empty(t, diagnostics, "compiler-resolved trusted admission is honored")
		input.ResolvedTools = nil
		input.ResolvedRoles = []string{"all"}
		diagnostics, err = runner.Run(context.Background(), input)
		require.NoError(t, err)
		assert.Empty(t, diagnostics, "unknown filtering is not weak filtering")
	}
	input := ruleContractInput(t, "on: {pull_request_target: {acknowledge-risk: true}, roles: all}\ntools: {github: {min-integrity: none}}\n", AWAST, false)
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
	require.Len(t, diagnostics, 1, "standalone Actions has no AW acknowledgment or tool policy")
	assert.Equal(t, "CTR-008", diagnostics[0].ThreatRuleID)
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
	for _, roles := range []string{"PRIVATE_VALUE", "[read, PRIVATE_VALUE]", "[all, 42]", "{invalid: PRIVATE_VALUE}"} {
		input := ruleContractInput(t, "on: {issue_comment: null, roles: "+roles+"}\ntools: {github: {min-integrity: none}}\n", AWAST, false)
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
