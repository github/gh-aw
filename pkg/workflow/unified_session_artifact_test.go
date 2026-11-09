//go:build !integration

package workflow

import (
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedSessionAgentArtifactPaths(t *testing.T) {
	for _, id := range []string{"claude", "copilot", "codex", "gemini", "pi"} {
		t.Run(id, func(t *testing.T) {
			compiler := NewCompiler()
			engine, err := compiler.getAgenticEngine(id)
			require.NoError(t, err)
			data := &WorkflowData{AI: id}
			paths := compiler.collectArtifactPaths(data, engine, constants.AgentStdioLogPath, nil)
			assert.Contains(t, paths, "/tmp/gh-aw/agent-session.jsonl")
			assert.Contains(t, paths, "/tmp/gh-aw/agent-errors.jsonl")
			if id == "copilot" {
				assert.Contains(t, paths, "/tmp/gh-aw/agent/model-endpoint-mismatch.json")
				assert.Contains(t, paths, "/tmp/gh-aw/runtime/aw_info.json")
			} else {
				assert.NotContains(t, paths, "/tmp/gh-aw/agent/model-endpoint-mismatch.json")
				assert.NotContains(t, paths, "/tmp/gh-aw/runtime/aw_info.json")
			}

			assert.Contains(t, paths, "/tmp/gh-aw/aw-prompts/user.txt")
			assert.Contains(t, paths, "/tmp/gh-aw/aw-prompts/system.txt")
			assert.Contains(t, paths, agentExecutionExitCodePath)
			assert.NotContains(t, paths, pluginInstallationsPath)
			data.Plugins = []string{"org/plugin@" + testPluginSHA}
			assert.Contains(t, compiler.collectArtifactPaths(data, engine, constants.AgentStdioLogPath, nil), pluginInstallationsPath)
		})
	}
}

func TestRuntimeModelMetadataConclusionTransport(t *testing.T) {
	for _, id := range []string{"copilot", "claude", "pi"} {
		for _, routed := range []bool{false, true} {
			t.Run(id+"/"+strconv.FormatBool(routed), func(t *testing.T) {
				compiler := NewCompiler()
				engine, err := compiler.getAgenticEngine(id)
				require.NoError(t, err)
				data := &WorkflowData{
					AI: id, EngineConfig: &EngineConfig{ID: id},
					SafeOutputs: &SafeOutputsConfig{},
				}
				if routed {
					data.EngineConfig.ModelRouting = &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced"}
				}
				paths := compiler.collectArtifactPaths(data, engine, constants.AgentStdioLogPath, nil)
				steps := strings.Join(compiler.buildConclusionSetupSteps(data), "")
				if routed || id == "copilot" {
					assert.Contains(t, paths, "/tmp/gh-aw/runtime/aw_info.json")
					assert.Contains(t, steps, "if [ -f /tmp/gh-aw/runtime/aw_info.json ]; then cp /tmp/gh-aw/runtime/aw_info.json /tmp/gh-aw/aw_info.json; fi")
					download := strings.Index(steps, "Download agent output artifact")
					require.GreaterOrEqual(t, download, 0)
					assert.Contains(t, steps[download:strings.Index(steps, "Restore runtime model metadata")], "path: /tmp/gh-aw/")
					assert.Less(t, download, strings.Index(steps, "Restore runtime model metadata"))
					assert.Less(t, strings.Index(steps, "Download workflow metadata"), strings.Index(steps, "Restore runtime model metadata"))
				} else {
					assert.NotContains(t, paths, "/tmp/gh-aw/runtime/aw_info.json")
					assert.NotContains(t, steps, "Restore runtime model metadata")
				}
			})
		}
	}
}

func TestWorkflowMetadataConclusionTransport(t *testing.T) {
	for _, id := range []string{"copilot", "claude", "codex", "gemini", "pi", "kiro"} {
		for _, reusable := range []bool{false, true} {
			t.Run(id+"/"+strconv.FormatBool(reusable), func(t *testing.T) {
				for _, ghes := range []bool{false, true} {
					t.Run("ghes/"+strconv.FormatBool(ghes), func(t *testing.T) {
						compiler := NewCompiler()
						compiler.SetGHESCompat(ghes)
						compiler.configureGHESCompatibility()
						data := &WorkflowData{
							AI: id, EngineConfig: &EngineConfig{ID: id},
							SafeOutputs: &SafeOutputsConfig{},
							On:          "workflow_dispatch:",
						}
						if reusable {
							data.On = "workflow_call:"
						}
						artifactName := "info"
						if reusable {
							artifactName = "${{ needs.activation.outputs.artifact_prefix }}info"
						}
						steps := strings.Join(compiler.buildConclusionSetupSteps(data), "")
						download := extractWorkflowStepByName(t, steps, "Download workflow metadata")
						assert.Contains(t, download, "if: always()")
						assert.Contains(t, download, "continue-on-error: true")
						assert.Contains(t, download, "name: "+artifactName+"\n")
						assert.Contains(t, download, "path: /tmp/gh-aw/")
						assert.NotContains(t, download, "pattern:")
						assert.NotContains(t, download, "activation\n")
						assert.Contains(t, download, "uses: "+compiler.getActionPin("actions/download-artifact"))
						assert.Less(t, strings.Index(steps, "Download agent output artifact"), strings.Index(steps, "Download workflow metadata"))
					})
				}
			})
		}
	}
}
