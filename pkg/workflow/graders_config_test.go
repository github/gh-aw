package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func graderEnvironmentPayload(yaml, prefix string) []byte {
	var payload strings.Builder
	pattern := regexp.MustCompile(prefix + `_\d+: '((?:[^']|'')*)'`)
	for _, match := range pattern.FindAllStringSubmatch(yaml, -1) {
		payload.WriteString(strings.ReplaceAll(match[1], "''", "'"))
	}
	return []byte(payload.String())
}

func TestParseGradersDefaults(t *testing.T) {
	for _, frontmatter := range []map[string]any{{}, {"graders": nil}, {"graders": map[string]any{}}} {
		cfg, err := ParseGradersFromFrontmatter(frontmatter)
		require.NoError(t, err)
		require.True(t, cfg.HasGraders())
		assert.Equal(t, DefaultGraderIDs, cfg.EnabledGraderIDs())
		assert.NotContains(t, cfg.EnabledGraderIDs(), "recurrence-determinism")
		assert.NotContains(t, cfg.EnabledGraderIDs(), "trajectory-ndtw")
	}
}

func TestFailureGradersRegistryAndOverrides(t *testing.T) {
	ids := []string{"progress-stagnation", "ineffective-retries", "premature-termination", "completion-misrepresentation"}
	cfg, err := ParseGradersFromFrontmatter(map[string]any{})
	require.NoError(t, err)
	for _, id := range ids {
		assert.Contains(t, DefaultGraderIDs, id)
		assert.Contains(t, BuiltinGraderIDs, id)
		definition := cfg.Graders[id]
		require.NotNil(t, definition)
		assert.Equal(t, "count", definition.Unit)
		assert.Equal(t, "lower_is_better", definition.Direction)
		require.NotNil(t, definition.Threshold)
		assert.Zero(t, *definition.Threshold)
		override, err := ParseGradersFromFrontmatter(map[string]any{
			"graders": map[string]any{id: map[string]any{"threshold": 2.0, "config": map[string]any{"window": 4}}},
		})
		require.NoError(t, err)
		assert.InDelta(t, 2, *override.Graders[id].Threshold, 0)
		assert.Equal(t, 4, override.Graders[id].Config["window"])
		assert.Empty(t, override.CustomGraderIDs())
	}
}

func TestParseGradersOverrides(t *testing.T) {
	cfg, err := ParseGradersFromFrontmatter(map[string]any{
		"graders": map[string]any{
			"loops":                  map[string]any{"enabled": false},
			"tool-success-rate":      map[string]any{"threshold": 0.95},
			"recurrence-determinism": nil,
		},
	})
	require.NoError(t, err)
	assert.NotContains(t, cfg.EnabledGraderIDs(), "loops")
	assert.Contains(t, cfg.EnabledGraderIDs(), "recurrence-determinism")
	assert.InDelta(t, 0.95, *cfg.Graders["tool-success-rate"].Threshold, 1e-9)
	assert.Len(t, cfg.EnabledGraderIDs(), len(DefaultGraderIDs))
}

func TestParseGradersRejectsExecutableConfiguration(t *testing.T) {
	for _, graders := range []map[string]any{
		{"custom": nil},
		{"custom": map[string]any{"enabled": false}},
		{"operational-value": map[string]any{"run": "../evaluator.sh"}},
		{"operational-value": map[string]any{"script": "#!/bin/bash\necho 1"}},
		{"loops": map[string]any{"script": "return 1"}},
		{"loops": map[string]any{"run": "evaluator.sh"}},
		{"loops": map[string]any{"unsupported": true}},
	} {
		_, err := ParseGradersFromFrontmatter(map[string]any{"graders": graders})
		require.Error(t, err)
	}
}

func TestParseOperationalValueShellGrader(t *testing.T) {
	cfg, err := ParseGradersFromFrontmatter(map[string]any{"graders": map[string]any{
		"operational-value": map[string]any{"run": ".github/graders/score.sh"},
	}})
	require.NoError(t, err)
	assert.Equal(t, []string{"operational-value"}, cfg.CustomGraderIDs())
	assert.Equal(t, "higher_is_better", cfg.Graders["operational-value"].Direction)
	builtin, err := graderEnvironment(cfg)
	require.NoError(t, err)
	assert.Contains(t, builtin, `"customGraders":["operational-value"]`)
	assert.NotContains(t, builtin, "score.sh")
	assert.NotContains(t, builtin, "shellScript")
}

