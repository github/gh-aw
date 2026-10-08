package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkQueueClaimAdapterVerifierIdentifiers(t *testing.T) {
	makeAdapter := func(id string) *WorkQueueClaimAdapter {
		return &WorkQueueClaimAdapter{
			Mode: "prepared", EffectType: "github_rest", TargetRepo: "owner/repo", VerifierID: id,
			FieldMap: map[string]string{"name": "name"},
			Request:  &WorkQueueRestRequest{Method: "POST", Route: "/repos/{owner}/{repo}/check-runs", Permission: "checks"},
			Verifier: &WorkQueueRestVerifier{Route: "/repos/{owner}/{repo}/check-runs/{receipt_id}", ResourceKind: "check_run", Fields: map[string]string{"name": "name"}},
		}
	}
	makeData := func(adapters map[string]*WorkQueueClaimAdapter) *WorkflowData {
		jobs := make(map[string]*SafeJobConfig, len(adapters))
		for name := range adapters {
			jobs[name] = &SafeJobConfig{Steps: []any{map[string]any{"run": "node prepare.cjs"}}}
		}
		return &WorkflowData{SafeOutputs: &SafeOutputsConfig{ClaimAdapters: adapters, Jobs: jobs}}
	}
	for _, id := range []string{"", "CheckCreated.v1", strings.Repeat("a", 128)} {
		data := makeData(map[string]*WorkQueueClaimAdapter{"custom": makeAdapter(id)})
		require.NoError(t, validateWorkQueueClaimAdapters(data))
	}
	for _, id := range []string{"_invalid", "${{ inputs.verifier }}", "read user", strings.Repeat("a", 129)} {
		data := makeData(map[string]*WorkQueueClaimAdapter{"custom": makeAdapter(id)})
		require.ErrorContains(t, validateWorkQueueClaimAdapters(data), "verifier-id")
	}
	for _, first := range []string{"", "shared.v1"} {
		id := first
		if id == "" {
			id = "custom"
		}
		data := makeData(map[string]*WorkQueueClaimAdapter{
			"custom": makeAdapter(first), "second": makeAdapter(id),
		})
		require.ErrorContains(t, validateWorkQueueClaimAdapters(data), "must be unique")
	}
	builtin := &WorkQueueClaimAdapter{
		Mode: "prepared", EffectType: "update_issue", TargetRepo: "owner/repo", VerifierID: "IssueUpdated.v1",
		FieldMap: map[string]string{"title": "title", "item_number": "number"},
	}
	require.NoError(t, validateWorkQueueClaimAdapters(makeData(map[string]*WorkQueueClaimAdapter{"custom": builtin})))
	require.ErrorContains(t, validateWorkQueueClaimAdapters(makeData(map[string]*WorkQueueClaimAdapter{
		"custom": builtin, "native": makeAdapter("IssueUpdated.v1"),
	})), "must be unique")
	for _, field := range []string{"state", "item_number", "claim_handle"} {
		invalid := &WorkQueueClaimAdapter{
			Mode: "prepared", EffectType: "create_issue", TargetRepo: "owner/repo",
			FieldMap: map[string]string{field: "content"},
		}
		require.ErrorContains(t, validateWorkQueueClaimAdapters(makeData(map[string]*WorkQueueClaimAdapter{"custom": invalid})), "undeclared effect field")
	}
	for _, id := range []string{"create_issue", "update_issue", "close_issue", "add_comment", "add_labels", "remove_labels", "replace_label"} {
		t.Run("native-verifier-"+id, func(t *testing.T) {
			data := makeData(map[string]*WorkQueueClaimAdapter{"custom": makeAdapter(id)})
			data.SafeOutputs.CreateIssues = &CreateIssuesConfig{}
			data.SafeOutputs.UpdateIssues = &UpdateIssuesConfig{}
			data.SafeOutputs.CloseIssues = &CloseIssuesConfig{}
			data.SafeOutputs.AddComments = &AddCommentsConfig{}
			data.SafeOutputs.AddLabels = &AddLabelsConfig{}
			data.SafeOutputs.RemoveLabels = &RemoveLabelsConfig{}
			data.SafeOutputs.ReplaceLabel = &ReplaceLabelConfig{}
			require.ErrorContains(t, validateWorkQueueClaimAdapters(data), "conflicts with the enabled native "+id+" verifier")
			data.SafeOutputs.ClaimAdapters[id] = makeAdapter("explicit-override.v1")
			data.SafeOutputs.Jobs[id] = &SafeJobConfig{Steps: []any{map[string]any{"run": "node prepare.cjs"}}}
			require.NoError(t, validateWorkQueueClaimAdapters(data))
		})
	}
}
