//go:build !integration

package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestGenerateContinuationRelayWorkflow(t *testing.T) {
	t.Setenv("GH_AW_ACTION_MODE", "dev")
	dir := t.TempDir()
	data := []*WorkflowData{nil, {Name: "plain"}, {Name: "durable", SafeOutputs: &SafeOutputsConfig{RequestContinuation: &RequestContinuationConfig{}}}}

	require.NoError(t, GenerateContinuationRelayWorkflow(context.Background(), data, dir, nil))
	content, err := os.ReadFile(filepath.Join(dir, ContinuationRelayWorkflowFile))
	require.NoError(t, err)
	text := string(content)

	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(content, &parsed), "relay must be valid YAML")
	jobs := parsed["jobs"].(map[string]any)
	require.ElementsMatch(t, []string{"resolve", "approve", "dispatch"}, mapKeys(jobs))
	require.Contains(t, parsed, "concurrency", "relay runs for one parent attempt are serialized")
	require.Equal(t, map[string]any{}, parsed["permissions"])
	require.Equal(t, map[string]any{"actions": "read", "contents": "read"}, jobs["resolve"].(map[string]any)["permissions"])
	require.Equal(t, map[string]any{}, jobs["approve"].(map[string]any)["permissions"])
	require.Equal(t, "${{ needs.resolve.outputs.approval_environment }}", jobs["approve"].(map[string]any)["environment"])
	require.Equal(t, map[string]any{"actions": "write", "contents": "read"}, jobs["dispatch"].(map[string]any)["permissions"])

	require.Contains(t, text, "run-name: Continuation relay ${{ inputs.parent_run_id }}/${{ inputs.parent_run_attempt }}")
	require.Contains(t, text, "checkpoint_run_attempt:")
	require.Contains(t, text, "GH_AW_PARENT_CHECKPOINT_ATTEMPT: ${{ inputs.checkpoint_run_attempt }}")
	require.Contains(t, text, "GH_AW_PARENT_RUN_ID: ${{ inputs.parent_run_id }}", "untrusted inputs must only reach scripts through env")
	require.Contains(t, text, "github.event_name == 'workflow_dispatch' && !github.event.repository.fork && github.ref == format('refs/heads/{0}', github.event.repository.default_branch)", "the relay only runs as a default-branch workflow_dispatch")
	require.Contains(t, text, "await resolve();")
	require.Contains(t, text, "await dispatch();")
	require.Contains(t, text, "GH_AW_APPROVAL_RESULT: ${{ needs.approve.result }}")
	require.Contains(t, text, "GH_AW_APPROVAL_ENVIRONMENT: ${{ needs.resolve.outputs.approval_environment }}")
	require.Contains(t, text, "name: continuation-dispatched")
	require.Contains(t, text, "retention-days: 90")
}

func TestGenerateContinuationRelayWorkflowRemovesStaleRelay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ContinuationRelayWorkflowFile)
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0o600))

	require.NoError(t, GenerateContinuationRelayWorkflow(context.Background(), []*WorkflowData{{Name: "plain", SafeOutputs: &SafeOutputsConfig{}}}, dir, nil))
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}