func TestParseGradersInvalidFields(t *testing.T) {
	for _, value := range []any{
		"loops",
		map[string]any{"bad id": nil},
		map[string]any{"loops": true},
		map[string]any{"loops": map[string]any{"enabled": "yes"}},
		map[string]any{"loops": map[string]any{"threshold": "high"}},
		map[string]any{"loops": map[string]any{"config": true}},
		map[string]any{"loops": map[string]any{"direction": "up"}},
		map[string]any{"loops": nil, " loops ": nil},
	} {
		_, err := ParseGradersFromFrontmatter(map[string]any{"graders": value})
		require.Error(t, err)
	}
}

func TestGradersCanAllBeDisabled(t *testing.T) {
	entries := map[string]any{}
	for _, id := range DefaultGraderIDs {
		entries[id] = map[string]any{"enabled": false}
	}
	cfg, err := ParseGradersFromFrontmatter(map[string]any{"graders": entries})
	require.NoError(t, err)
	assert.False(t, cfg.HasGraders())
	assert.Empty(t, cfg.EnabledGraderIDs())
}

func TestBuiltinGraderRegistryMatchesRuntime(t *testing.T) {
	content, err := os.ReadFile("../../actions/setup/js/builtin_graders.json")
	require.NoError(t, err)
	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(content, &metadata))
	require.Len(t, metadata, len(BuiltinGraderRegistry))
	for _, meta := range BuiltinGraderRegistry {
		var actual struct {
			Name        string
			Description string
			Unit        string
			Direction   string
			Threshold   *float64
			Min         *float64
			Max         *float64
		}
		require.NoError(t, json.Unmarshal(metadata[meta.ID], &actual))
		assert.Equal(t, meta.Name, actual.Name)
		assert.Equal(t, meta.Description, actual.Description)
		assert.Equal(t, meta.Unit, actual.Unit)
		assert.Equal(t, meta.Direction, actual.Direction)
		assert.Equal(t, meta.Threshold, actual.Threshold)
		assert.Equal(t, meta.Min, actual.Min)
		assert.Equal(t, meta.Max, actual.Max)
	}
}

func TestGraderEnvironmentRoundTrip(t *testing.T) {
	cfg, err := ParseGradersFromFrontmatter(map[string]any{"graders": map[string]any{
		"loops": map[string]any{"name": "'${{ secrets.DO_NOT_EVALUATE }}", "config": map[string]any{"data": strings.Repeat("x", 6000)}},
	}})
	require.NoError(t, err)
	env, err := graderEnvironment(cfg)
	require.NoError(t, err)
	assert.NotContains(t, env, "${{")
	var actual graderManifest
	require.NoError(t, json.Unmarshal(graderEnvironmentPayload(env, "GH_AW_GRADER_MANIFEST"), &actual))
	assert.Equal(t, buildGraderManifest(cfg, false), &actual)
	assert.NotContains(t, env, "GH_AW_GRADER_EXEC")
}

func TestGraderDefaultEnvironmentIsCompact(t *testing.T) {
	cfg, err := ParseGradersFromFrontmatter(map[string]any{})
	require.NoError(t, err)
	env, err := graderEnvironment(cfg)
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":2,"graders":[]}`, string(graderEnvironmentPayload(env, "GH_AW_GRADER_MANIFEST")))
}

func TestGradersRunOnlyInConclusion(t *testing.T) {
	for _, config := range []string{"", "graders: {}\n", "graders:\n  loops:\n    threshold: 8\n"} {
		t.Run(config, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "test.md")
			require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nengine: copilot\nstrict: false\n"+config+"---\nTest.\n"), 0o600))
			compiler := NewCompiler(WithVersion("dev"))
			require.NoError(t, compiler.CompileWorkflow(file))
			content, err := os.ReadFile(stringutil.MarkdownToLockFile(file))
			require.NoError(t, err)
			agent := extractJobSection(string(content), "agent")
			conclusion := extractJobSection(string(content), "conclusion")
			assert.NotContains(t, agent, "GRADER")
			assert.NotContains(t, agent, "/graders/")
			assert.Contains(t, conclusion, "GH_AW_GRADER_MANIFEST")
			assert.Contains(t, conclusion, "generate_usage_artifacts.cjs")
			assert.Less(t, strings.Index(conclusion, "Generate usage activity summary and unified session"), strings.Index(conclusion, "Upload usage artifact"))
			assert.Contains(t, conclusion, "/usage/graders/grader_results.json")
			assert.NotContains(t, string(content), "GH_AW_GRADER_EXEC")
		})
	}
}

