package lint

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func additionalPolicyRules() []Rule {
	return []Rule{InstallScriptsRule(), AllowedLabelsRule(), NetworkScopeRule(), PushTargetRule()}
}

func TestAdditionalPolicySafeInputs(t *testing.T) {
	for _, source := range []string{
		"", "network: defaults", "network:\n  allowed: []",
		"network:\n  allowed: [defaults, node, '*.example.com']",
		"network:\n  blocked: ['*']",
		"runtimes:\n  node:\n    version: 22\n    run-install-scripts: false",
		"runtimes:\n  python:\n    run-install-scripts: true",
		"run-install-scripts: true",
		"safe-outputs:\n  create-issue:\n    allowed-labels: [bug, 'team-*']",
		"safe-outputs:\n  create-discussion:\n    allowed-labels: []",
		"safe-outputs:\n  create-pull-request:",
		"safe-outputs:\n  merge-pull-request:\n    required-labels: ['*']",
		"safe-outputs:\n  merge-pull-request:\n    allowed-labels: ['*']",
		"safe-outputs:\n  push-to-pull-request-branch:\n    target: 123",
	} {
		for _, rule := range additionalPolicyRules() {
			input := policyTestInput(t, source)
			before, err := json.Marshal(input)
			require.NoError(t, err)
			diagnostics, err := rule.Check(context.Background(), input)
			require.NoError(t, err, "%s: %s", rule.ID, source)
			assert.Empty(t, diagnostics, "%s: %s", rule.ID, source)
			after, err := json.Marshal(input)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			input.Document = nil
			diagnostics, err = rule.Check(context.Background(), input)
			require.NoError(t, err)
			assert.Empty(t, diagnostics, "native Actions do not establish authored AW policy")
		}
	}
}

func TestAllowedLabelsRule(t *testing.T) {
	for _, handler := range []string{"create-issue", "create-discussion", "update-discussion", "create-pull-request"} {
		for _, strict := range []string{"true", "false"} {
			input := policyTestInput(t, "strict: "+strict+"\nsafe-outputs:\n  "+handler+":\n    allowed-labels: [bug, ' * ', 'team-*']")
			runner, err := New(AllowedLabelsRule())
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			require.Len(t, diagnostics, 1)
			assert.Equal(t, Error, diagnostics[0].Severity)
			assert.Equal(t, "safe-outputs."+handler+".allowed-labels", diagnostics[0].Path)
		}
	}
}

func TestAllowedLabelsRuleSkipsBooleanHandlers(t *testing.T) {
	// smoke-claude-sub-agents disables issue creation with `create-issue: false`; a boolean
	// handler has no allowed-labels to check and must not be reported as malformed.
	for _, handler := range []string{"create-issue", "create-discussion", "update-discussion", "create-pull-request"} {
		other := "create-discussion"
		if handler == other {
			other = "create-pull-request"
		}
		for _, value := range []string{"true", "false"} {
			input := policyTestInput(t, "safe-outputs:\n  "+handler+": "+value+"\n  "+other+":\n    allowed-labels: ['*']")
			runner, err := New(AllowedLabelsRule())
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err, handler+": "+value)
			require.Len(t, diagnostics, 1, "other handlers are still checked")
			assert.Equal(t, "safe-outputs."+other+".allowed-labels", diagnostics[0].Path)
		}
	}
}

