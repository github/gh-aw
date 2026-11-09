//go:build !integration && !windows

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBehaviorDefinedEngineErrorDetection(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			def := newHarnessEngineDefinition()
			def.Behaviors.ErrorDetection = enabled
			engine, err := NewBehaviorDefinedEngine(def)
			require.NoError(t, err)
			expected := ""
			if enabled {
				expected = "detect_agent_errors"
			}
			assert.Equal(t, expected, engine.GetErrorDetectionScriptId())
		})
	}
	assert.Empty(t, (&BehaviorDefinedEngine{}).GetErrorDetectionScriptId())
}

func TestBehaviorDefinedEngineGeneratedErrorDetection(t *testing.T) {
	deepSeek := loadDeepSeekSample(t)
	require.True(t, deepSeek.Behaviors.ErrorDetection)
	for _, def := range []*EngineDefinition{deepSeek, newHarnessEngineDefinition()} {
		t.Run(def.ID, func(t *testing.T) {
			dir := t.TempDir()
			source, err := yaml.Marshal(map[string]any{"engine": def})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "engine.md"), []byte("---\n"+string(source)+"---\n"), 0o600))
			path := filepath.Join(dir, "test.md")
			workflow := fmt.Sprintf(`---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: %s
model: copilot/claude-haiku-5.5
timeout-minutes: 7
imports: [engine.md]
tools:
  github:
    mode: gh-proxy
    toolsets: [repos]
safe-outputs:
  threat-detection: false
  noop:
---
Test.
`, def.ID)
			require.NoError(t, os.WriteFile(path, []byte(workflow), 0o600))
			require.NoError(t, NewCompiler().CompileWorkflow(path))
			lock, err := os.ReadFile(filepath.Join(dir, "test.lock.yml"))
			require.NoError(t, err)
			var compiled struct {
				Jobs map[string]struct {
					Outputs map[string]string `yaml:"outputs"`
					Steps   []struct {
						ID              string            `yaml:"id"`
						If              string            `yaml:"if"`
						ContinueOnError bool              `yaml:"continue-on-error"`
						Env             map[string]string `yaml:"env"`
						Uses            string            `yaml:"uses"`
						With            map[string]string `yaml:"with"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(lock, &compiled))
			agent := compiled.Jobs[string(constants.AgentJobName)]
			detectors := 0
			for _, step := range agent.Steps {
				if step.ID != string(constants.DetectAgentErrorsStepID) {
					continue
				}
				detectors++
				assert.Equal(t, "always()", step.If)
				assert.True(t, step.ContinueOnError)
				assert.Equal(t, def.ID, step.Env["GH_AW_ENGINE_ID"])
				assert.Equal(t, "${{ steps.agentic_execution.outcome }}", step.Env["GH_AW_AGENTIC_EXECUTION_OUTCOME"])
				assert.Equal(t, "7", step.Env["GH_AW_ENGINE_STEP_TIMEOUT_MINUTES"])
				assert.Contains(t, step.Uses, "actions/github-script@")
				assert.Contains(t, step.With["script"], "'detect_agent_errors.cjs'")
				assert.Contains(t, step.With["script"], "await main();")
			}
			if !def.Behaviors.ErrorDetection {
				assert.Zero(t, detectors)
				assert.NotContains(t, agent.Outputs, "agentic_engine_timeout")
				assert.NotContains(t, agent.Outputs, "model_not_supported_error")
				assert.NotContains(t, string(lock), "GH_AW_AGENTIC_ENGINE_TIMEOUT:")
				return
			}
			assert.Equal(t, 1, detectors)
			for _, output := range []string{"agentic_engine_timeout", "inference_access_error", "model_not_supported_error", "http_400_response_error"} {
				assert.Equal(t, fmt.Sprintf("${{ steps.detect-agent-errors.outputs.%s || 'false' }}", output), agent.Outputs[output])
				assert.Contains(t, string(lock), fmt.Sprintf("GH_AW_%s: ${{ needs.agent.outputs.%s }}", strings.ToUpper(output), output))
			}
		})
	}
}

func TestDetectAgentErrorsStepEngineID(t *testing.T) {
	for _, engine := range []CodingAgentEngine{NewCopilotEngine(), NewClaudeEngine(), NewCodexEngine(), NewPiEngine()} {
		t.Run(engine.GetID(), func(t *testing.T) {
			var generated strings.Builder
			NewCompiler().generateDetectAgentErrorsStep(&generated, &WorkflowData{EngineConfig: &EngineConfig{ID: engine.GetID()}}, engine)
			assert.Contains(t, generated.String(), fmt.Sprintf("GH_AW_ENGINE_ID: %q", engine.GetID()))
		})
	}
}
