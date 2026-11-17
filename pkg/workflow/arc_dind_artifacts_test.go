package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteTmpGhAwPathsForArcDind(t *testing.T) {
	t.Run("preserves logical runtime paths until final lowering", func(t *testing.T) {
		input := []string{
			"/tmp/gh-aw/agent_output.json",
			"/tmp/gh-aw/safe_outputs.ndjson",
			"/tmp/gh-aw/aw-prompts/prompt.txt",
			"/tmp/gh-aw/mcp-logs/",
			"/tmp/gh-aw/aw-*.patch",
		}
		result := rewriteTmpGhAwPathsForArcDind(input)
		assert.Equal(t, input, result)
	})

	t.Run("rebases legacy ARC artifact paths without using setup root", func(t *testing.T) {
		input := []string{
			"${{ runner.temp }}/gh-aw/sandbox/firewall/logs/",
			"${{ runner.temp }}/gh-aw/awf-config.json",
		}
		result := rewriteTmpGhAwPathsForArcDind(input)
		assert.Equal(t, []string{
			"/tmp/gh-aw/sandbox/firewall/logs/",
			"/tmp/gh-aw/awf-config.json",
		}, result)
		assert.Equal(t, result, rewriteTmpGhAwPathsForArcDind(result))
	})

	t.Run("rewrites roots and preserves trailing slashes", func(t *testing.T) {
		assert.Equal(t, []string{
			"/tmp/gh-aw",
			"/tmp/gh-aw/",
			"/tmp/gh-aw",
			"/tmp/gh-aw/",
		}, rewriteTmpGhAwPathsForArcDind([]string{
			"/tmp/gh-aw",
			"/tmp/gh-aw/",
			"${{ runner.temp }}/gh-aw",
			"${{ runner.temp }}/gh-aw/",
		}))
	})

	t.Run("preserves unrelated paths", func(t *testing.T) {
		input := []string{
			"/some/other/path",
			"relative/path",
			"/tmp/gh-aw-other/file",
			"${{ runner.temp }}/gh-aw-other/file",
		}
		result := rewriteTmpGhAwPathsForArcDind(input)
		assert.Equal(t, input, result)
	})
}

func TestArcDindPostAgentStepsUsePrivateRuntimePaths(t *testing.T) {
	data := &WorkflowData{RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind}}
	compiler := &Compiler{}

	var output strings.Builder
	compiler.generateTokenUsageSummary(&output, data)
	assert.NotContains(t, output.String(), "GH_AW_TMP_DIR: ${{ runner.temp }}/gh-aw")

	output.Reset()
	compiler.generateAgentStepSummaryAppend(&output, data)
	assert.NotContains(t, output.String(), "GH_AW_TMP_DIR: ${{ runner.temp }}/gh-aw")

	output.Reset()
	compiler.generateAgentOutputPlaceholderStep(&output, data)
	assert.Contains(t, output.String(), "/tmp/gh-aw/agent_output.json")
	assert.NotContains(t, output.String(), "${RUNNER_TEMP}/gh-aw/agent_output.json")
}

func TestArcDindFailureHandlerUsesDownloadedArtifactPaths(t *testing.T) {
	data := &WorkflowData{
		AI:           "copilot",
		SafeOutputs:  &SafeOutputsConfig{},
		RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
	}
	steps, err := NewCompiler().buildAgentFailureStep(data, "agent", "", "")
	require.NoError(t, err)
	assert.NotContains(t, strings.Join(steps, ""), "GH_AW_TMP_DIR:")
}

func TestArcDindDetectionHarnessRetryTelemetryCollection(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		RunnerConfig:  &RunnerConfig{Topology: RunnerTopologyArcDind},
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Disabled: true}},
	}
	detectionData := buildThreatDetectionWorkflowData(data, "")
	assert.False(t, isFirewallEnabled(data))
	assert.True(t, isFirewallEnabled(detectionData))
	detection := strings.Join(compiler.buildCopyDetectionFirewallLogsStep(detectionData), "")
	assert.Contains(t, detection, `"${GH_AW_RUNTIME_DIR}/sandbox/agent/detection_harness_retry.json" /tmp/gh-aw/threat-detection/detection_harness_retry.json`)
}

func TestArcDindArtifactConsolidationKeepsRuntimeOutOfSetup(t *testing.T) {
	var output strings.Builder
	NewCompiler().generateArcDindArtifactConsolidationStep(&output, arcDindArtifactCopy{
		sourcePath: "sandbox/agent/harness_retry.json",
		targetPath: "agent/harness_retry.json",
	})
	step := output.String()
	assert.Contains(t, step, `cp "${GH_AW_RUNTIME_DIR}/sandbox/agent/harness_retry.json" "${GH_AW_RUNTIME_DIR}/agent/harness_retry.json"`)
	assert.NotContains(t, step, "/tmp/gh-aw")
	assert.NotContains(t, step, "cp -a")
	assert.NotContains(t, step, "${RUNNER_TEMP}/gh-aw")
}

func TestArcDindUnifiedArtifactPathsSharePrivateRuntimeRoot(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "copilot"},
		RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind},
		SafeOutputs:  &SafeOutputsConfig{},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}
	paths := NewCompiler().collectArtifactPaths(data, NewCopilotEngine(), "/tmp/gh-aw/agent-stdio.log", nil)
	require.NotEmpty(t, paths)
	for _, artifactPath := range paths {
		assert.True(t, strings.HasPrefix(artifactPath, "/tmp/gh-aw/"), "artifact path must share the logical runtime root until final lowering: %s", artifactPath)
		assert.True(t, strings.HasPrefix(resolveRuntimeStepPaths(artifactPath), "${{ env.GH_AW_RUNTIME_DIR }}/"), "lowered artifact path must share the private runtime root: %s", artifactPath)
	}
}
