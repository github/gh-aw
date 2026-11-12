//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runtimeMaxAICreditsConfigScript returns the part of the compiled AWF step that
// resolves GH_AW_MAX_AI_CREDITS and writes the AWF config file.
func runtimeMaxAICreditsConfigScript(t *testing.T, data *WorkflowData) string {
	t.Helper()
	command := BuildAWFCommand(AWFCommandConfig{
		EngineName:                 "copilot",
		EngineCommand:              "copilot --prompt-file /tmp/prompt.txt",
		LogFile:                    "/tmp/gh-aw/agent-stdio.log",
		AllowedDomains:             "github.com",
		ResolveMaxAICreditsFromEnv: true,
		WorkflowData:               data,
	})
	start := strings.Index(command, awfMaxAICreditsVarName+"=")
	require.GreaterOrEqual(t, start, 0, "compiled step must assign GH_AW_MAX_AI_CREDITS")
	end := strings.Index(command[start:], "\ncp \""+awfConfigRuntimePathExpr+"\"")
	require.Positive(t, end, "compiled step must copy the AWF config after writing it")
	return command[start : start+end]
}

type runtimeMaxAICreditsResult struct {
	apiProxy map[string]any
	output   string
}

func runRuntimeMaxAICreditsScript(t *testing.T, script string, env ...string) runtimeMaxAICreditsResult {
	t.Helper()
	runnerTemp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runnerTemp, "gh-aw"), 0o755))
	cmd := exec.Command("bash", "-e", "-c", script)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "RUNNER_TEMP=" + runnerTemp}, env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	raw, err := os.ReadFile(filepath.Join(runnerTemp, "gh-aw", "awf-config.json"))
	require.NoError(t, err)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var config map[string]any
	require.NoError(t, decoder.Decode(&config), string(raw))
	apiProxy, ok := config["apiProxy"].(map[string]any)
	require.True(t, ok, "AWF config must contain apiProxy")
	return runtimeMaxAICreditsResult{apiProxy: apiProxy, output: string(out)}
}

// TestRuntimeMaxAICreditsNormalization_Shellcheck verifies the runtime normalization
// script passes ShellCheck, which gh aw compile --dry-run runs on generated steps.
func TestRuntimeMaxAICreditsNormalization_Shellcheck(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck is not installed")
	}
	script := buildRuntimeMaxAICreditsNormalization(&WorkflowData{}, "1000")
	cmd := exec.Command("shellcheck", "-s", "bash", "-")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "runtime max AI credits normalization must pass ShellCheck:\n%s", output)
}

