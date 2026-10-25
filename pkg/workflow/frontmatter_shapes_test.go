//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatterConfig_RegressionPublicSweep(t *testing.T) {
	// The sweep at github/gh-aw@aa89ad15310c2bfd983aaee1ef6e1268e4508e81
	// found this daily-limit shape in daily-cli-performance, daily-code-metrics,
	// and daily-community-attribution. Keep the reproducer to the observed fields.
	source := `---
on: workflow_dispatch
max-daily-ai-credits:
  value: 10000
  backend: repo-memory
---
`
	result, err := parser.ExtractFrontmatterFromContent(source)
	require.NoError(t, err)
	frontmatter := result.Frontmatter
	require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "daily-cli-performance.md"))
	before, err := json.Marshal(frontmatter)
	require.NoError(t, err)

	compiler := NewCompiler()
	stderr := testutil.CaptureStderr(t, func() {
		config := compiler.tryParseFrontmatterConfig(frontmatter)
		require.NotNil(t, config)
		require.NotNil(t, config.MaxDailyAICredits)
		assert.Equal(t, "10000", config.MaxDailyAICredits.String())
	})
	assert.Zero(t, compiler.GetWarningCount())
	assert.NotContains(t, stderr, "Failed to parse frontmatter config")
	assert.Equal(t, "repo-memory", resolveMaxDailyAICBackend(frontmatter, ""))
	after, err := json.Marshal(frontmatter)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
}

func TestParseFrontmatterConfigSupportedShapes(t *testing.T) {
	for _, needs := range []any{"activation", []any{"activation", "agent"}} {
		t.Run(fmt.Sprintf("safe job needs %v", needs), func(t *testing.T) {
			frontmatter := map[string]any{
				"on": "workflow_dispatch",
				"safe-outputs": map[string]any{
					"jobs": map[string]any{
						"publish-analysis": map[string]any{
							"needs": needs,
							"steps": []any{map[string]any{"run": "echo analysis"}},
						},
					},
				},
			}
			before, err := json.Marshal(frontmatter)
			require.NoError(t, err)
			require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "shapes.md"))
			config, err := ParseFrontmatterConfig(frontmatter)
			require.NoError(t, err)
			require.NotNil(t, config.SafeOutputs)
			require.NotNil(t, config.SafeOutputs.Jobs["publish-analysis"])
			expected := []string{"activation"}
			if _, array := needs.([]any); array {
				expected = append(expected, "agent")
			}
			assert.Equal(t, expected, config.SafeOutputs.Jobs["publish-analysis"].Needs)
			after, err := json.Marshal(frontmatter)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after))
		})
	}

	cache := map[string]any{"key": "analysis-cache", "path": []any{".cache", "dist"}}
	for _, value := range []any{cache, []any{cache}, []any{}} {
		t.Run(fmt.Sprintf("cache %T", value), func(t *testing.T) {
			frontmatter := map[string]any{"on": "workflow_dispatch", "cache": value}
			require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "shapes.md"))
			config, err := ParseFrontmatterConfig(frontmatter)
			require.NoError(t, err)
			expected, err := json.Marshal(value)
			require.NoError(t, err)
			actual, err := json.Marshal(config.Cache)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(actual))
			roundTrip, err := ParseFrontmatterConfig(config.ToMap())
			require.NoError(t, err)
			assert.Equal(t, config.Cache, roundTrip.Cache)
		})
	}
}

