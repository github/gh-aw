//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReactionCleanupConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reaction string
		enabled  bool
		invalid  bool
	}{
		{"scalar unchanged", "eyes", false, false},
		{"object default unchanged", "{type: eyes}", false, false},
		{"enabled", "{type: eyes, remove-on-complete: true}", true, false},
		{"explicit false", "{type: eyes, remove-on-complete: false}", false, false},
		{"default eyes", "{remove-on-complete: true}", true, false},
		{"none", "{type: none, remove-on-complete: true}", false, false},
		{"invalid boolean", "{remove-on-complete: sometimes}", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "reaction.md")
			content := "---\non:\n  issues:\n    types: [opened]\n  reaction: " + tc.reaction + "\n  status-comment: false\nstrict: false\n---\nTest reaction cleanup.\n"
			require.NoError(t, os.WriteFile(file, []byte(content), 0600))
			c := NewCompiler()
			data, err := c.ParseWorkflowFile(file)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.enabled, reactionCleanupEnabled(data))
			yaml, _, _, err := c.generateYAML(data, file)
			require.NoError(t, err)
			assert.Equal(t, tc.enabled, strings.Contains(yaml, "Remove trigger reaction on completion"))
			assert.Equal(t, tc.enabled, strings.Contains(yaml, "reaction_id: ${{ steps.react.outputs.reaction-id }}"))
			assert.NotContains(t, yaml, "Update reaction comment with completion status")
			if tc.enabled {
				assert.Contains(t, yaml, "GH_AW_REMOVE_REACTION_ID: ${{ needs.activation.outputs.reaction_id }}")
				assert.Contains(t, yaml, "if: always() && needs.activation.outputs.reaction_id != ''")
			}
		})
	}
}

func TestReactionCleanupConclusion(t *testing.T) {
	data := &WorkflowData{
		AI: "copilot", On: "on:\n  issues:\n    types: [opened]",
		AIReaction: ReactionTypeEyes, ReactionRemoveOnComplete: true,
		SafeOutputs: &SafeOutputsConfig{},
	}
	c := NewCompiler()
	job, err := c.buildConclusionJob(data, "agent", []string{"add_comment"})
	require.NoError(t, err)
	require.NotNil(t, job)
	// Cleanup must run even if the agent was skipped or a safe output posted a comment.
	assert.Contains(t, job.If, "|| always() && needs.activation.outputs.reaction_id != ''")
	assert.Contains(t, job.Permissions, "issues: write")
	assert.NotContains(t, job.Permissions, "discussions: write")
	steps := strings.Join(job.Steps, "")
	assert.Contains(t, steps, "github-token: ${{ secrets.GITHUB_TOKEN }}")
	assert.NotContains(t, steps, "Update reaction comment")

	data.ActivationGitHubToken = "${{ secrets.REACTION_TOKEN }}"
	steps = strings.Join(c.buildReactionCleanupSteps(data), "")
	assert.Contains(t, steps, "github-token: ${{ secrets.REACTION_TOKEN }}")
	baseline := *data
	baseline.ReactionRemoveOnComplete = false
	assert.Equal(t, computeConclusionJobPermissions(&baseline).RenderToYAML(), computeConclusionJobPermissions(data).RenderToYAML())

	data.ActivationGitHubApp = &GitHubAppConfig{
		AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}",
	}
	steps = strings.Join(c.buildReactionCleanupSteps(data), "")
	assert.Contains(t, steps, "id: activation-app-token")
	assert.Contains(t, steps, "permission-issues: write")
	assert.Contains(t, steps, "github-token: ${{ steps.activation-app-token.outputs.token }}")
	assert.NotContains(t, steps, "permission-discussions: write")
	assert.Contains(t, steps, "skip-token-revoke: false")

	data.On = "on:\n  workflow_call:"
	steps = strings.Join(c.buildReactionCleanupSteps(data), "")
	assert.Contains(t, steps, "repositories: ${{ github.event.repository.name }}")
	assert.NotContains(t, steps, "repositories: ${{ needs.activation.outputs.target_repo_name }}")
	assert.Contains(t, steps, "owner: ${{ github.repository_owner }}")
}

func TestReactionCleanupTargetPermissions(t *testing.T) {
	for _, tc := range []struct {
		trigger string
		scope   PermissionScope
	}{
		{"issues", PermissionIssues},
		{"issue_comment", PermissionIssues},
		{"pull_request", PermissionPullRequests},
		{"pull_request_review_comment", PermissionPullRequests},
		{"discussion", PermissionDiscussions},
		{"discussion_comment", PermissionDiscussions},
	} {
		t.Run(tc.trigger, func(t *testing.T) {
			data := &WorkflowData{
				On:         "on:\n  " + tc.trigger + ":\n    types: [created]",
				AIReaction: ReactionTypeEyes, ReactionRemoveOnComplete: true,
				SafeOutputs: &SafeOutputsConfig{},
			}
			level, ok := computeConclusionJobPermissions(data).Get(tc.scope)
			require.True(t, ok)
			assert.Equal(t, PermissionWrite, level)
		})
	}
}

func TestReactionCleanupDryRun(t *testing.T) {
	data := &WorkflowData{AIReaction: ReactionTypeEyes, ReactionRemoveOnComplete: true, SafeOutputs: &SafeOutputsConfig{}}
	c := NewCompiler()
	c.dryRun = true
	dry := c.dryRunConclusionData(data)
	assert.False(t, dry.ReactionRemoveOnComplete)
	assert.Empty(t, c.buildReactionCleanupSteps(dry))
	assert.True(t, data.ReactionRemoveOnComplete)
}

func TestReactionCleanupDoesNotExpandSafeOutputAppToken(t *testing.T) {
	data := &WorkflowData{
		On:         "on:\n  discussion:\n    types: [created]",
		AIReaction: ReactionTypeEyes, ReactionRemoveOnComplete: true,
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID: "${{ vars.SAFE_OUTPUT_APP_ID }}", PrivateKey: "${{ secrets.SAFE_OUTPUT_APP_KEY }}",
			},
		},
	}
	c := NewCompiler()
	assert.Contains(t, computeConclusionJobPermissions(data).RenderToYAML(), "discussions: write")
	setup := strings.Join(c.buildConclusionSetupSteps(data), "")
	assert.NotContains(t, setup, "permission-discussions:")
	cleanup := strings.Join(c.buildReactionCleanupSteps(data), "")
	assert.Contains(t, cleanup, "github-token: ${{ secrets.GITHUB_TOKEN }}")
	assert.NotContains(t, cleanup, "safe-outputs-app-token")
}
