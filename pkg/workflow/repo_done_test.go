//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoDoneParsingAndSchema(t *testing.T) {
	for _, value := range []any{true, false, nil, map[string]any{}, map[string]any{
		"body": "Done\nwith checks", "github-token": "${{ secrets.DONE_TOKEN }}", "staged": "${{ inputs.preview }}",
	}} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			frontmatter := map[string]any{"on": "workflow_dispatch", "safe-outputs": map[string]any{"repo-done": value}}
			require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "repo-done.md"))
			config := NewCompiler().extractSafeOutputsConfig(frontmatter)
			require.NotNil(t, config.RepoDone)
			assert.Equal(t, value != false, repoDoneEnabled(config))
			if _, object := value.(map[string]any); object && config.RepoDone.Body != "" {
				assert.Equal(t, "Done\nwith checks", config.RepoDone.Body)
				assert.Equal(t, "${{ secrets.DONE_TOKEN }}", config.RepoDone.GitHubToken)
				assert.Equal(t, TemplatableBool("${{ inputs.preview }}"), *config.RepoDone.Staged)
			}
		})
	}
	for _, value := range []any{"true", 1, []any{}, map[string]any{"body": false}, map[string]any{"target": "other/repo"}, map[string]any{"github-app": map[string]any{}}, map[string]any{"staged": 1}} {
		err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "safe-outputs": map[string]any{"repo-done": value},
		}, "repo-done.md")
		require.Error(t, err, "%v", value)
	}
	assert.Nil(t, NewCompiler().parseRepoDoneConfig(map[string]any{}))
	keys, err := parser.GetSafeOutputTypeKeys()
	require.NoError(t, err)
	assert.NotContains(t, keys, "repo-done", "declarative reporting must not be registered as an agent tool")
}

func TestRepoDoneImports(t *testing.T) {
	c := NewCompiler()
	for _, enabled := range []bool{false, true} {
		raw := map[string]any{"repo-done": enabled}
		top := c.extractSafeOutputsConfig(map[string]any{"safe-outputs": raw})
		result, err := c.MergeSafeOutputs(top, []string{`{"repo-done":{"body":"Imported"}}`}, raw)
		require.NoError(t, err)
		assert.Equal(t, enabled, repoDoneEnabled(result))
		assert.Empty(t, result.RepoDone.Body, "explicit main configuration wins")
	}
	result, err := c.MergeSafeOutputs(&SafeOutputsConfig{}, []string{`{"repo-done":{"body":"Imported"}}`}, nil)
	require.NoError(t, err)
	require.NotNil(t, result.RepoDone)
	assert.Equal(t, "Imported", result.RepoDone.Body)
	result, err = c.MergeSafeOutputs(&SafeOutputsConfig{}, []string{`{"repo-done":false}`, `{"repo-done":true}`}, nil)
	require.NoError(t, err)
	assert.False(t, repoDoneEnabled(result), "the first explicit import is retained")
}

func TestRepoDoneConclusion(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{AI: "copilot", SafeOutputs: &SafeOutputsConfig{RepoDone: &RepoDoneConfig{}}}
	applyDefaultCreateIssue(data)
	assert.Nil(t, data.SafeOutputs.CreateIssues, "repo-done must not enable an agent issue-writing tool")
	configJSON, err := generateSafeOutputsConfig(data)
	require.NoError(t, err)
	assert.Empty(t, configJSON, "repo-done must not be exposed in agent handler configuration")
	condition := RenderCondition(c.buildConclusionJobCondition(data, "agent", []string{"add_comment"}))
	assert.Contains(t, condition, "always()")
	assert.NotContains(t, condition, "needs.add_comment.outputs.comment_id", "agent comments cannot suppress repo-done")
	steps := strings.Join(c.buildRepoDoneStep(data, "agent"), "")
	assert.Contains(t, steps, "always() && needs.activation.result == 'success' && needs.agent.result != 'skipped' && needs.agent.result != ''")
	assert.Contains(t, steps, "/repo_done.cjs")
	assert.NotContains(t, steps, "load_agent_output")
	assert.NotContains(t, steps, "download-artifact")
	data.SafeOutputs.RepoDone.Disabled = true
	assert.Empty(t, c.buildRepoDoneStep(data, "agent"))
	assert.Contains(t, RenderCondition(c.buildConclusionJobCondition(data, "agent", []string{"add_comment"})), "needs.add_comment.outputs.comment_id")
	data.SafeOutputs.RepoDone = nil
	assert.Empty(t, c.buildRepoDoneStep(data, "agent"))
}