func TestParseFrontmatterConfigAICreditShapes(t *testing.T) {
	for _, field := range []string{"max-ai-credits", maxDailyAICreditsField} {
		for _, tc := range []struct {
			value any
			want  string
		}{
			{10000, "10000"},
			{"20K", "20000"},
			{"2m", "2000000"},
			{"10000", "10000"},
			{int64(3000000000), "3000000000"},
			{"3000M", "3000000000"},
			{-1, "-1"},
			{"-1", "-1"},
			{"${{ inputs.budget }}", "${{ inputs.budget }}"},
		} {
			t.Run(fmt.Sprintf("%s/%v", field, tc.value), func(t *testing.T) {
				frontmatter := map[string]any{"on": "workflow_dispatch", field: tc.value}
				require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "shapes.md"))
				config, err := ParseFrontmatterConfig(frontmatter)
				require.NoError(t, err)
				limit := config.MaxAICredits
				if field == maxDailyAICreditsField {
					limit = config.MaxDailyAICredits
				}
				require.NotNil(t, limit)
				assert.Equal(t, tc.want, limit.String())
			})
		}
	}

	for _, value := range []any{10000, "20K", "${{ inputs.budget }}"} {
		t.Run(fmt.Sprintf("daily object/%v", value), func(t *testing.T) {
			daily := map[string]any{
				"value": value, "backend": "repo-memory", "continue-on-error": true,
				"github-app": map[string]any{"client-id": "${{ vars.APP_ID }}", "private-key": "${{ secrets.APP_KEY }}"},
			}
			frontmatter := map[string]any{"on": "workflow_dispatch", maxDailyAICreditsField: daily}
			before, err := json.Marshal(frontmatter)
			require.NoError(t, err)
			require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "shapes.md"))
			config, err := ParseFrontmatterConfig(frontmatter)
			require.NoError(t, err)
			require.NotNil(t, config.MaxDailyAICredits)
			want := parseMaxDailyAICValue(value)
			require.NotNil(t, want)
			assert.Equal(t, *want, config.MaxDailyAICredits.String())
			assert.Equal(t, "repo-memory", resolveMaxDailyAICBackend(frontmatter, ""))
			assert.True(t, resolveMaxDailyAICContinueOnError(frontmatter, ""))
			require.NotNil(t, extractMaxDailyAICGitHubApp(frontmatter))
			after, err := json.Marshal(frontmatter)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after))
		})
	}
}

func TestParseFrontmatterConfigInvalidShapes(t *testing.T) {
	for _, tc := range []struct {
		field string
		value any
	}{
		{"cache", false},
		{"cache", "cache"},
		{"cache", []any{42}},
		{"safe-outputs", map[string]any{"jobs": map[string]any{"publish": map[string]any{"needs": true}}}},
		{"safe-outputs", map[string]any{"jobs": map[string]any{"publish": map[string]any{"needs": []any{42}}}}},
		{"max-ai-credits", "20G"},
		{"max-ai-credits", 0},
		{"max-ai-credits", -2},
		{"max-ai-credits", 1.5},
		{"max-ai-credits", true},
		{"max-ai-credits", map[string]any{"value": 100}},
		{maxDailyAICreditsField, map[string]any{"backend": "repo-memory"}},
		{maxDailyAICreditsField, map[string]any{"value": -1}},
		{maxDailyAICreditsField, map[string]any{"value": map[string]any{"value": 100}}},
		{"timeout-minutes", "20K"},
	} {
		t.Run(fmt.Sprintf("%s/%v", tc.field, tc.value), func(t *testing.T) {
			compiler := NewCompiler()
			stderr := testutil.CaptureStderr(t, func() {
				config := compiler.tryParseFrontmatterConfig(map[string]any{tc.field: tc.value})
				assert.Nil(t, config)
			})
			assert.Equal(t, 1, compiler.GetWarningCount())
			assert.Contains(t, stderr, "Failed to parse frontmatter config")
		})
	}
}

func TestCompileFrontmatterSupportedShapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shapes.md")
	source := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
max-ai-credits: 20K
max-daily-ai-credits:
  value: 10000
  backend: repo-memory
  continue-on-error: true
tools:
  repo-memory: true
cache:
  - key: analysis-cache
    path: .cache
safe-outputs:
  jobs:
    publish-analysis:
      needs: agent
      steps:
        - run: echo analysis
excluded-env:
  - WORKFLOW_PRIVATE_HEADER
---
Analyze the repository.
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler()
	stderr := testutil.CaptureStderr(t, func() {
		require.NoError(t, compiler.CompileWorkflow(path))
	})
	assert.NotContains(t, stderr, "Failed to parse frontmatter config")
	lock, err := os.ReadFile(filepath.Join(dir, "shapes.lock.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(lock), "--exclude-env WORKFLOW_PRIVATE_HEADER")
	assert.Contains(t, string(lock), "analysis-cache")
	assert.Contains(t, string(lock), "publish_analysis:")
	assert.Contains(t, string(lock), `GH_AW_MAX_AI_CREDITS: "20000"`)
	assert.Contains(t, string(lock), `GH_AW_MAX_DAILY_AI_CREDITS: "10000"`)
}
