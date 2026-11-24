//go:build !integration

package workflow

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adaptiveRoutingSource is a routed Copilot workflow (the implicit cost/auto default)
// with repo-memory, triggered as on gives and with extra frontmatter.
func adaptiveRoutingSource(on, extra string) string {
	return "---\non:" + on + "\npermissions:\n  contents: read\nengine: copilot\ntools:\n  repo-memory: true\n" + extra + "---\nTriage the newest issue.\n"
}

type adaptiveRoutingCompilation struct {
	lock string
	data *WorkflowData
	err  error
}

// compileAdaptiveRouting compiles source as .github/workflows/triage.md with an
// optional aw.json and any other files.
func compileAdaptiveRouting(t *testing.T, source, repoConfig string, files map[string]string) adaptiveRoutingCompilation {
	t.Helper()
	root := t.TempDir()
	workflowPath := filepath.Join(root, ".github", "workflows", "triage.md")
	all := map[string]string{".github/workflows/triage.md": source}
	if repoConfig != "" {
		all[RepoConfigFileName] = repoConfig
	}
	maps.Copy(all, files)
	for name, content := range all {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	compiler := NewCompiler(WithNoEmit(true))
	compiler.gitRoot = root
	var result adaptiveRoutingCompilation
	testutil.CaptureStderr(t, func() {
		result.data, result.err = compiler.ParseWorkflowFile(workflowPath)
		if result.err == nil {
			result.lock, result.err = compiler.CompileToYAML(result.data, workflowPath)
		}
	})
	return result
}

func lockJobs(t *testing.T, lock string) map[string]map[string]any {
	t.Helper()
	var workflow struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(lock), &workflow))
	return workflow.Jobs
}

func stepEnv(t *testing.T, job map[string]any, id string) map[string]any {
	t.Helper()
	for _, step := range job["steps"].([]any) {
		if step := step.(map[string]any); step["id"] == id {
			return step["env"].(map[string]any)
		}
	}
	t.Fatalf("step %s not found", id)
	return nil
}

func TestAdaptiveRoutingObserveAddsOnlyTheWriterJob(t *testing.T) {
	off := compileAdaptiveRouting(t, adaptiveRoutingSource(" workflow_dispatch", ""), "", nil)
	require.NoError(t, off.err)
	on := compileAdaptiveRouting(t, adaptiveRoutingSource(" workflow_dispatch", ""), `{"adaptive_routing": "observe"}`, nil)
	require.NoError(t, on.err)
	require.Equal(t, "observe", on.data.AdaptiveRouting)
	require.NotContains(t, off.lock, "push_routing_history")

	offJobs, onJobs := lockJobs(t, off.lock), lockJobs(t, on.lock)
	writer, added := onJobs[pushRoutingHistoryJobName]
	require.True(t, added)
	delete(onJobs, pushRoutingHistoryJobName)
	// Only conclusion's job inventory names the added job.
	for _, jobs := range []map[string]map[string]any{offJobs, onJobs} {
		env := jobs["conclusion"]["env"].(map[string]any)
		delete(env, "GH_AW_JOB_DISPLAY_NAMES")
		delete(env, "GH_AW_JOB_TIMING_ELIGIBLE")
	}
	assert.Equal(t, offJobs, onJobs, "observe must not change the agent job, its prompt, or any other job")

	assert.Equal(t, []any{"activation", "agent", "conclusion"}, writer["needs"])
	assert.Equal(t, "always() && needs.agent.result != 'skipped'", writer["if"])
	assert.Equal(t, map[string]any{"actions": "read", "contents": "write"}, writer["permissions"])
	assert.NotContains(t, writer, "concurrency")
	env := stepEnv(t, writer, "push_routing_history")
	assert.Equal(t, "triage", env["GH_AW_ROUTING_WORKFLOW"])
	assert.Equal(t, "observe", env["GH_AW_ROUTING_MODE"])
	assert.Equal(t, "copilot", env["GH_AW_ROUTING_ENGINE"])
	assert.Equal(t, "cost", env["GH_AW_ROUTING_GOAL"])
	assert.NotContains(t, env, "GH_AW_ROUTING_LEVEL", "a compile-time level shares records across levels")
	assert.Equal(t, "/tmp/gh-aw/usage/aw_session.jsonl", env["GH_AW_ROUTING_SESSION_FILE"])
	assert.Regexp(t, `^[0-9a-f]{64}:[0-9a-f]{64}$`, env["GH_AW_ROUTING_GENERATION"])
	assert.Equal(t, on.data.FrontmatterHash+":"+on.data.BodyHash, env["GH_AW_ROUTING_GENERATION"])
}

func TestAdaptiveRoutingRunTimeLevelHasItsOwnScope(t *testing.T) {
	compiled := compileAdaptiveRouting(t, adaptiveRoutingSource("\n  workflow_dispatch:\n    inputs:\n      level:\n        type: string", "model: ${{ inputs.level }}\nadaptive-routing: observe\n"), "", nil)
	require.NoError(t, compiled.err)
	require.True(t, isModelRoutingDynamic(compiled.data))
	env := stepEnv(t, lockJobs(t, compiled.lock)[pushRoutingHistoryJobName], "push_routing_history")
	assert.Equal(t, "${{ needs.activation.outputs.model-routing-mode }}", env["GH_AW_ROUTING_LEVEL"])
}

