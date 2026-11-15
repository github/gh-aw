//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteGateReviewConfigAndPermissions(t *testing.T) {
	compiler := NewCompiler()
	config := compiler.extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{
		"gate": map[string]any{"review-repo": "${{ inputs.review_repo }}", "allowed-repos": []any{"reviews/*"}, "approval-environment": "reviewed-writes"},
	}})
	require.NotNil(t, config.Gate)
	require.Equal(t, "${{ inputs.review_repo }}", config.Gate.ReviewRepo)
	require.Equal(t, []string{"reviews/*"}, config.Gate.AllowedRepos)
	require.Equal(t, "reviewed-writes", config.Gate.ApprovalEnvironment)
	permission, exists := ComputePermissionsForSafeOutputs(config).Get(PermissionIssues)
	require.True(t, exists)
	require.Equal(t, PermissionWrite, permission)
	staged := TemplatableBool("true")
	config.Staged = &staged
	permission, _ = ComputePermissionsForSafeOutputs(config).Get(PermissionIssues)
	require.Equal(t, PermissionRead, permission)
	env := compiler.buildJobLevelSafeOutputEnvVars(&WorkflowData{SafeOutputs: config}, "test")
	require.Equal(t, `"reviewed-writes"`, env["GH_AW_WRITE_GATE_APPROVAL_ENVIRONMENT"])
	require.Equal(t, `"reviews/*"`, env["GH_AW_WRITE_GATE_ALLOWED_REPOS"])
}

func TestWriteGateNormalizesBeforeReviewCredentialsAndWriters(t *testing.T) {
	compiler := NewCompiler()
	data := writeGateTestData()
	data.SafeOutputs.Gate = &SafeOutputsGateConfig{ReviewRepo: "reviews/project", AllowedRepos: []string{"reviews/project"}}
	data.SafeOutputs.GitHubApp = &GitHubAppConfig{
		AppID: "${{ secrets.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}",
		Repositories: []string{"*"}, Permissions: map[string]string{"contents": "write"},
	}
	job := &Job{
		Name: "safe_outputs",
		Steps: []string{`      - name: Validate gate
        id: original_gate
        uses: actions/github-script@1111111111111111111111111111111111111111
      - name: Existing writer
        id: process_safe_outputs
        if: ${{ steps.original_gate.outputs.decision == 'allow' }}
        run: echo write
`},
		Outputs: map[string]string{"write_gate": "${{ steps.original_gate.outputs.decision }}", "write_gate_reason": "${{ steps.original_gate.outputs.reason }}"},
	}
	require.NoError(t, compiler.jobManager.AddJob(job))
	require.NoError(t, compiler.addWriteGateItemDecisionSteps(data))
	text := strings.Join(job.Steps, "")
	require.Less(t, strings.Index(text, "Normalize write item decisions"), strings.Index(text, "Generate gate review GitHub App token"))
	require.Less(t, strings.Index(text, "Generate gate review GitHub App token"), strings.Index(text, "Existing writer"))
	require.Contains(t, text, "GH_AW_VALIDATED_WRITE_GATE: ${{ steps.original_gate.outputs.decision }}")
	require.Contains(t, text, "repositories: ${{ env.GH_AW_WRITE_GATE_REVIEW_REPO }}")
	require.Contains(t, text, "permission-issues: write")
	require.NotContains(t, text, "permission-contents:")
	require.Contains(t, text, "if: ${{ steps.normalize_write_decisions.outputs.decision == 'allow' }}")
	require.Contains(t, text, "write-decisions.normalized.json")
	require.Equal(t, "${{ steps.normalize_write_decisions.outcome == 'success' && steps.normalize_write_decisions.outputs.decision || 'deny' }}", job.Outputs["write_gate"])
}

func TestWriteGateConfigRequiresARealGateAndLeavesUngatedJobsUnchanged(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{}}
	require.NoError(t, compiler.addWriteGateItemDecisionSteps(data))
	data.SafeOutputs.Gate = &SafeOutputsGateConfig{ApprovalEnvironment: "reviewed"}
	err := compiler.addWriteGateItemDecisionSteps(data)
	require.ErrorContains(t, err, "gate-steps")
}

func TestMaintenanceHeldReplayUsesOriginalEnvironmentAndPreparedPins(t *testing.T) {
	opts := buildMaintenanceWorkflowYAMLOptions{hasWriteGate: true, runsOnValue: "ubuntu-latest"}
	text := buildMaintenanceHeldReplayJobs(opts, "github/gh-aw/actions/setup@1111111111111111111111111111111111111111")
	require.Contains(t, text, "prepare_safe_output_replay:")
	require.Contains(t, text, "environment: ${{ needs.prepare_safe_output_replay.outputs.approval_environment }}")
	require.Contains(t, text, "GH_AW_REPLAY_SOURCE_WORKFLOW_SHA:")
	require.Contains(t, text, "GH_AW_REPLAY_SAFE_OUTPUT_ITEMS_SHA256:")
	prepare := text[:strings.Index(text, "\n  apply_held_safe_outputs:")]
	require.NotContains(t, prepare, "issues: write")
	require.NotContains(t, prepare, "contents: write")
	require.Contains(t, text, "restorePendingInputs")
}
