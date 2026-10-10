package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func awQueueCompilerFixture(t *testing.T, config string) (*Compiler, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, name := range []string{"alpha", "beta"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\non:\n  workflow_dispatch:\ntools:\n  work-queue:\n    worker: true\n---\nRun the assignment.\n"), 0o600))
	}
	if config != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "aw.json"), []byte(config), 0o600))
	}
	c := NewCompiler()
	c.gitRoot = root
	return c, filepath.Join(dir, "dispatcher.md")
}

func awQueueDispatcherFixture() *WorkflowData {
	return &WorkflowData{
		Tools:          map[string]any{"work-queue": true},
		RawFrontmatter: map[string]any{},
		SafeOutputs: &SafeOutputsConfig{
			DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{"beta"}},
		},
	}
}

func TestAWQueueDefaultOnlyCompilerBootstrap(t *testing.T) {
	for _, config := range []string{"", "{}", `{"work_queue":{}}`} {
		t.Run(config, func(t *testing.T) {
			c, path := awQueueCompilerFixture(t, config)
			data := awQueueDispatcherFixture()
			require.NoError(t, c.applyRepositoryWorkQueueOptions(data))
			require.NoError(t, c.applyRepositoryWorkQueueOptions(data))
			require.NoError(t, validateWorkQueueConfiguration(data))
			require.NoError(t, c.configureAWWorkQueue(data, path))
			require.NoError(t, c.validateWorkQueueTargets(data, path))
			policy := data.WorkQueuePolicy.Policy
			require.Equal(t, "aw", policy.Authorization)
			require.Empty(t, policy.Producers)
			require.Equal(t, 4096, policy.Limits.PendingNodes)
			pool := policy.Pools["default"]
			require.Equal(t, 16, pool.LogicalLimit)
			require.Equal(t, 16, pool.NativeLimit)
			require.Equal(t, 3, pool.Retry.MaxAttempts)
			require.Equal(t, int64(30000), pool.Retry.BackoffMS)
			for _, name := range []string{"alpha", "beta"} {
				profile := pool.Profiles[name]
				require.Equal(t, ".github/workflows/"+name+".lock.yml", profile.Workflow)
				require.Equal(t, "${{ github.sha }}", profile.Ref)
				require.Len(t, profile.LogicalContract, 64)
				require.Empty(t, profile.Principal)
				require.Equal(t, 1, profile.MaxClaims)
			}
			env := strings.Join(workQueuePolicyEnvironment(data), "")
			require.Contains(t, env, "GH_AW_WORK_QUEUE_POLICY:")
			require.NotContains(t, env, "github.actor_id")
			steps, err := c.buildWorkQueueControlProcessingStep(data)
			require.NoError(t, err)
			var controls map[string]any
			for _, line := range steps {
				if !strings.Contains(line, "GH_AW_WORK_QUEUE_CONTROL_CONFIG:") {
					continue
				}
				var encoded string
				require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.SplitN(line, ":", 2)[1])), &encoded))
				require.NoError(t, json.Unmarshal([]byte(encoded), &controls))
			}
			require.Equal(t, []any{"beta"}, controls["work_queue_workflows"])
		})
	}
}

func TestAWQueueSharedSchedulingAndGlobalIssues(t *testing.T) {
	c, path := awQueueCompilerFixture(t, `{"work_queue":{"concurrency":3,"pending_limit":30,"retry":{"max_attempts":2,"backoff_seconds":45},"pools":{"default":{},"reports":{"concurrency":2,"per_account_limit":1}},"issues":{"label":"cookie"}}}`)
	data := awQueueDispatcherFixture()
	require.NoError(t, c.applyRepositoryWorkQueueOptions(data))
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.NoError(t, c.configureAWWorkQueue(data, path))
	require.JSONEq(t, `{"label":"cookie"}`, data.WorkQueuePolicy.IssuesJSON)
	require.Equal(t, 30, data.WorkQueuePolicy.Policy.Limits.PendingNodes)
	require.Equal(t, 3, data.WorkQueuePolicy.Policy.Pools["default"].LogicalLimit)
	require.Equal(t, 2, data.WorkQueuePolicy.Policy.Pools["reports"].LogicalLimit)
	require.Equal(t, data.WorkQueuePolicy.Policy.Pools["default"].Profiles, data.WorkQueuePolicy.Policy.Pools["reports"].Profiles)
	require.Contains(t, strings.Join(workQueuePolicyEnvironment(data), ""), "GH_AW_WORK_QUEUE_ISSUES:")
}

func TestAWQueueConfigErrorsDoNotFallBackToDefaults(t *testing.T) {
	for _, config := range []string{
		`{"work_queue":null}`, `{"work_queue":{"concurrency":0}}`,
		`{"work_queue":{"pending_limit":1.5}}`, `{"work_queue":{"producers":{}}}`,
		`{"work_queue":{"issues":{"label":" "}}}`, `{"work_queue":{"issues":{"label":"${{ inputs.label }}"}}}`,
		`{"work_queue":{"retry":{"backoff_seconds":0}}}`,
	} {
		t.Run(config, func(t *testing.T) {
			c, _ := awQueueCompilerFixture(t, config)
			require.ErrorContains(t, c.applyRepositoryWorkQueueOptions(awQueueDispatcherFixture()), "work_queue")
		})
	}
}

func TestAWQueueRejectsGlobalAndWorkflowIssueDefinitions(t *testing.T) {
	c, _ := awQueueCompilerFixture(t, `{"work_queue":{"issues":true}}`)
	data := awQueueDispatcherFixture()
	data.Tools["work-queue"] = map[string]any{"issues": true}
	require.ErrorContains(t, c.applyRepositoryWorkQueueOptions(data), "conflicts")
}

func TestAWQueueRejectsGlobalAndWorkflowPolicyDefinitions(t *testing.T) {
	c, _ := awQueueCompilerFixture(t, `{"work_queue":{}}`)
	data := awQueueDispatcherFixture()
	data.RawFrontmatter["work-queue-policy"] = map[string]any{"mode": "weighted-priority"}
	require.ErrorContains(t, c.applyRepositoryWorkQueueOptions(data), "conflicts")
}

func TestAWQueueInvalidConfigDoesNotWarnAboutDefaults(t *testing.T) {
	c, _ := awQueueCompilerFixture(t, `{"work_queue":{"concurrency":0}}`)
	stderr := testutil.CaptureStderr(t, func() {
		require.Error(t, c.applyRepositoryWorkQueueOptions(awQueueDispatcherFixture()))
	})
	require.Contains(t, stderr, "invalid global work_queue configuration")
	require.NotContains(t, stderr, "continue with defaults")
}

func TestAWQueueWorkerConsumesInstalledPolicyWithoutProposal(t *testing.T) {
	c, path := awQueueCompilerFixture(t, "{}")
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}}
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.NoError(t, c.configureAWWorkQueue(data, filepath.Join(filepath.Dir(path), "alpha.md")))
	require.NotContains(t, strings.Join(workQueuePolicyEnvironment(data), ""), "GH_AW_WORK_QUEUE_POLICY:")
	require.Contains(t, strings.Join(workQueuePolicyEnvironment(data), ""), "GH_AW_WORK_QUEUE_CONTRACT:")
}
