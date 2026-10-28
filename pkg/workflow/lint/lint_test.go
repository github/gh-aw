package lint

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuleRegistrationAndSelection(t *testing.T) {
	runner := Default()
	_, err := New(runner.Rules()...)
	require.NoError(t, err, "built-in rule registrations must satisfy the same contract as extensions")
	var ruleIDs []string
	for _, rule := range runner.Rules() {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	assert.ElementsMatch(t, []string{
		"secret-leak", "otlp-secret-leak", "action-pin", "job-timeout", "needs-consistency", "step-reference", "template-injection", "workflow-run-scope",
		"permission-scope", "version-check", "conditional-import", "private-to-public",
		"install-scripts", "allowed-labels", "network-scope", "push-target",
		"tool-scope", "sandbox-config", "image-argument", "checkout-mode", "fork-association",
		"pull-request-target-roles", "safe-output-shell-expansion", "firewall-ssl-bump",
		"custom-job-detection",
		"grader-file-io",
		"grader-shell-file-write",
		"pull-request-target-checkout", "prompt-expression",
		"gateway-dispatch-literals",
		"artipacked", "cache-poisoning", "excessive-permissions", "obfuscation",
		"bash-case-pattern",
		"privileged-checkout", "artifact-execution",
	}, ruleIDs)
	selected, err := runner.Select("secret-leak", "secret-leak")
	require.NoError(t, err)
	assert.Len(t, selected.Rules(), 1)
	_, err = runner.Select("missing")
	require.ErrorContains(t, err, "unknown workflow lint rule")
	_, err = New(SecretLeakRule(), SecretLeakRule())
	require.ErrorContains(t, err, "duplicate")
	for _, rule := range []Rule{{}, {ID: "Bad ID"}, {ID: "valid", Description: "test", Check: func(context.Context, Input) ([]Diagnostic, error) { return nil, nil }}} {
		_, err := New(rule)
		require.Error(t, err)
	}
	rules := runner.Rules()
	rules[0].ID = "changed"
	assert.Equal(t, "secret-leak", runner.Rules()[0].ID)
}

func TestIndependentRulesAndFailures(t *testing.T) {
	first := Rule{
		ID: "structural", Description: "An independent non-taint rule", Severity: Warning,
		Check: func(_ context.Context, input Input) ([]Diagnostic, error) {
			return []Diagnostic{{Path: "name", Message: input.Workflow.Name}}, errors.New("check incomplete")
		},
	}
	second := Rule{
		ID: "another", Description: "Continue after an earlier rule failure", Severity: Error,
		Check: func(context.Context, Input) ([]Diagnostic, error) {
			return []Diagnostic{{Path: "env", Message: "found"}}, nil
		},
	}
	runner, err := New(first, second)
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Workflow: &actions.Workflow{Name: "example"}})
	require.ErrorContains(t, err, "lint rule structural: check incomplete")
	require.Len(t, diagnostics, 2)
	assert.Equal(t, "another", diagnostics[0].RuleID)
	assert.Equal(t, Error, diagnostics[0].Severity)
	assert.Equal(t, Warning, diagnostics[1].Severity)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, Input{Workflow: &actions.Workflow{}})
	require.ErrorIs(t, err, context.Canceled)
	_, err = runner.Run(context.Background(), Input{})
	require.Error(t, err)
}

func TestRuleThreatReference(t *testing.T) {
	for _, tt := range []struct {
		name, threatID string
		invalid        bool
	}{
		{name: "known mapping", threatID: "CTR-017"},
		{name: "unmapped extension"},
		{name: "wrong prefix", threatID: "ctr-017", invalid: true},
		{name: "wrong width", threatID: "CTR-17", invalid: true},
		{name: "invalid characters", threatID: "CTR-ABC", invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner, err := New(Rule{
				ID: "extension", Description: "An optional compiler threat reference",
				Severity: Warning, ThreatRuleID: tt.threatID,
				Check: func(context.Context, Input) ([]Diagnostic, error) {
					return []Diagnostic{{Path: "name", Message: "finding", ThreatRuleID: "CTR-999"}}, nil
				},
			})
			if tt.invalid {
				require.ErrorContains(t, err, "compiler threat ID in CTR-NNN form")
				return
			}
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Workflow: &actions.Workflow{}})
			require.NoError(t, err)
			require.Len(t, diagnostics, 1)
			assert.Equal(t, tt.threatID, diagnostics[0].ThreatRuleID, "the registry owns threat references")
			encoded, err := json.Marshal(diagnostics)
			require.NoError(t, err)
			if tt.threatID == "" {
				assert.NotContains(t, string(encoded), "threat_rule_id")
			} else {
				assert.Contains(t, string(encoded), `"threat_rule_id":"CTR-017"`)
			}
		})
	}
}

func TestSecretRuleDiagnostics(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ secrets.TOKEN }}'
      - shell: pwsh
        run: Write-Output test
`))
	require.NoError(t, err)
	diagnostics, err := Default().Run(context.Background(), Input{Workflow: workflow})
	require.NoError(t, err)
	require.Len(t, diagnostics, 2)
	assert.Equal(t, Error, diagnostics[0].Severity)
	assert.Equal(t, Warning, diagnostics[1].Severity)
	assert.NotContains(t, diagnostics[0].Message, "TOKEN")
	assert.Contains(t, diagnostics[1].Message, "Incomplete")
}

func TestSecretRuleCoverageWarningLocation(t *testing.T) {
	workflow, err := actions.Parse([]byte(`
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        run: |
          : "${TOKEN:?credential required}"
          test -n "$TOKEN"
          unknown "$TOKEN"
          unknown "$TOKEN"
`))
	require.NoError(t, err)
	runner, err := New(SecretLeakRule())
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	assert.Equal(t, Warning, diagnostics[0].Severity)
	assert.Equal(t, "jobs.test.steps[0].run", diagnostics[0].Path)
	assert.Equal(t, 3, diagnostics[0].Line)
	assert.Equal(t, "Incomplete secret-leak analysis: external command data flow is not modeled", diagnostics[0].Message)
	assert.Empty(t, diagnostics[0].Related)
}