func TestPushTargetRule(t *testing.T) {
	for _, tt := range []struct {
		fields string
		want   int
	}{
		{"", 1},
		{"required-title-prefix: '[bot] '", 0},
		{"required-author: workflow-bot", 0},
		{"title-prefix: '[legacy] '", 0},
		{"required-title-prefix: ''\n    title-prefix: '[legacy] '", 0},
		{"required-labels: [automated]", 0},
		{"labels: [automated]", 0},
		{"required-labels: []\n    labels: [automated]", 0},
		{"required-labels: ['*']", 0},
		{"required-labels: '${{ inputs.labels }}'", 0},
		{"required-title-prefix: ''\n    required-labels: []", 1},
	} {
		for _, strict := range []string{"true", "false"} {
			input := policyTestInput(t, "strict: "+strict+"\nsafe-outputs:\n  push-to-pull-request-branch:\n    target: '*'\n    "+tt.fields)
			runner, err := New(PushTargetRule())
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), input)
			require.NoError(t, err)
			require.Len(t, diagnostics, tt.want)
			for _, diagnostic := range diagnostics {
				assert.Equal(t, Warning, diagnostic.Severity, "CTR-012 remains a warning even in strict mode")
			}
			input.Document.Frontmatter["imports"] = []any{map[string]any{"path": "shared/constraints.md"}}
			diagnostics, err = runner.Run(context.Background(), input)
			require.NoError(t, err)
			assert.Empty(t, diagnostics, "imports may supply constraints; do not guess merged policy")
		}
	}
}

func TestAdditionalPolicyMalformedInputs(t *testing.T) {
	for _, tt := range []struct {
		rule Rule
		text string
	}{
		{InstallScriptsRule(), "runtimes: private-value"},
		{InstallScriptsRule(), "runtimes:\n  node: private-value"},
		{InstallScriptsRule(), "runtimes:\n  node:\n    run-install-scripts: private-value"},
		{NetworkScopeRule(), "network: private-value"},
		{NetworkScopeRule(), "network:\n  allowed: private-value"},
		{AllowedLabelsRule(), "safe-outputs: private-value"},
		{AllowedLabelsRule(), "safe-outputs:\n  create-issue: private-value"},
		{AllowedLabelsRule(), "safe-outputs:\n  create-issue:\n    allowed-labels: private-value"},
		{PushTargetRule(), "safe-outputs:\n  push-to-pull-request-branch: private-value"},
		{PushTargetRule(), "safe-outputs:\n  push-to-pull-request-branch:\n    target: '*'\n    required-labels: private-value"},
		{PushTargetRule(), "imports: private-value\nsafe-outputs:\n  push-to-pull-request-branch:\n    target: '*'"},
	} {
		_, err := tt.rule.Check(context.Background(), policyTestInput(t, tt.text))
		require.Error(t, err, tt.rule.ID)
		assert.NotContains(t, err.Error(), "private-value")
	}
	for _, rule := range additionalPolicyRules() {
		for _, input := range []Input{{}, {Workflow: &actions.Workflow{Jobs: []*actions.Job{nil}}}} {
			_, err := rule.Check(context.Background(), input)
			require.Error(t, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := rule.Check(ctx, Input{Workflow: &actions.Workflow{}})
		require.ErrorIs(t, err, context.Canceled)
	}
}

func TestAdditionalPolicyPartialFindingsAndCancellation(t *testing.T) {
	for _, tt := range []struct {
		rule Rule
		text string
	}{
		{NetworkScopeRule(), "network:\n  allowed: [42, '*']"},
		{AllowedLabelsRule(), "safe-outputs:\n  create-issue: 42\n  create-discussion:\n    allowed-labels: ['*', 42]"},
	} {
		input := policyTestInput(t, tt.text)
		runner, err := New(tt.rule)
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), input)
		require.Error(t, err)
		require.Len(t, diagnostics, 1, "valid independent findings survive malformed declarations")
	}
	input := policyTestInput(t, "network:\n  allowed: ["+strings.Repeat("example.com, ", 20)+"'last.example.com']")
	ctx := &integrityCancelContext{Context: context.Background(), remaining: 4}
	_, err := NetworkScopeRule().Check(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
	input = policyTestInput(t, "safe-outputs:\n  create-issue:\n    allowed-labels: ["+strings.Repeat("bug, ", 20)+"enhancement]")
	ctx = &integrityCancelContext{Context: context.Background(), remaining: 4}
	_, err = AllowedLabelsRule().Check(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
}