func TestRepoDonePermissionsAndDryRun(t *testing.T) {
	for _, mode := range []string{"live", "global-staged", "local-staged", "dry-run", "global-overrides-local"} {
		t.Run(mode, func(t *testing.T) {
			c := NewCompiler()
			data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{RepoDone: &RepoDoneConfig{}}}
			yes, no := TemplatableBool("true"), TemplatableBool("false")
			switch mode {
			case "global-staged", "global-overrides-local":
				data.SafeOutputs.Staged = &yes
				data.SafeOutputs.RepoDone.Staged = &no
			case "local-staged":
				data.SafeOutputs.RepoDone.Staged = &yes
			case "dry-run":
				c.SetDryRun(true)
				data.SafeOutputs.RepoDone.Staged = &no
				data = c.dryRunConclusionData(data)
			}
			permissions := NewPermissions()
			addRepoDonePermissions(data, permissions)
			for _, scope := range []PermissionScope{PermissionIssues, PermissionPullRequests, PermissionDiscussions, PermissionContents} {
				level, found := permissions.Get(scope)
				assert.Equal(t, mode == "live", found)
				if found {
					assert.Equal(t, PermissionWrite, level)
				}
			}
			// This type must never grant the ordinary safe_outputs/agent jobs scopes.
			assert.Empty(t, ComputePermissionsForSafeOutputs(data.SafeOutputs).permissions)
			if mode == "dry-run" {
				step := strings.Join(c.buildRepoDoneStep(data, "agent"), "")
				assert.Contains(t, step, `GH_AW_REPO_DONE_DRY_RUN: "true"`)
				assert.Contains(t, step, `GH_AW_SAFE_OUTPUTS_STAGED: "true"`)
				assert.Contains(t, step, `GH_AW_REPO_DONE_STAGED: "false"`)
			}
		})
	}
}

func TestRepoDoneRejectsWorkQueueParticipants(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{
		Tools:       map[string]any{"work-queue": map[string]any{"worker": true}},
		SafeOutputs: &SafeOutputsConfig{RepoDone: &RepoDoneConfig{}},
	}
	require.True(t, isWorkQueueParticipant(data))
	_, err := c.buildConclusionJob(data, "agent", nil)
	require.ErrorContains(t, err, "repo-done is not supported for work-queue participants")
}

func TestRepoDoneNativePermissions(t *testing.T) {
	for _, tc := range []struct {
		trigger string
		scopes  []PermissionScope
	}{
		{"issues", []PermissionScope{PermissionIssues}},
		{"pull_request", []PermissionScope{PermissionPullRequests}},
		{"issue_comment", []PermissionScope{PermissionIssues, PermissionPullRequests}},
		{"discussion_comment", []PermissionScope{PermissionDiscussions}},
		{"push", []PermissionScope{PermissionContents}},
		{"commit_comment", []PermissionScope{PermissionContents}},
		{"workflow_run", nil},
		{"check_run", nil},
		{"check_suite", nil},
		{"schedule", nil},
		{"workflow_dispatch", []PermissionScope{PermissionIssues, PermissionPullRequests, PermissionDiscussions, PermissionContents}},
	} {
		t.Run(tc.trigger, func(t *testing.T) {
			data := &WorkflowData{On: "on:\n  " + tc.trigger + ": {}\n", SafeOutputs: &SafeOutputsConfig{RepoDone: &RepoDoneConfig{}}}
			permissions := NewPermissions()
			addRepoDonePermissions(data, permissions)
			assert.Len(t, permissions.permissions, len(tc.scopes))
			for _, scope := range tc.scopes {
				level, found := permissions.Get(scope)
				require.True(t, found)
				assert.Equal(t, PermissionWrite, level)
			}
		})
	}
}

