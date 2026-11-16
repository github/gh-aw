package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUntrustedRolesDetectionPolicy(t *testing.T) {
	const disabled = "safe-outputs:\n  add-labels: {}\n  threat-detection: false\n"
	for _, tt := range []struct {
		name, source string
		want         bool
	}{
		{"scalar all with issue trigger", "on: {issues: null, roles: all}\n" + disabled, true},
		{"list containing all", "on: {issue_comment: null, roles: [write, all]}\n" + disabled, true},
		{"slash command", "on: {slash_command: bot, roles: all}\n" + disabled, true},
		{"unknown trigger fails closed", "on: {future_event: null, roles: all}\n" + disabled, true},
		{"enabled false form", "on: {issues: null, roles: all}\nsafe-outputs: {threat-detection: {enabled: false}}\n", true},
		{"engine false without steps", "on: {issues: null, roles: all}\nsafe-outputs: {threat-detection: {engine: false}}\n", true},
		{"trusted-only triggers", "on: {schedule: [{cron: '0 0 * * *'}], workflow_dispatch: null, push: null, roles: all, skip-roles: [admin]}\n" + disabled, false},
		{"trusted roles", "on: {issues: null, roles: [write]}\n" + disabled, false},
		{"roles omitted", "on: {issues: null}\n" + disabled, false},
		{"detection enabled", "on: {issues: null, roles: all}\nsafe-outputs: {threat-detection: true}\n", false},
		{"detection default", "on: {issues: null, roles: all}\nsafe-outputs: {add-labels: {}}\n", false},
		{"detection expression", "on: {issues: null, roles: all}\nsafe-outputs: {threat-detection: {enabled: '${{ inputs.detect }}'}}\n", false},
		{"engine false with steps", "on: {issues: null, roles: all}\nsafe-outputs: {threat-detection: {engine: false, steps: [{run: scan}]}}\n", false},
		{"no safe outputs", "on: {issues: null, roles: all}\n", false},
		{"nvx does not exempt", "on: {issues: null, roles: all}\nsandbox: {agent: {id: awf, runtime: nvx}}\n" + disabled, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, strict := range []string{"", "strict: false\n"} {
				diagnostics := runUntrustedRolesDetection(t, policyTestInput(t, strict+tt.source))
				if !tt.want {
					assert.Empty(t, diagnostics)
					continue
				}
				require.Len(t, diagnostics, 1)
				severity := Error
				if strict != "" {
					severity = Warning
				}
				assert.Equal(t, severity, diagnostics[0].Severity)
				assert.Equal(t, "on.roles", diagnostics[0].Path)
				assert.Equal(t, "CTR-005", diagnostics[0].ThreatRuleID)
			}
		})
	}
}

func TestUntrustedRolesDetectionRecommendsNVX(t *testing.T) {
	const source = "on: {issues: null, roles: all}\nsafe-outputs: {threat-detection: false}\n"
	diagnostics := runUntrustedRolesDetection(t, policyTestInput(t, source))
	require.Len(t, diagnostics, 1)
	assert.Contains(t, diagnostics[0].Message, "sandbox.agent.runtime: nvx")

	diagnostics = runUntrustedRolesDetection(t, policyTestInput(t, source+"sandbox: {agent: {runtime: nvx}}\n"))
	require.Len(t, diagnostics, 1)
	assert.NotContains(t, diagnostics[0].Message, "sandbox.agent.runtime: nvx")
}

func TestUntrustedRolesDetectionCompiledGraph(t *testing.T) {
	jobs := func(ids ...string) *actions.Workflow {
		workflow := &actions.Workflow{}
		for _, id := range ids {
			workflow.Jobs = append(workflow.Jobs, &actions.Job{ID: actions.JobID(id)})
		}
		return workflow
	}
	const roles = "on: {issues: null, roles: all}\n"
	imported := policyTestInput(t, roles+"imports: [shared/outputs.md]\n")
	imported.Workflow = jobs("agent", "safe_outputs")
	assert.Len(t, runUntrustedRolesDetection(t, imported), 1, "imported detection disablement is visible in the compiled graph")

	enabled := policyTestInput(t, roles+"safe-outputs: {threat-detection: false}\n")
	enabled.Workflow = jobs("agent", "detection", "safe_outputs")
	assert.Empty(t, runUntrustedRolesDetection(t, enabled), "a compiled detection job takes precedence")
}

func runUntrustedRolesDetection(t *testing.T, input Input) []Diagnostic {
	t.Helper()
	runner, err := Default().Select("untrusted-roles-detection")
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), input)
	require.NoError(t, err)
	return diagnostics
}
