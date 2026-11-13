//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// TestRuntimeMaxAICreditsScript_Shellcheck verifies the generated budget resolution
// script passes ShellCheck, which gh aw compile --dry-run runs on generated steps.
func TestRuntimeMaxAICreditsScript_Shellcheck(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck is not installed")
	}
	data := &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot"}, NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}}
	script := runtimeMaxAICreditsConfigScript(t, data)
	cmd := exec.Command("shellcheck", "-s", "bash", "-")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "runtime max AI credits script must pass ShellCheck:\n%s", output)
}

// TestRuntimeMaxAICreditsVariableValues runs the compiled AWF step with runtime
// GH_AW_MAX_AI_CREDITS values (as resolved from vars.GH_AW_DEFAULT_*_MAX_AI_CREDITS).
// -1 and positive whole numbers pass through; anything else falls back to the
// built-in default with a value-withheld warning. Covers T-AIC-PR-003.
func TestRuntimeMaxAICreditsVariableValues(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}
	runs := []struct {
		name          string
		data          *WorkflowData
		defaultBudget string
	}{
		{"agent", &WorkflowData{}, "1000"},
		{"detection", &WorkflowData{IsDetectionRun: true}, "400"},
		{"evals", &WorkflowData{IsEvalsRun: true}, "400"},
	}
	values := []struct {
		value string
		want  string // "default" means the built-in default
	}{
		{"-1", "-1"},
		{"1", "1"},
		{"750", "750"},
		{"1500", "1500"},
		{"9223372036854775807", "9223372036854775807"},
		{"9223372036854775808", "default"},
		{"9999999999999999999", "default"},
		{"0", "default"},
		{"10000000000000000000", "default"},
		{"0100", "default"},
		{"5K", "default"},
		{"100M", "default"},
		{"-2", "default"},
		{" -1 ", "default"},
		{"abc", "default"},
		{"s3cr3t", "default"},
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

				credits, present := result.apiProxy["maxAiCredits"]
				require.True(t, present, "maxAiCredits must be present")
				assert.Equal(t, json.Number(want), credits)

				if tc.want == "default" {
					assert.Contains(t, result.output, "::warning::Ignoring invalid GH_AW_MAX_AI_CREDITS value (value withheld)")
					assert.Contains(t, result.output, "default budget of "+run.defaultBudget+" AI credits")
					assert.NotContains(t, result.output, "s3cr3t", "raw value must be withheld")
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
		assert.Contains(t, result.output, "Admission allowance applied")
	})

	frontmatter := []struct {
		raw  any
		want string
	}{
		{-1, "-1"},
		{"5K", "5000"},
		{int64(9223372036854775807), "9223372036854775807"},
	}
	for _, tc := range frontmatter {
		t.Run("frontmatter "+fmt.Sprint(tc.raw)+" emits no warning", func(t *testing.T) {
			data := &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", MaxAICredits: parseMaxAICreditsValue(tc.raw)}, NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}}
			script := runtimeMaxAICreditsConfigScript(t, data)
			assert.Contains(t, script, awfMaxAICreditsVarName+`="`+tc.want+`"`)
			result := runRuntimeMaxAICreditsScript(t, script, awfMaxAICreditsVarName+"=abc")
			assert.Equal(t, json.Number(tc.want), result.apiProxy["maxAiCredits"])
			assert.NotContains(t, result.output, "::warning::")
		})
	}
}
