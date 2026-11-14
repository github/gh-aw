//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileClaudeOverridesWorkflow(t *testing.T, frontmatter string) (string, string) {
	t.Helper()
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	workflowPath := filepath.Join(t.TempDir(), "claude-overrides.md")
	source := "---\non: workflow_dispatch\npermissions:\n  contents: read\n  copilot-requests: write\ncheckout: false\ntools:\n  github: false\n" +
		frontmatter + "---\nCall noop once."
	require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0600))
	stderr := testutil.CaptureStderr(t, func() {
		require.NoError(t, compiler.CompileWorkflow(workflowPath))
	})
	lock, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	return string(lock), stderr
}

func TestClaudeCopilotModelOverridesResolvedAtRuntime(t *testing.T) {
	for name, frontmatter := range map[string]string{
		"model-provider github": "engine:\n  id: claude\n  model-provider: github\nmodel: claude-haiku-4.5\nsafe-outputs:\n  noop:\n  threat-detection: false\n",
		"copilot prefix":        "engine:\n  id: claude\nmodel: copilot/claude-haiku-4.5\nsafe-outputs:\n  noop:\n  threat-detection: false\n",
	} {
		t.Run(name, func(t *testing.T) {
			lock, _ := compileClaudeOverridesWorkflow(t, frontmatter)
			agent := extractJobSection(lock, "agent")
			assert.Contains(t, agent, "ANTHROPIC_MODEL: claude-haiku-4.5")
			assert.NotContains(t, agent, "--settings", "the runtime harness resolves model overrides from AWF")
			assert.Contains(t, agent, "claude_harness.cjs")
			assert.NotContains(t, agent, `"claude-sonnet-5":`)
			assert.NotContains(t, agent, `"claude-opus-5":`)
			assert.NotContains(t, agent, `"claude-fable-5":`)
			assert.NotContains(t, agent, claudeSettingsEnvVar)
		})
	}
}

func TestClaudeAnthropicProviderOmitsModelOverrides(t *testing.T) {
	lock, _ := compileClaudeOverridesWorkflow(t, "engine:\n  id: claude\nmodel: claude-haiku-4-5\nsafe-outputs:\n  noop:\n  threat-detection: false\n")
	assert.NotContains(t, lock, "modelOverrides")
	assert.NotContains(t, lock, "--settings")
	assert.NotContains(t, lock, claudeSettingsEnvVar)
}

func TestClaudeModelRoutingDefersModelOverridesToRuntime(t *testing.T) {
	lock, _ := compileClaudeOverridesWorkflow(t, "engine:\n  id: claude\n  model-provider: github\n  model-routing:\n    goal: cost\n    mode: economy\n    allowed-models: [claude-sonnet-5]\nsafe-outputs:\n  noop:\n  threat-detection: false\n")
	agent := extractJobSection(lock, "agent")
	assert.Contains(t, agent, "GH_AW_MODEL_ROUTING")
	assert.NotContains(t, agent, "--settings")
	assert.Contains(t, agent, "claude_harness.cjs")
}

func TestClaudeEngineArgsSettingsKeptWithWarning(t *testing.T) {
	for name, args := range map[string]string{
		"separate value": `["--settings", "/tmp/custom-settings.json"]`,
		"equals form":    `["--settings=/tmp/custom-settings.json"]`,
	} {
		t.Run(name, func(t *testing.T) {
			lock, stderr := compileClaudeOverridesWorkflow(t, "engine:\n  id: claude\n  model-provider: github\n  args: "+args+"\nmodel: claude-haiku-4.5\nsafe-outputs:\n  noop:\n  threat-detection: false\n")
			agent := extractJobSection(lock, "agent")
			assert.Equal(t, 1, strings.Count(agent, "--settings"), "exactly one --settings should be passed")
			assert.Contains(t, agent, "/tmp/custom-settings.json")
			assert.NotContains(t, agent, "modelOverrides")
			assert.Contains(t, stderr, "Copilot model-id map (modelOverrides) for Claude Code was not added")
		})
	}
}