// TestRuntimeMaxAICreditsVariableValues runs the compiled AWF step with runtime
// GH_AW_MAX_AI_CREDITS values (as resolved from vars.GH_AW_DEFAULT_*_MAX_AI_CREDITS)
// and verifies they are accepted exactly like frontmatter values. Covers T-AIC-PR-003.
func TestRuntimeMaxAICreditsVariableValues(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}
	runs := []struct {
		name          string
		data          *WorkflowData
		source        string
		defaultBudget string
	}{
		{"agent", &WorkflowData{}, "vars.GH_AW_DEFAULT_MAX_AI_CREDITS", "1000"},
		{"detection", &WorkflowData{IsDetectionRun: true}, "vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS", "400"},
		{"evals", &WorkflowData{IsEvalsRun: true}, "vars.GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS", "400"},
	}
	values := []struct {
		value string
		want  string // "" means maxAiCredits is omitted; "default" means the built-in default
	}{
		{"-1", ""},
		{" -1 ", ""},
		{"0", "default"},
		{"1500", "1500"},
		{"5K", "5000"},
		{"5k", "5000"},
		{"2M", "2000000"},
		{"2m", "2000000"},
		{" 7K ", "7000"},
		{"+010", "10"},
		{"9223372036854775807", "9223372036854775807"},
		{"9223372036854775808", "default"},
		{"9223372036854776K", "default"},
		{"0K", "default"},
		{"-5", "default"},
		{"-1K", "default"},
		{"1.5K", "default"},
		{"K", "default"},
		{"5 K", "default"},
		{"unlimited", "default"},
		{"$(echo s3cr3t)", "default"},
	}
	for _, run := range runs {
		run.data.EngineConfig = &EngineConfig{ID: "copilot"}
		run.data.NetworkPermissions = &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}
		script := runtimeMaxAICreditsConfigScript(t, run.data)
		for _, tc := range values {
			t.Run(run.name+"/"+tc.value, func(t *testing.T) {
				result := runRuntimeMaxAICreditsScript(t, script, awfMaxAICreditsVarName+"="+tc.value)
				want := tc.want
				if want == "default" {
					want = run.defaultBudget
				}

				// Runtime acceptance must match the compiler's frontmatter parser.
				parsed, ok := parseMaxAICLimitValue(tc.value)
				if ok {
					expected := strconv.FormatInt(parsed, 10)
					if parsed == -1 {
						expected = ""
					}
					assert.Equal(t, expected, want, "runtime value must expand like the frontmatter value")
				} else {
					assert.Equal(t, "default", tc.want, "values the compiler rejects must fall back to the default")
				}

				credits, present := result.apiProxy["maxAiCredits"]
				if want == "" {
					assert.False(t, present, "-1 must omit maxAiCredits from the AWF config")
					assert.Contains(t, result.output, "budget disabled at runtime")
				} else {
					assert.NotContains(t, result.output, "budget disabled at runtime")
					require.True(t, present, "maxAiCredits must be present")
					assert.Equal(t, json.Number(want), credits)
				}

				if tc.want == "default" {
					assert.Contains(t, result.output, "::warning::Ignoring invalid "+run.source+" (value withheld)")
					assert.Contains(t, result.output, "default budget of "+run.defaultBudget+" AI credits")
					assert.NotContains(t, result.output, "s3cr3t")
				} else {
					assert.NotContains(t, result.output, "::warning::")
				}
			})
		}
	}

	t.Run("unset variable uses the default without a warning", func(t *testing.T) {
		data := &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot"}, NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}}
		result := runRuntimeMaxAICreditsScript(t, runtimeMaxAICreditsConfigScript(t, data))
		assert.Equal(t, json.Number("1000"), result.apiProxy["maxAiCredits"])
		assert.NotContains(t, result.output, "::warning::")
	})

	t.Run("admission allowance overrides a runtime -1", func(t *testing.T) {
		data := &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot"}, NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}}
		result := runRuntimeMaxAICreditsScript(t, runtimeMaxAICreditsConfigScript(t, data),
			awfMaxAICreditsVarName+"=-1", "GH_AW_ADMISSION_RESULT=admitted", "GH_AW_ADMISSION_MAX_AI_CREDITS=111")
		assert.Equal(t, json.Number("111"), result.apiProxy["maxAiCredits"])
		assert.NotContains(t, result.output, "budget disabled at runtime", "the omit message must not appear when an admission allowance replaces -1")
	})

	t.Run("frontmatter -1 keeps the compiled literal", func(t *testing.T) {
		data := &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", MaxAICredits: -1}, NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}}
		script := runtimeMaxAICreditsConfigScript(t, data)
		assert.Contains(t, script, awfMaxAICreditsVarName+`="-1"`)
		assert.NotContains(t, script, "::warning::")
		result := runRuntimeMaxAICreditsScript(t, script, awfMaxAICreditsVarName+"=5K")
		assert.Equal(t, json.Number("-1"), result.apiProxy["maxAiCredits"])
	})
}

// TestRuntimeMaxAICreditsDryRunSourceLabel verifies the dry-run fallback warning names
// the frontmatter field the user configured, even when dry-run preparation promotes an
// expression-valued max-daily-ai-credits into the per-run budget.
func TestRuntimeMaxAICreditsDryRunSourceLabel(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}
	daily := "${{ vars.DAILY_BUDGET }}"
	for _, tc := range []struct {
		name, source string
		raw          map[string]any
	}{
		{"max-daily-ai-credits", maxDailyAICreditsField, map[string]any{maxDailyAICreditsField: daily}},
		{"max-ai-credits", "max-ai-credits", map[string]any{maxDailyAICreditsField: daily, "max-ai-credits": "${{ vars.RUN_BUDGET }}"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetDryRun(true)
			data := compiler.dryRunWorkflowData(&WorkflowData{
				MaxDailyAICredits:         &daily,
				MaxDailyAICreditsExplicit: true,
				EngineConfig:              &EngineConfig{ID: "copilot"},
				NetworkPermissions:        &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
				RawFrontmatter:            tc.raw,
			})
			assert.Equal(t, tc.source, data.DryRunMaxAICreditsSource)
			result := runRuntimeMaxAICreditsScript(t, runtimeMaxAICreditsConfigScript(t, data), awfMaxAICreditsVarName+"=unlimited")
			assert.Contains(t, result.output, "::warning::Ignoring invalid "+tc.source+" (value withheld)")
			assert.Equal(t, json.Number("1000"), result.apiProxy["maxAiCredits"])
		})
	}
}
