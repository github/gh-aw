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

// routingLearningSource is a routed Copilot workflow (the implicit cost/auto default)
// with repo-memory, triggered as on gives and with extra frontmatter.
func routingLearningSource(on, extra string) string {
	return "---\non:" + on + "\npermissions:\n  contents: read\nengine: copilot\ntools:\n  repo-memory: true\n" + extra + "---\nTriage the newest issue.\n"
}

type routingLearningCompilation struct {
	lock string
	data *WorkflowData
	err  error
}

// compileRoutingLearning compiles source as .github/workflows/triage.md with an
// optional aw.json and any other files.
func compileRoutingLearning(t *testing.T, source, repoConfig string, files map[string]string) routingLearningCompilation {
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
	var result routingLearningCompilation
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

func TestRoutingLearningObserveAddsOnlyTheWriterJob(t *testing.T) {
	off := compileRoutingLearning(t, routingLearningSource(" workflow_dispatch", ""), "", nil)
	require.NoError(t, off.err)
	on := compileRoutingLearning(t, routingLearningSource(" workflow_dispatch", ""), `{"model_routing": {"learning": "observe"}}`, nil)
	require.NoError(t, on.err)
	require.Equal(t, "observe", on.data.RoutingLearningMode)
	require.NotContains(t, off.lock, "push_routing_history")

	offJobs, onJobs := lockJobs(t, off.lock), lockJobs(t, on.lock)
	writer, added := onJobs[pushRoutingHistoryJobName]
	require.True(t, added)
	delete(onJobs, pushRoutingHistoryJobName)
	// Opting in without explicit safe-outputs also emits the small fallback artifact upload
	// on the agent job so routing observations survive an agent artifact upload timeout.
	onAgentSteps := onJobs["agent"]["steps"].([]any)
	fallbackStepIdx := -1
	for i, raw := range onAgentSteps {
		if step, ok := raw.(map[string]any); ok && step["name"] == "Upload agent output fallback artifact" {
			fallbackStepIdx = i
			break
		}
	}
	require.GreaterOrEqual(t, fallbackStepIdx, 0, "observe must emit the fallback artifact upload when no explicit safe-outputs are declared")
	onJobs["agent"]["steps"] = append(append([]any{}, onAgentSteps[:fallbackStepIdx]...), onAgentSteps[fallbackStepIdx+1:]...)
	// Only conclusion's job inventory names the added job.
	for _, jobs := range []map[string]map[string]any{offJobs, onJobs} {
		env := jobs["conclusion"]["env"].(map[string]any)
		delete(env, "GH_AW_JOB_DISPLAY_NAMES")
		delete(env, "GH_AW_JOB_TIMING_ELIGIBLE")
	}
	assert.Equal(t, offJobs, onJobs, "observe must not change the agent prompt, permissions, model selection, or any other job")

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

func TestRoutingLearningRunTimeLevelHasItsOwnScope(t *testing.T) {
	compiled := compileRoutingLearning(t, routingLearningSource("\n  workflow_dispatch:\n    inputs:\n      level:\n        type: string", "model: ${{ inputs.level }}\nmodel-routing:\n  learning: observe\n"), "", nil)
	require.NoError(t, compiled.err)
	require.True(t, isModelRoutingDynamic(compiled.data))
	env := stepEnv(t, lockJobs(t, compiled.lock)[pushRoutingHistoryJobName], "push_routing_history")
	assert.Equal(t, "${{ needs.activation.outputs.model-routing-mode }}", env["GH_AW_ROUTING_LEVEL"])
}

func TestRoutingLearningSettingPrecedence(t *testing.T) {
	observeDefault := &RepoConfig{RoutingLearning: "observe"}
	tests := []struct {
		name        string
		frontmatter map[string]any
		imported    string
		repo        *RepoConfig
		want        routingLearningSetting
	}{
		{name: "off by default", want: routingLearningSetting{}},
		{name: "repository default", repo: observeDefault, want: routingLearningSetting{mode: "observe"}},
		{name: "import beats the repository default", imported: "false", repo: observeDefault, want: routingLearningSetting{explicit: true}},
		{name: "import opts in", imported: `"observe"`, want: routingLearningSetting{mode: "observe", explicit: true}},
		{name: "workflow false beats an import", frontmatter: map[string]any{"model-routing": map[string]any{"learning": false}}, imported: `"observe"`, repo: observeDefault, want: routingLearningSetting{explicit: true}},
		{name: "workflow opts in", frontmatter: map[string]any{"model-routing": map[string]any{"learning": "observe"}}, want: routingLearningSetting{mode: "observe", explicit: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveRoutingLearningSetting(tt.frontmatter, tt.imported, tt.repo)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRoutingLearningFromAnImport(t *testing.T) {
	compiled := compileRoutingLearning(t, routingLearningSource(" workflow_dispatch", "imports: [shared/off.md, shared/observe.md]\n"), `{"model_routing":{"learning":"observe"}}`, map[string]string{
		".github/workflows/shared/off.md":     "---\nmodel-routing:\n  learning: false\n---\nDo not record.\n",
		".github/workflows/shared/observe.md": "---\nmodel-routing:\n  learning: observe\n---\nRecord the ordinary pick.\n",
	})
	require.NoError(t, compiled.err)
	assert.True(t, isModelRoutingEnabled(compiled.data))
	assert.NotContains(t, lockJobs(t, compiled.lock), pushRoutingHistoryJobName, "the first import opts out despite the later opt-ins")
}

func TestRoutingLearningComposesWithShortlist(t *testing.T) {
	for _, tc := range []struct{ name, local, imported string }{
		{"same block", "  allowed-models: [gpt-5.6-luna]\n  learning: observe\n", ""},
		{"imported learning", "  allowed-models: [gpt-5.6-luna]\n", "  learning: observe\n"},
		{"imported shortlist", "  learning: observe\n", "  allowed-models: [gpt-5.6-luna]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := "model: router/balanced\nmodel-routing:\n" + tc.local
			var files map[string]string
			if tc.imported != "" {
				config += "imports: [shared/routing.md]\n"
				files = map[string]string{".github/workflows/shared/routing.md": "---\nmodel-routing:\n" + tc.imported + "---\n"}
			}
			got := compileRoutingLearning(t, routingLearningSource(" workflow_dispatch", config), "", files)
			require.NoError(t, got.err)
			require.Equal(t, []string{"gpt-5.6-luna"}, got.data.EngineConfig.ModelRouting.AllowedModels)
			require.Contains(t, lockJobs(t, got.lock), pushRoutingHistoryJobName)
		})
	}
}

func TestRoutingLearningFalsePreservesImplicitRouting(t *testing.T) {
	got := compileRoutingLearning(t, routingLearningSource(" workflow_dispatch", "model-routing:\n  learning: false\n"), `{"model_routing":{"learning":"observe"}}`, nil)
	require.NoError(t, got.err)
	require.True(t, isModelRoutingEnabled(got.data))
	require.NotContains(t, lockJobs(t, got.lock), pushRoutingHistoryJobName)
}

func TestRoutingLearningIneligibleWorkflows(t *testing.T) {
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
			explicit := compileRoutingLearning(t, routingLearningSource(on, tt.frontmatter+"imports: [shared/observe.md]\n"), "", map[string]string{
				".github/workflows/shared/observe.md": "---\nmodel-routing:\n  learning: observe\n---\n",
			})
			require.ErrorContains(t, explicit.err, tt.reason)
			defaulted := compileRoutingLearning(t, routingLearningSource(on, tt.frontmatter), `{"model_routing": {"learning": "observe"}}`, nil)
			require.NoError(t, defaulted.err, "a repository default skips the workflow silently")
			assert.NotContains(t, defaulted.lock, pushRoutingHistoryJobName)
		})
	}

	withoutMemory := strings.Replace(routingLearningSource(" workflow_dispatch", "model-routing:\n  learning: observe\n"), "tools:\n  repo-memory: true\n", "", 1)
	require.ErrorContains(t, compileRoutingLearning(t, withoutMemory, "", nil).err, "tools.repo-memory is required")
}

func TestRoutingLearningRejectsUnsupportedSyntax(t *testing.T) {
	for _, tt := range []struct{ config, diagnostic string }{
		{"adaptive-routing: observe", "Unknown property: adaptive-routing"},
		{"model-routing: {learning: true}", "'learning': value must be one of"},
		{"model-routing: {learning: live}", "'learning': value must be one of"},
		{"model-routing: {learning: {mode: live, tests: shadow}}", "'learning': value must be one of"},
		{"model-routing: {}", "minProperties: got 0, want 1"},
	} {
		t.Run(tt.config, func(t *testing.T) {
			got := compileRoutingLearning(t, routingLearningSource(" workflow_dispatch", tt.config+"\n"), "", nil)
			require.ErrorContains(t, got.err, tt.diagnostic)
			imported := compileRoutingLearning(t, routingLearningSource(" workflow_dispatch", "imports: [shared/config.md]\n"), "", map[string]string{
				".github/workflows/shared/config.md": "---\n" + tt.config + "\n---\n",
			})
			require.ErrorContains(t, imported.err, tt.diagnostic)
		})
	}
}

func TestRoutingLearningRejectsRemovedOptOutInMarkdownImport(t *testing.T) {
	// Ordinary Markdown imports allow unrelated frontmatter. Ignoring this old opt-out
	// would let the repository default start recording the workflow instead.
	got := compileRoutingLearning(t, routingLearningSource(" workflow_dispatch", "imports: [../prompts/triage.md]\n"), `{"model_routing":{"learning":"observe"}}`, map[string]string{
		".github/prompts/triage.md": "---\nadaptive-routing: false\n---\nTriage an issue.\n",
	})
	require.ErrorContains(t, got.err, "adaptive-routing is no longer supported; set learning under model-routing instead")
}

func TestRepoMemoryCannotWriteTheRoutingHistories(t *testing.T) {
	for _, memory := range []string{
		"  repo-memory:\n    branch-name: memory/adaptive-routing/triage\n",
		// The root itself would block every history branch.
		"  repo-memory:\n    branch-name: memory/adaptive-routing\n",
	} {
		source := strings.Replace(routingLearningSource(" workflow_dispatch", ""), "  repo-memory: true\n", memory, 1)
		require.ErrorContains(t, compileRoutingLearning(t, source, "", nil).err, "memory/adaptive-routing/ is reserved for adaptive routing histories")
	}
}

func TestLoadRepoConfig_RoutingLearning(t *testing.T) {
	root := t.TempDir()
	writeAWJSON(t, root, `{"model_routing": {"learning": "observe"}}`)
	config, err := LoadRepoConfig(root)
	require.NoError(t, err)
	assert.Equal(t, "observe", config.RoutingLearning)

	writeAWJSON(t, root, `{"model_routing": {"learning": false}}`)
	config, err = LoadRepoConfig(root)
	require.NoError(t, err)
	assert.Empty(t, config.RoutingLearning)

	for _, invalid := range []string{`{"adaptive_routing":"observe"}`, `{"model_routing":{"learning":true}}`, `{"model_routing":{"learning":"live"}}`, `{"model_routing":{"allowed-models":["small"]}}`} {
		writeAWJSON(t, root, invalid)
		_, err = LoadRepoConfig(root)
		require.Error(t, err, invalid)
	}
}

func TestRoutingLearningDryRunOmitsWriterJob(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, ".github", "workflows", "triage.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(workflowPath), 0o755))
	require.NoError(t, os.WriteFile(workflowPath, []byte(routingLearningSource(" workflow_dispatch", "model-routing:\n  learning: observe\n")), 0o600))

	compiler := NewCompiler(WithNoEmit(true))
	compiler.gitRoot = root
	for _, dryRun := range []bool{true, false} {
		compiler.SetDryRun(dryRun)
		var (
			data *WorkflowData
			lock string
			err  error
		)
		testutil.CaptureStderr(t, func() {
			data, err = compiler.ParseWorkflowFile(workflowPath)
			if err == nil {
				lock, err = compiler.CompileToYAML(data, workflowPath)
			}
		})
		require.NoError(t, err)
		assert.Equal(t, "observe", data.RoutingLearningMode, "dry-run compilation must not mutate parsed WorkflowData")
		jobs := lockJobs(t, lock)
		if dryRun {
			assert.NotContains(t, jobs, pushRoutingHistoryJobName, "dry-run compilation must omit push_routing_history rather than leaving a skipped job")
			assert.NotContains(t, lock, pushRoutingHistoryJobName)
		} else {
			assert.Contains(t, jobs, pushRoutingHistoryJobName, "normal compilation after dry-run must restore push_routing_history")
		}
	}

	// Repository-default opt-in via aw.json is also omitted under --dry-run.
	defaultRoot := t.TempDir()
	writeAWJSON(t, defaultRoot, `{"model_routing": {"learning": "observe"}}`)
	defaultPath := filepath.Join(defaultRoot, ".github", "workflows", "triage.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(defaultPath), 0o755))
	require.NoError(t, os.WriteFile(defaultPath, []byte(routingLearningSource(" workflow_dispatch", "")), 0o600))
	defaultCompiler := NewCompiler(WithNoEmit(true))
	defaultCompiler.gitRoot = defaultRoot
	defaultCompiler.SetDryRun(true)
	var (
		defaultData *WorkflowData
		defaultLock string
		err         error
	)
	testutil.CaptureStderr(t, func() {
		defaultData, err = defaultCompiler.ParseWorkflowFile(defaultPath)
		if err == nil {
			defaultLock, err = defaultCompiler.CompileToYAML(defaultData, defaultPath)
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "observe", defaultData.RoutingLearningMode)
	assert.NotContains(t, lockJobs(t, defaultLock), pushRoutingHistoryJobName)

	// Validation of ineligible explicit learning settings still runs under --dry-run.
	invalidPath := filepath.Join(root, ".github", "workflows", "invalid.md")
	invalidSource := strings.Replace(routingLearningSource(" workflow_dispatch", "model-routing:\n  learning: observe\n"), "tools:\n  repo-memory: true\n", "", 1)
	require.NoError(t, os.WriteFile(invalidPath, []byte(invalidSource), 0o600))
	compiler.SetDryRun(true)
	testutil.CaptureStderr(t, func() {
		_, err = compiler.ParseWorkflowFile(invalidPath)
	})
	require.ErrorContains(t, err, "tools.repo-memory is required")
}

func TestPushRoutingHistoryOTLPConclusionWiring(t *testing.T) {
	compileMode := func(t *testing.T, mode ActionMode) map[string]any {
		t.Helper()
		root := t.TempDir()
		workflowPath := filepath.Join(root, ".github", "workflows", "triage.md")
		require.NoError(t, os.MkdirAll(filepath.Dir(workflowPath), 0o755))
		require.NoError(t, os.WriteFile(workflowPath, []byte(routingLearningSource(" workflow_dispatch", "model-routing:\n  learning: observe\n")), 0o600))
		compiler := NewCompiler(WithNoEmit(true), WithVersion("1.0.0"))
		compiler.gitRoot = root
		compiler.SetActionMode(mode)
		var (
			data *WorkflowData
			lock string
			err  error
		)
		testutil.CaptureStderr(t, func() {
			data, err = compiler.ParseWorkflowFile(workflowPath)
			if err == nil {
				lock, err = compiler.CompileToYAML(data, workflowPath)
			}
		})
		require.NoError(t, err)
		writer, ok := lockJobs(t, lock)[pushRoutingHistoryJobName]
		require.True(t, ok, "push_routing_history job must be present")
		return writer
	}

	findStep := func(t *testing.T, steps []any, name string) (int, map[string]any) {
		t.Helper()
		for i, raw := range steps {
			step := raw.(map[string]any)
			if step["name"] == name {
				return i, step
			}
		}
		return -1, nil
	}

	t.Run("script mode appends Clean Scripts with job status", func(t *testing.T) {
		writer := compileMode(t, ActionModeScript)
		steps := writer["steps"].([]any)

		downloadIdx, download := findStep(t, steps, "Download usage artifact")
		require.GreaterOrEqual(t, downloadIdx, 0)
		assert.Nil(t, download["continue-on-error"], "a failed usage artifact download must fail the job")

		pushIdx, push := findStep(t, steps, "Record the run in the routing history")
		require.Greater(t, pushIdx, downloadIdx)
		assert.Nil(t, push["if"], "push step must be skipped when artifact download fails")

		cleanIdx, clean := findStep(t, steps, "Clean Scripts")
		require.Equal(t, len(steps)-1, cleanIdx, "Clean Scripts must be the final step")
		assert.Equal(t, "always()", clean["if"])
		assert.Contains(t, clean["run"], "actions/setup/clean.sh")
		cleanEnv := clean["env"].(map[string]any)
		assert.Equal(t, "${{ github.job }}", cleanEnv["INPUT_JOB_NAME"])
		assert.Equal(t, "${{ job.status }}", cleanEnv["GH_AW_JOB_RESULT"])
		assert.Equal(t, "copilot", cleanEnv["GH_AW_INFO_ENGINE_ID"])

		recordIdx, _ := findStep(t, steps, "Record job result for OTLP")
		assert.Equal(t, -1, recordIdx, "script mode must not emit action-mode job-result export")
	})

	for _, mode := range []ActionMode{ActionModeDev, ActionModeRelease, ActionModeAction} {
		t.Run("action mode "+mode.String()+" passes job status to post-hook without duplicate conclusion emitter", func(t *testing.T) {
			writer := compileMode(t, mode)
			steps := writer["steps"].([]any)

			setupIdx, setup := findStep(t, steps, "Setup Scripts")
			require.GreaterOrEqual(t, setupIdx, 0)
			assert.NotEmpty(t, setup["uses"], "action mode must use the setup action so its post-hook runs")

			downloadIdx, download := findStep(t, steps, "Download usage artifact")
			require.Greater(t, downloadIdx, setupIdx)
			assert.Nil(t, download["continue-on-error"], "a failed usage artifact download must fail the job")

			pushIdx, push := findStep(t, steps, "Record the run in the routing history")
			require.Greater(t, pushIdx, downloadIdx)
			assert.Nil(t, push["if"], "push step must be skipped when artifact download fails")

			recordIdx, record := findStep(t, steps, "Record job result for OTLP")
			require.Equal(t, len(steps)-1, recordIdx, "Record job result for OTLP must be the final step")
			assert.Equal(t, "always()", record["if"])
			assert.Equal(t, true, record["continue-on-error"])
			assert.Equal(t, "bash", record["shell"])
			recordEnv := record["env"].(map[string]any)
			assert.Equal(t, "${{ job.status }}", recordEnv["GH_AW_JOB_RESULT"])
			assert.Contains(t, record["run"], `echo "GH_AW_JOB_RESULT=${GH_AW_JOB_RESULT}" >> "$GITHUB_ENV"`)
			assert.NotContains(t, record["run"], "action_conclusion_otlp.cjs", "must rely on setup post-hook rather than emitting a duplicate conclusion span")

			if mode == ActionModeDev {
				restoreIdx, _ := findStep(t, steps, "Restore actions folder")
				require.Greater(t, restoreIdx, pushIdx)
				assert.Greater(t, recordIdx, restoreIdx, "job result export must run after Restore actions folder in dev mode")
			}

			cleanIdx, _ := findStep(t, steps, "Clean Scripts")
			assert.Equal(t, -1, cleanIdx, "action mode must not emit script-mode Clean Scripts")
		})
	}
}