func TestClaudeCopilotThreatDetectionModelOverrides(t *testing.T) {
	const mainEngine = "engine:\n  id: claude\n  model-provider: github\nmodel: claude-haiku-4.5\n"
	const claudeDetection = "safe-outputs:\n  noop:\n  threat-detection:\n    engine:\n      id: claude\n      model-provider: github\n    model: claude-haiku-4.5\n"

	t.Run("default detection", func(t *testing.T) {
		lock, _ := compileClaudeOverridesWorkflow(t, mainEngine+"safe-outputs:\n  noop:\n")
		detection := extractJobSection(lock, "detection")
		require.NotEmpty(t, detection)
		assert.Contains(t, detection, "threat-detect --engine copilot")
		assert.NotContains(t, detection, "modelOverrides")
		assert.NotContains(t, detection, claudeSettingsEnvVar)
	})

	t.Run("external claude detector", func(t *testing.T) {
		lock, _ := compileClaudeOverridesWorkflow(t, mainEngine+claudeDetection)
		detection := extractJobSection(lock, "detection")
		require.NotEmpty(t, detection)
		assert.Contains(t, detection, "threat-detect --engine claude")
		assert.NotContains(t, detection, claudeSettingsEnvVar)
		assert.NotContains(t, detection, "modelOverrides")
	})

	t.Run("inline claude detector", func(t *testing.T) {
		lock, _ := compileClaudeOverridesWorkflow(t, mainEngine+"features:\n  gh-aw-detection: false\n"+claudeDetection)
		detection := extractJobSection(lock, "detection")
		require.NotEmpty(t, detection)
		assert.NotContains(t, detection, "threat-detect --engine")
		assert.NotContains(t, detection, "--settings")
		assert.Contains(t, detection, "claude_harness.cjs")
	})
}

func TestClaudeCopilotThreatDetectionSettingsArgs(t *testing.T) {
	const mainEngine = "engine:\n  id: claude\n  model-provider: github\nmodel: claude-haiku-4.5\n"
	for name, args := range map[string]string{
		"separate value": `["--settings", "/tmp/custom-settings.json"]`,
		"equals form":    `["--settings=/tmp/custom-settings.json"]`,
	} {
		detection := "safe-outputs:\n  noop:\n  threat-detection:\n    engine:\n      id: claude\n      model-provider: github\n      args: " + args + "\n    model: claude-haiku-4.5\n"

		t.Run("external claude detector "+name, func(t *testing.T) {
			lock, _ := compileClaudeOverridesWorkflow(t, mainEngine+detection)
			detectionJob := extractJobSection(lock, "detection")
			require.NotEmpty(t, detectionJob)
			assert.Contains(t, detectionJob, "threat-detect --engine claude")
			assert.Contains(t, detectionJob, claudeSettingsEnvVar+": /tmp/custom-settings.json")
			assert.NotContains(t, detectionJob, "modelOverrides")
		})

		t.Run("inline claude detector "+name, func(t *testing.T) {
			lock, _ := compileClaudeOverridesWorkflow(t, mainEngine+"features:\n  gh-aw-detection: false\n"+detection)
			detectionJob := extractJobSection(lock, "detection")
			require.NotEmpty(t, detectionJob)
			assert.NotContains(t, detectionJob, "threat-detect --engine")
			assert.Contains(t, detectionJob, "/tmp/custom-settings.json")
			assert.NotContains(t, detectionJob, claudeSettingsEnvVar)
			assert.NotContains(t, detectionJob, "modelOverrides")
		})
	}
}

func TestClaudeCopilotThreatDetectionSettingsWarningInheritsModel(t *testing.T) {
	lock, stderr := compileClaudeOverridesWorkflow(t, "engine:\n  id: claude\nmodel: copilot/claude-haiku-4.5\nsafe-outputs:\n  noop:\n  threat-detection:\n    engine:\n      id: claude\n      args: [\"--settings\", \"/tmp/custom-settings.json\"]\n")
	assert.Contains(t, stderr, "safe-outputs.threat-detection.engine.args passes --settings, so the Copilot model-id map (modelOverrides) for Claude Code was not added")
	assert.NotContains(t, stderr, "warning: engine.args passes --settings")
	detectionJob := extractJobSection(lock, "detection")
	require.NotEmpty(t, detectionJob)
	assert.Contains(t, detectionJob, claudeSettingsEnvVar+": /tmp/custom-settings.json")
}