func TestRepoDoneTokens(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{On: "on:\n  discussion: {}\n", SafeOutputs: &SafeOutputsConfig{RepoDone: &RepoDoneConfig{}}}
	assert.Contains(t, strings.Join(c.buildRepoDoneStep(data, "agent"), ""), "secrets.GH_AW_GITHUB_TOKEN")
	data.SafeOutputs.GitHubToken = "${{ secrets.GLOBAL_TOKEN }}"
	assert.Contains(t, strings.Join(c.buildRepoDoneStep(data, "agent"), ""), "github-token: ${{ secrets.GLOBAL_TOKEN }}")
	data.SafeOutputs.RepoDone.GitHubToken = "${{ secrets.DONE_TOKEN }}"
	assert.Contains(t, strings.Join(c.buildRepoDoneStep(data, "agent"), ""), "github-token: ${{ secrets.DONE_TOKEN }}")
	data.SafeOutputs.GitHubApp = &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}"}
	assert.Contains(t, strings.Join(c.buildRepoDoneStep(data, "agent"), ""), "steps.safe-outputs-app-token.outputs.token")
	setup := strings.Join(c.buildConclusionSetupSteps(data), "")
	assert.Contains(t, setup, "permission-discussions: write")
	assert.NotContains(t, setup, "permission-contents: write")
	yes := TemplatableBool("true")
	data.SafeOutputs.Staged = &yes
	assert.NotContains(t, strings.Join(c.buildConclusionSetupSteps(data), ""), "permission-discussions: write")
}

func TestRepoDoneCompiled(t *testing.T) {
	const source = `---
on:
  issues: {}
  pull_request: {}
  discussion: {}
  push: {}
  workflow_dispatch:
strict: false
engine: copilot
permissions:
  contents: read
safe-outputs:
  repo-done:
    body: "Checks complete."
    github-token: "${{ secrets.DONE_TOKEN }}"
    staged: false
  report-failed-jobs: false
  report-failure-as-issue: false
  report-missing-tool: false
  report-incomplete: false
---
Run the repository checks.
`
	dir := t.TempDir()
	path := filepath.Join(dir, "repo-done.md")
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	for _, dryRun := range []bool{false, true} {
		c := NewCompiler()
		c.gitRoot = dir
		c.SetSkipValidation(true)
		c.SetDryRun(dryRun)
		require.NoError(t, c.CompileWorkflow(path))
		content, err := os.ReadFile(filepath.Join(dir, "repo-done.lock.yml"))
		require.NoError(t, err)
		var compiled map[string]any
		require.NoError(t, yaml.Unmarshal(content, &compiled))
		jobs := compiled["jobs"].(map[string]any)
		agent := jobs["agent"].(map[string]any)
		assert.NotContains(t, fmt.Sprint(agent), "DONE_TOKEN")
		assert.NotContains(t, fmt.Sprint(agent), "repo_done.cjs")
		assert.NotContains(t, fmt.Sprint(agent["permissions"]), "write")
		conclusion := jobs["conclusion"].(map[string]any)
		steps := fmt.Sprint(conclusion["steps"])
		assert.Contains(t, steps, "repo_done.cjs")
		assert.Contains(t, steps, "secrets.DONE_TOKEN")
		assert.Equal(t, !dryRun, strings.Contains(fmt.Sprint(conclusion["permissions"]), "write"))
	}
}
