package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteTmpGhAwPathsForArcDind(t *testing.T) {
	t.Run("rewrites /tmp/gh-aw/ prefixed paths to runner.temp expression", func(t *testing.T) {
		input := []string{
			"/tmp/gh-aw/agent_output.json",
			"/tmp/gh-aw/safe_outputs.ndjson",
			"/tmp/gh-aw/aw-prompts/prompt.txt",
			"/tmp/gh-aw/mcp-logs/",
			"/tmp/gh-aw/aw-*.patch",
		}
		result := rewriteTmpGhAwPathsForArcDind(input)
		assert.Equal(t, "${{ runner.temp }}/gh-aw/agent_output.json", result[0])
		assert.Equal(t, "${{ runner.temp }}/gh-aw/safe_outputs.ndjson", result[1])
		assert.Equal(t, "${{ runner.temp }}/gh-aw/aw-prompts/prompt.txt", result[2])
		assert.Equal(t, "${{ runner.temp }}/gh-aw/mcp-logs/", result[3])
		assert.Equal(t, "${{ runner.temp }}/gh-aw/aw-*.patch", result[4])
	})

	t.Run("preserves paths already using runner.temp expression", func(t *testing.T) {
		input := []string{
			"${{ runner.temp }}/gh-aw/sandbox/firewall/logs/",
			"${{ runner.temp }}/gh-aw/awf-config.json",
		}
		result := rewriteTmpGhAwPathsForArcDind(input)
		assert.Equal(t, input, result)
	})

	t.Run("preserves unrelated paths", func(t *testing.T) {
		input := []string{
			"/some/other/path",
			"relative/path",
		}
		result := rewriteTmpGhAwPathsForArcDind(input)
		assert.Equal(t, input, result)
	})
}

func TestArcDindPostAgentStepsUseRunnerTempPaths(t *testing.T) {
	data := &WorkflowData{RunnerConfig: &RunnerConfig{Topology: RunnerTopologyArcDind}}
	compiler := &Compiler{}

	var output strings.Builder
	compiler.generateTokenUsageSummary(&output, data)
	assert.Contains(t, output.String(), "GH_AW_TMP_DIR: ${{ runner.temp }}/gh-aw")

	output.Reset()
	compiler.generateAgentStepSummaryAppend(&output, data)
	assert.Contains(t, output.String(), "GH_AW_TMP_DIR: ${{ runner.temp }}/gh-aw")

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
