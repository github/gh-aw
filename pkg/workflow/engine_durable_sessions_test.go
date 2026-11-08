//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestCompileDurableBuiltinEngines(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex", "gemini", "agy", "pi"} {
		for _, sdk := range []bool{false, true} {
			if sdk && engine != "copilot" {
				continue
			}
			t.Run(fmt.Sprintf("%s/sdk=%t", engine, sdk), func(t *testing.T) {
				dir := t.TempDir()
				extra := ""
				if sdk {
					extra = "  copilot-sdk: true\n"
				}
				source := filepath.Join(dir, "continuable.md")
				require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\nengine:\n  id: "+engine+"\n  durable: {}\n"+extra+"---\nContinue the workflow task.\n"), 0o600))
				require.NoError(t, NewCompiler(WithVersion("dev")).CompileWorkflow(source))
				lock, err := os.ReadFile(filepath.Join(dir, "continuable.lock.yml"))
				require.NoError(t, err)
				text := string(lock)
				for _, expected := range []string{"Restore durable workflow state", "Stage durable session files", "Upload durable checkpoint artifact", "GH_AW_DURABLE_RESUMED", "TRACEPARENT:", "unified_session", "GH_AW_DURABLE_ENGINE:"} {
					require.Contains(t, text, expected)
				}
				require.Equal(t, 1, strings.Count(text, "- name: Restore durable workflow state"))
				require.NotContains(t, text, "Select durable checkpoint artifact")
				require.NotContains(t, text, "Restore durable checkpoint artifact")
				require.NotContains(t, text, "restore_durable_files")
				require.NotContains(t, text, "Prepare durable continuation prompt")
				require.Contains(t, text, "steps.durable_checkpoint.outputs.restored")
				if engine == "pi" {
					require.Contains(t, text, "pi_durable_driver.cjs")
					require.Contains(t, text, "npm ci --prefix /tmp/gh-aw/pi-durable-runtime --ignore-scripts")
				} else {
					require.Contains(t, text, durableSessionDirectory(engine))
					require.Contains(t, text, "durable_artifact.cjs")
					require.NotContains(t, text, "gemini_durable_harness.cjs")
					require.NotContains(t, text, "Install Pi durable")
				}
				if engine == "copilot" {
					require.Contains(t, text, "Collect Copilot durable session files")
					require.Contains(t, text, "steps.capture_durable_sessions.outcome == 'success'")
					require.Contains(t, text, "Scope Copilot session logs to current run")
				} else {
					require.NotContains(t, text, "steps.capture_durable_sessions")
				}
				if engine == "codex" {
					require.Contains(t, text, "CODEX_HOME: /tmp/gh-aw/agent/durable-sessions/codex")
				}
			})
		}
	}
}

func TestDurableBuiltinSessionDeclarations(t *testing.T) {
	registry := NewEngineRegistry()
	for _, id := range []string{"copilot", "claude", "codex", "gemini", "agy"} {
		t.Run(id, func(t *testing.T) {
			engine, err := registry.GetEngine(id)
			require.NoError(t, err)
			data := &WorkflowData{WorkflowID: "test", AI: id, EngineConfig: &EngineConfig{ID: id, Durable: &DurableConfig{RetentionDays: 7}}}
			plan, err := engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
			require.NoError(t, err)
			require.NoError(t, validateDurableFiles(plan.Files))
			require.NotEmpty(t, plan.Files)
			require.Empty(t, plan.InstallationSteps)
			require.NotEmpty(t, plan.ExecutionSteps)
			require.NotContains(t, plan.Files, durableSessionDirectory(id)+"/capture-offset.json")
			for _, file := range plan.Files {
				require.True(t, strings.HasPrefix(file, durableSessionDirectory(id)+"/"))
				require.NotContains(t, file, "settings.json")
				require.NotContains(t, file, "config.toml")
			}

			data.EngineConfig.Args = []string{"--custom-supported-flag"}
			_, err = engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
			require.NoError(t, err)
			data.EngineConfig.Driver = "custom.cjs"
			_, err = engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
			require.ErrorContains(t, err, "drivers")
		})
	}
}

func TestDurableSessionOwnedArguments(t *testing.T) {
	for _, engine := range []string{"claude", "codex", "copilot"} {
		t.Run(engine, func(t *testing.T) {
			args := []string{"--continue", "--session-id=some-session"}
			if engine == "codex" {
				args = []string{"resume", "--last", "--ephemeral"}
			}
			for _, arg := range args {
				require.ErrorContains(t, validateDurableSessionArgs(engine, []string{arg}), "remove")
			}
			require.NoError(t, validateDurableSessionArgs(engine, []string{"--model", "custom-model"}))
		})
	}
}

func TestDurableThreatDetectionStartsFresh(t *testing.T) {
	for _, id := range []string{"copilot", "claude", "codex"} {
		t.Run(id, func(t *testing.T) {
			data := &WorkflowData{
				AI: id,
				EngineConfig: &EngineConfig{
					ID: id, Durable: &DurableConfig{RetentionDays: 7},
				},
				SafeOutputs: &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			}
			detection := buildExternalDetectorWorkflowData(data, id)
			require.Nil(t, detection.EngineConfig.Durable, "detection must not inherit the primary agent's continuation")
			text := strings.Join(NewCompiler().buildDetectionEngineExecutionStep(data), "")
			require.NotContains(t, text, "GH_AW_DURABLE_STORAGE")
			require.NotContains(t, text, durableSessionDirectory(id))
			require.NotNil(t, data.EngineConfig.Durable)
		})
	}
}
