//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDryRunCreditLimits(t *testing.T) {
	for _, tc := range []struct {
		name, daily, expression string
		explicit                bool
		perRun, expected        int64
	}{
		{name: "explicit-daily", daily: "25", explicit: true, expected: 25},
		{name: "existing-per-run", daily: "25", explicit: true, perRun: 10, expected: 10},
		{name: "disabled-per-run", daily: "25", explicit: true, perRun: -1, expected: 25},
		{name: "daily-expression", daily: "${{ vars.DAILY_BUDGET }}", explicit: true},
		{name: "existing-expression", daily: "25", explicit: true, expression: "${{ vars.RUN_BUDGET }}"},
		{name: "runtime-default", daily: "${{ vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS || '10000' }}"},
		{name: "default-bounds-disabled-run", daily: "${{ vars.GH_AW_DEFAULT_MAX_DAILY_AI_CREDITS || '10000' }}", perRun: -1, expected: constants.DefaultMaxAICredits},
		{name: "disabled-daily", perRun: -1, expected: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var daily *string
			if tc.daily != "" {
				daily = &tc.daily
			}
			parsedDaily := TemplatableInt32(tc.daily)
			data := &WorkflowData{
				MaxDailyAICredits: daily, MaxDailyAICreditsExplicit: tc.explicit,
				MaxDailyAICBackend: "repo-memory", MaxDailyAICContinueOnError: true,
				MaxDailyAICreditsGitHubApp: &GitHubAppConfig{AppID: "daily-only"},
				EngineConfig:               &EngineConfig{MaxAICredits: tc.perRun},
				RawFrontmatter:             map[string]any{maxDailyAICreditsField: tc.daily},
				ParsedFrontmatter:          &FrontmatterConfig{MaxDailyAICredits: &parsedDaily},
			}
			if tc.expression != "" {
				data.RawFrontmatter["max-ai-credits"] = tc.expression
			}
			compiler := NewCompiler()
			compiler.SetDryRun(true)
			result := compiler.dryRunWorkflowData(data)
			assert.Nil(t, result.MaxDailyAICredits)
			assert.False(t, result.MaxDailyAICreditsExplicit)
			assert.Empty(t, result.MaxDailyAICBackend)
			assert.Nil(t, result.MaxDailyAICreditsGitHubApp)
			assert.False(t, result.MaxDailyAICContinueOnError)
			assert.NotContains(t, result.RawFrontmatter, maxDailyAICreditsField)
			assert.Nil(t, result.ParsedFrontmatter.MaxDailyAICredits)
			assert.False(t, hasMaxDailyAICGuardrail(result))
			assert.Equal(t, tc.expected, result.EngineConfig.MaxAICredits)
			env := make(map[string]string)
			applyDefaultMaxAICreditsEnvToMap(env, result)
			if tc.expression != "" {
				assert.Equal(t, tc.expression, env[awfMaxAICreditsVarName])
			} else if isExpression(tc.daily) && tc.explicit {
				assert.Equal(t, tc.daily, env[awfMaxAICreditsVarName])
			}
			assert.Same(t, daily, data.MaxDailyAICredits)
			assert.Equal(t, tc.perRun, data.EngineConfig.MaxAICredits)
			assert.Contains(t, data.RawFrontmatter, maxDailyAICreditsField)
			assert.Same(t, &parsedDaily, data.ParsedFrontmatter.MaxDailyAICredits)
			assert.NotNil(t, data.MaxDailyAICreditsGitHubApp)
			compiler.SetDryRun(false)
			assert.Same(t, data, compiler.dryRunWorkflowData(data))
		})
	}
}

func TestDryRunCompiledCreditLimits(t *testing.T) {
	for _, tc := range []struct {
		name, config, expected string
	}{
		{name: "defaults", expected: "GH_AW_DEFAULT_MAX_AI_CREDITS"},
		{name: "daily", config: "max-daily-ai-credits: 25\n", expected: `"maxAiCredits":25`},
		{name: "daily-suffix", config: "max-daily-ai-credits: 1k\n", expected: `"maxAiCredits":1000`},
		{name: "both", config: "max-daily-ai-credits: 25\nmax-ai-credits: 10\n", expected: `"maxAiCredits":10`},
		{name: "disabled-per-run", config: "max-daily-ai-credits: 25\nmax-ai-credits: -1\n", expected: `"maxAiCredits":25`},
		{name: "expression", config: "max-daily-ai-credits: ${{ vars.DAILY_BUDGET }}\n", expected: "GH_AW_MAX_AI_CREDITS: ${{ vars.DAILY_BUDGET }}"},
		{name: "imported", config: "imports: [shared.md]\n", expected: `"maxAiCredits":25`},
		{name: "github-app", config: "max-daily-ai-credits:\n  value: 25\n  github-app:\n    client-id: ${{ vars.DAILY_APP_ID }}\n    private-key: ${{ secrets.DAILY_ONLY_KEY }}\n", expected: `"maxAiCredits":25`},
		{name: "repo-memory", config: "max-daily-ai-credits:\n  value: 25\n  backend: repo-memory\ntools:\n  repo-memory: true\n", expected: `"maxAiCredits":25`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\nmax-daily-ai-credits: 25\n---\n"), 0600))
			path := filepath.Join(dir, "credits.md")
			source := "---\non: workflow_dispatch\nengine: copilot\nstrict: false\npermissions:\n  contents: read\n" + tc.config + "---\nBounded diagnostic.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0600))
			compiler := NewCompiler()
			compiler.SetSkipValidation(true)
			compiler.SetApprove(true)
			var normal string
			for _, dryRun := range []bool{false, true, false} {
				compiler.SetDryRun(dryRun)
				require.NoError(t, compiler.CompileWorkflow(path))
				content, err := os.ReadFile(filepath.Join(dir, "credits.lock.yml"))
				require.NoError(t, err)
				compiled := string(content)
				if dryRun {
					assert.Contains(t, compiled, tc.expected)
					for _, removed := range []string{"GH_AW_MAX_DAILY_AI_CREDITS", "daily-ai-credits-workflow-guardrail", "daily-aic-app-token", "daily_aic_workflow_guardrail.cjs", "Append daily AIC repo-memory ledger", "DAILY_ONLY_KEY"} {
						assert.NotContains(t, compiled, removed)
					}
				} else {
					assert.Contains(t, compiled, "daily-ai-credits-workflow-guardrail")
					if normal == "" {
						normal = compiled
					} else {
						assert.Equal(t, normal, compiled)
					}
				}
			}
		})
	}
}