func TestAdaptiveRoutingSettingPrecedence(t *testing.T) {
	observeDefault := &RepoConfig{AdaptiveRouting: "observe"}
	tests := []struct {
		name        string
		frontmatter map[string]any
		imported    string
		repo        *RepoConfig
		want        adaptiveRoutingSetting
	}{
		{name: "off by default", want: adaptiveRoutingSetting{}},
		{name: "repository default", repo: observeDefault, want: adaptiveRoutingSetting{mode: "observe"}},
		{name: "import beats the repository default", imported: "false", repo: observeDefault, want: adaptiveRoutingSetting{explicit: true}},
		{name: "import opts in", imported: `"observe"`, want: adaptiveRoutingSetting{mode: "observe", explicit: true}},
		{name: "workflow false beats an import", frontmatter: map[string]any{"adaptive-routing": false}, imported: `"observe"`, repo: observeDefault, want: adaptiveRoutingSetting{explicit: true}},
		{name: "workflow opts in", frontmatter: map[string]any{"adaptive-routing": "observe"}, want: adaptiveRoutingSetting{mode: "observe", explicit: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveAdaptiveRoutingSetting(tt.frontmatter, tt.imported, tt.repo)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
	_, err := resolveAdaptiveRoutingSetting(map[string]any{"adaptive-routing": true}, "", nil)
	require.ErrorContains(t, err, "live adaptive routing is not available yet; use observe")
}

func TestAdaptiveRoutingFromAnImport(t *testing.T) {
	shared := "---\nadaptive-routing: observe\n---\nShared routing settings.\n"
	compiled := compileAdaptiveRouting(t, adaptiveRoutingSource(" workflow_dispatch", "imports:\n  - shared/routing.md\n"), "", map[string]string{".github/workflows/shared/routing.md": shared})
	require.NoError(t, compiled.err)
	assert.Contains(t, lockJobs(t, compiled.lock), pushRoutingHistoryJobName)
}

func TestAdaptiveRoutingIneligibleWorkflows(t *testing.T) {
	tests := []struct {
		name        string
		on          string
		frontmatter string
		reason      string
	}{
		{name: "fixed model", frontmatter: "model: gpt-5.4\n", reason: "its primary model is not chosen by model routing"},
		{name: "routing turned off", frontmatter: "model-routing: false\n", reason: "its primary model is not chosen by model routing"},
		// Each caller numbers its own runs, so their runs cannot share one history.
		{name: "reusable workflow", on: " workflow_call", reason: "reusable workflows (on: workflow_call) are not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			on := tt.on
			if on == "" {
				on = " workflow_dispatch"
			}
			explicit := compileAdaptiveRouting(t, adaptiveRoutingSource(on, tt.frontmatter+"adaptive-routing: observe\n"), "", nil)
			require.ErrorContains(t, explicit.err, tt.reason)
			defaulted := compileAdaptiveRouting(t, adaptiveRoutingSource(on, tt.frontmatter), `{"adaptive_routing": "observe"}`, nil)
			require.NoError(t, defaulted.err, "a repository default skips the workflow silently")
			assert.NotContains(t, defaulted.lock, pushRoutingHistoryJobName)
		})
	}

	withoutMemory := strings.Replace(adaptiveRoutingSource(" workflow_dispatch", "adaptive-routing: observe\n"), "tools:\n  repo-memory: true\n", "", 1)
	require.ErrorContains(t, compileAdaptiveRouting(t, withoutMemory, "", nil).err, "adaptive routing requires tools.repo-memory")

	live := compileAdaptiveRouting(t, adaptiveRoutingSource(" workflow_dispatch", "adaptive-routing: true\n"), "", nil)
	require.ErrorContains(t, live.err, "live adaptive routing is not available yet; use observe")
}

func TestRepoMemoryCannotWriteTheRoutingHistories(t *testing.T) {
	for _, memory := range []string{
		"  repo-memory:\n    branch-name: memory/adaptive-routing/triage\n",
		// The root itself would block every history branch.
		"  repo-memory:\n    branch-name: memory/adaptive-routing\n",
	} {
		source := strings.Replace(adaptiveRoutingSource(" workflow_dispatch", ""), "  repo-memory: true\n", memory, 1)
		require.ErrorContains(t, compileAdaptiveRouting(t, source, "", nil).err, "memory/adaptive-routing/ is reserved for adaptive routing histories")
	}
}

func TestLoadRepoConfig_AdaptiveRouting(t *testing.T) {
	root := t.TempDir()
	writeAWJSON(t, root, `{"adaptive_routing": "observe"}`)
	config, err := LoadRepoConfig(root)
	require.NoError(t, err)
	assert.Equal(t, "observe", config.AdaptiveRouting)

	writeAWJSON(t, root, `{"adaptive_routing": false}`)
	config, err = LoadRepoConfig(root)
	require.NoError(t, err)
	assert.Empty(t, config.AdaptiveRouting)

	writeAWJSON(t, root, `{"adaptive_routing": true}`)
	_, err = LoadRepoConfig(root)
	require.Error(t, err, "live adaptive routing has no repository default yet")
}