func TestMergeImportedGradersFrontmatter(t *testing.T) {
	merged, err := mergeImportedGradersFrontmatter(map[string]any{"graders": map[string]any{
		"loops": map[string]any{"threshold": 8},
	}}, `{"loops":{"threshold":2,"enabled":true},"recurrence-determinism":{}}`)
	require.NoError(t, err)
	cfg, err := ParseGradersFromFrontmatter(merged)
	require.NoError(t, err)
	assert.InDelta(t, 8.0, *cfg.Graders["loops"].Threshold, 1e-9)
	assert.Contains(t, cfg.EnabledGraderIDs(), "recurrence-determinism")
}

func TestCustomGraderManifestPartition(t *testing.T) {
	cfg, err := ParseGradersFromFrontmatter(map[string]any{"graders": map[string]any{
		"custom-z": map[string]any{"script": "return '${{ secrets.DO_NOT_EVALUATE }}'.length"},
		"custom-a": map[string]any{"script": "return 1"},
		"disabled": map[string]any{"script": "throw Error('disabled')", "enabled": false},
	}})
	require.NoError(t, err)
	assert.Equal(t, []string{"custom-a", "custom-z"}, cfg.CustomGraderIDs())
	builtin, err := graderEnvironment(cfg)
	require.NoError(t, err)
	assert.NotContains(t, builtin, "return")
	assert.NotContains(t, builtin, "script")
	assert.Contains(t, builtin, `"customGraders":["custom-a","custom-z"]`)
	custom, err := graderEnvironmentForJob(cfg, true)
	require.NoError(t, err)
	assert.NotContains(t, custom, "${{")
	assert.Contains(t, custom, "GH_AW_CUSTOM_GRADER_MANIFEST")
	assert.Len(t, buildGraderManifest(cfg, true).Graders, 2)
}

func TestCustomGradersRunAfterConclusionWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.md")
	const source = "return helpers.createSessionLookups(session).stats.toolCallCount"
	require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nengine: copilot\nstrict: false\nenvironment: production\ngraders:\n  custom-count:\n    script: "+source+"\n---\nTest.\n"), 0o600))
	compiler := NewCompiler(WithVersion("dev"))
	compiler.SetEnvironmentOverride("debug")
	require.NoError(t, compiler.CompileWorkflow(file))
	content, err := os.ReadFile(stringutil.MarkdownToLockFile(file))
	require.NoError(t, err)
	conclusion := extractJobSection(string(content), "conclusion")
	custom := extractJobSection(string(content), "graders")
	assert.NotContains(t, conclusion, source)
	assert.NotContains(t, conclusion, "custom_graders.cjs")
	assert.NotContains(t, conclusion, "GH_AW_CUSTOM_GRADER")
	assert.Contains(t, conclusion, `"customGraders":["custom-count"]`)
	assert.Contains(t, custom, source)
	assert.Contains(t, custom, "custom_graders.cjs")
	assert.Contains(t, custom, "needs:\n      - activation\n      - conclusion")
	assert.NotContains(t, custom, "        permissions:")
	assert.Contains(t, custom, "contents: read")
	assert.Contains(t, custom, "runs-on: ubuntu-latest")
	assert.Contains(t, custom, "Download unified session")
	assert.Contains(t, custom, "Upload custom grader results")
	for _, forbidden := range []string{"id-token:", "write", "environment:", "COPILOT_GITHUB_TOKEN", "GH_AW_OTLP", "actions/create-github-app-token", "persist-credentials: true"} {
		assert.NotContains(t, custom, forbidden)
	}

	assert.NotContains(t, conclusion, "needs: [graders")
	job, ok := compiler.jobManager.GetJob("conclusion")
	require.True(t, ok)
	assert.NotContains(t, job.Needs, "graders")
	assert.Contains(t, conclusion, "environment: \"debug\"")
}

func TestCustomGradersJobCannotBeCustomized(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.md")
	const content = "---\non: workflow_dispatch\nstrict: false\ngraders:\n  custom-count:\n    script: return 1\njobs:\n  graders:\n    permissions:\n      issues: write\n---\nTest.\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	require.ErrorContains(t, NewCompiler(WithVersion("dev")).CompileWorkflow(file), "jobs.graders is compiler-owned")
}

func TestConclusionRejectsUserProvidedSteps(t *testing.T) {
	for _, field := range []string{"steps", "pre-steps", "setup-steps"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "test.md")
			content := "---\non: workflow_dispatch\nstrict: false\njobs:\n  conclusion:\n    " + field + ":\n      - run: node -e 'throw Error(\"must not run\")'\n---\nTest.\n"
			require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
			require.ErrorContains(t, NewCompiler(WithVersion("dev")).CompileWorkflow(file), "jobs.conclusion cannot run user-provided steps")
		})
	}
}
