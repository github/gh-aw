package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workqueue"
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

func compileAWQueuePolicy(t *testing.T, c *Compiler, data *WorkflowData, path string) workqueue.PolicyEnv {
	t.Helper()
	require.NoError(t, c.applyRepositoryWorkQueueOptions(data))
	require.NoError(t, validateWorkQueueConfiguration(data))
	require.NoError(t, c.configureAWWorkQueue(data, path))
	require.NoError(t, c.validateWorkQueueTargets(data, path))
	policy, err := workqueue.ParsePolicyEnv([]byte(strings.ReplaceAll(data.WorkQueuePolicy.PolicyJSON, "${{ github.repository }}", "owner/repo")))
	require.NoError(t, err)
	return policy
}

func TestAWQueueDefaultOnlyCompilerPolicy(t *testing.T) {
	for _, config := range []string{"", "{}", `{"work_queue":{}}`} {
		t.Run(config, func(t *testing.T) {
			c, path := awQueueCompilerFixture(t, config)
			data := awQueueDispatcherFixture()
			require.NoError(t, c.applyRepositoryWorkQueueOptions(data))
			policy := compileAWQueuePolicy(t, c, data, path)
			require.Equal(t, 4, policy.Version)
			require.Equal(t, 4096, policy.PendingLimit)
			require.Nil(t, policy.Issues)
			pool := policy.Pools["default"]
			require.Equal(t, 16, pool.Concurrency)
			require.Equal(t, 0, pool.PerAccountLimit)
			require.Equal(t, 3, pool.MaxAttempts)
			require.Equal(t, "alpha", pool.DefaultProfile)
			for _, name := range []string{"alpha", "beta"} {
				require.Equal(t, workqueue.ProfileEnv{Workflow: ".github/workflows/" + name + ".lock.yml", EffectScope: "owner/repo"}, pool.Profiles[name])
			}
			require.Contains(t, data.WorkQueuePolicy.PolicyJSON, `"effect_scope":"${{ github.repository }}"`)
			env := strings.Join(workQueuePolicyEnvironment(data), "")
			require.Contains(t, env, "GH_AW_WORK_QUEUE_POLICY:")
			require.NotContains(t, env, "GH_AW_WORK_QUEUE_ISSUES")
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
			require.NotContains(t, controls, "work_queue_dispatch_credential")
		})
	}
}

func TestAWQueueSharedSchedulingAndGlobalIssues(t *testing.T) {
	c, path := awQueueCompilerFixture(t, `{"work_queue":{"concurrency":3,"pending_limit":30,"retry":{"max_attempts":2},"pools":{"default":{},"reports":{"concurrency":2,"per_account_limit":1}},"issues":{"label":"cookie","orphaned_after_days":7}}}`)
	data := awQueueDispatcherFixture()
	policy := compileAWQueuePolicy(t, c, data, path)
	require.Equal(t, &workqueue.IssuesEnv{Label: "cookie", TitlePrefix: "[work queue]", OrphanedAfterDays: 7}, policy.Issues)
	require.Equal(t, 30, policy.PendingLimit)
	require.Equal(t, 3, policy.Pools["default"].Concurrency)
	require.Equal(t, 2, policy.Pools["default"].MaxAttempts)
	require.Equal(t, 2, policy.Pools["reports"].Concurrency)
	require.Equal(t, 1, policy.Pools["reports"].PerAccountLimit)
	require.Equal(t, policy.Pools["default"].Profiles, policy.Pools["reports"].Profiles)
}

func TestAWQueueConfigErrorsDoNotFallBackToDefaults(t *testing.T) {
	for _, config := range []string{
		`{"work_queue":null}`, `{"work_queue":{"concurrency":0}}`,
		`{"work_queue":{"pending_limit":1.5}}`, `{"work_queue":{"producers":{}}}`,
		`{"work_queue":{"issues":{"label":" "}}}`, `{"work_queue":{"issues":{"label":"${{ inputs.label }}"}}}`,
		`{"work_queue":{"issues":{"orphaned_after_days":0}}}`, `{"work_queue":{"issues":{"orphaned_after_days":3651}}}`,
		`{"work_queue":{"retry":{"backoff_seconds":30}}}`, `{"work_queue":{"accounting_weights":{"":1}}}`,
		`{"work_queue":{"mode":"strict-priority"}}`,
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

func TestAWQueueInvalidConfigDoesNotWarnAboutDefaults(t *testing.T) {
	c, _ := awQueueCompilerFixture(t, `{"work_queue":{"concurrency":0}}`)
	stderr := testutil.CaptureStderr(t, func() {
		require.Error(t, c.applyRepositoryWorkQueueOptions(awQueueDispatcherFixture()))
	})
	require.Contains(t, stderr, "invalid global work_queue configuration")
	require.NotContains(t, stderr, "continue with defaults")
}

func TestAWQueueWorkerCarriesCompiledPolicy(t *testing.T) {
	c, path := awQueueCompilerFixture(t, `{"work_queue":{"pools":{"default":{"concurrency":2}}}}`)
	data := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"worker": true}}}
	policy := compileAWQueuePolicy(t, c, data, filepath.Join(filepath.Dir(path), "alpha.md"))
	require.Equal(t, 2, policy.Pools["default"].Concurrency)
	require.Contains(t, policy.Pools["default"].Profiles, "alpha")
	env := strings.Join(workQueuePolicyEnvironment(data), "")
	require.Contains(t, env, "GH_AW_WORK_QUEUE_POLICY:")
	require.NotContains(t, env, "GH_AW_WORK_QUEUE_CONTRACT")
}

func TestAWQueueRepositoryPolicyPrefersIssueProjection(t *testing.T) {
	plain := &WorkflowData{WorkQueuePolicy: &WorkQueuePolicyConfig{PolicyJSON: `{"plain":true}`}}
	issues := &WorkflowData{Tools: map[string]any{"work-queue": map[string]any{"issues": true}}, WorkQueuePolicy: &WorkQueuePolicyConfig{PolicyJSON: `{"issues":true}`}}
	observer := &WorkflowData{WorkQueuePolicy: &WorkQueuePolicyConfig{}}
	require.Empty(t, workQueueRepositoryPolicyJSON([]*WorkflowData{nil, observer}))
	require.JSONEq(t, `{"plain":true}`, workQueueRepositoryPolicyJSON([]*WorkflowData{observer, plain}))
	require.JSONEq(t, `{"issues":true}`, workQueueRepositoryPolicyJSON([]*WorkflowData{plain, issues}))
}
