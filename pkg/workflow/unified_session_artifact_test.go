//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
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

func TestUnifiedArtifactConditionallyIncludesOTLPMetricsMirror(t *testing.T) {
	for _, tt := range []struct {
		name        string
		env         string
		wantMetrics bool
	}{
		{name: "enabled by default", env: "env:\n  GH_AW_OTLP_ENDPOINTS: '[]'\n", wantMetrics: true},
		{name: "explicitly enabled", env: "env:\n  GH_AW_OTLP_ENDPOINTS: '[]'\n  GH_AW_OTEL_METRICS: 'true'\n", wantMetrics: true},
		{name: "runtime opt out", env: "env:\n  GH_AW_OTLP_ENDPOINTS: '[]'\n  GH_AW_OTEL_METRICS: 'false'\n", wantMetrics: true},
		{name: "not configured", env: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			engine, err := compiler.getAgenticEngine("copilot")
			require.NoError(t, err)
			data := &WorkflowData{AI: "copilot", Env: tt.env}

			paths := compiler.collectArtifactPaths(data, engine, constants.AgentStdioLogPath, nil)

			if tt.wantMetrics {
				assert.Contains(t, paths, conditionalOTLPMetricsArtifactPath)
				assert.NotContains(t, recoveryArtifactScanRoots(paths), constants.TmpGhAwDirSlash+constants.OtelMetricsJsonlFilename.String())
				assert.Contains(t, paths, constants.TmpGhAwDirSlash+constants.OtelJsonlFilename.String())
				assert.Contains(t, paths, constants.TmpGhAwDirSlash+constants.OtlpExportErrorsFilename.String())
			} else {
				assert.NotContains(t, paths, conditionalOTLPMetricsArtifactPath)
				assert.NotContains(t, paths, constants.TmpGhAwDirSlash+constants.OtelJsonlFilename.String())
				assert.NotContains(t, paths, constants.TmpGhAwDirSlash+constants.OtlpExportErrorsFilename.String())
			}
		})
	}
}

func TestOTLPMetricsConclusionRunsBeforeArtifactUploadWithRuntimeGuard(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  string
		want bool
	}{
		{name: "enabled by default", env: "env:\n  GH_AW_OTLP_ENDPOINTS: '[]'\n", want: true},
		{name: "runtime opt out", env: "env:\n  GH_AW_OTLP_ENDPOINTS: '[]'\n  GH_AW_OTEL_METRICS: 'false'\n", want: true},
		{name: "not configured", env: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			NewCompiler().generateOTLPMetricsConclusionStep(&yaml, &WorkflowData{Env: tt.env})

			if tt.want {
				assert.Contains(t, yaml.String(), "action_conclusion_otlp.cjs")
				assert.Contains(t, yaml.String(), "if: always() && env.GH_AW_OTEL_METRICS == 'true'")
				assert.Contains(t, yaml.String(), "GH_AW_JOB_RESULT: ${{ job.status }}")
			} else {
				assert.Empty(t, yaml.String())
			}
		})
	}
}

func TestOTLPMetricsDefaultWorkflowCompilesWithConclusionBeforeUpload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, "metrics.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
on: workflow_dispatch
engine: copilot
observability:
  otlp:
    endpoint: https://otel.example.com
---
Test metrics.
`), 0600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(path))
	content, err := os.ReadFile(filepath.Join(dir, "metrics.lock.yml"))
	require.NoError(t, err)
	compiled := string(content)

	assert.Contains(t, compiled, "GH_AW_OTEL_METRICS: ${{ vars.GH_AW_OTEL_METRICS || 'true' }}")
	assert.Contains(t, compiled, "if: always() && env.GH_AW_OTEL_METRICS == 'true'")
	emit := strings.Index(compiled, "Emit OTLP job conclusion before artifacts")
	upload := strings.Index(compiled, "Upload agent artifacts")
	require.NotEqual(t, -1, emit)
	require.NotEqual(t, -1, upload)
	assert.Less(t, emit, upload)
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
