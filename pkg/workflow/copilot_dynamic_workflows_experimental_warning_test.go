//go:build !integration

package workflow

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopilotDynamicWorkflowsExperimentalWarning(t *testing.T) {
	const warning = "Using experimental feature: copilot.dynamic-workflows"
	enabled, disabled := true, false

	tests := []struct {
		name   string
		data   *WorkflowData
		expect bool
	}{
		{name: "disabled by default", data: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot"}}},
		{name: "explicitly enabled", data: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", DynamicWorkflows: &enabled}}, expect: true},
		{name: "explicitly disabled", data: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", DynamicWorkflows: &disabled}}},
		{name: "SDK disabled by default", data: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true}}},
		{name: "SDK explicitly enabled", data: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true, DynamicWorkflows: &enabled}}, expect: true},
		{name: "SDK explicitly disabled", data: &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true, DynamicWorkflows: &disabled}}},
		{name: "legacy engine", data: &WorkflowData{AI: "copilot"}},
		{name: "legacy engine enabled", data: &WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{DynamicWorkflows: &enabled}}, expect: true},
		{name: "legacy engine disabled", data: &WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{DynamicWorkflows: &disabled}}},
		{name: "engine config overrides legacy Copilot", data: &WorkflowData{AI: "copilot", EngineConfig: &EngineConfig{ID: "claude", DynamicWorkflows: &enabled}}},
		{name: "engine config selects Copilot", data: &WorkflowData{AI: "claude", EngineConfig: &EngineConfig{ID: "copilot", DynamicWorkflows: &enabled}}, expect: true},
		{name: "Claude dynamic workflows", data: &WorkflowData{EngineConfig: &EngineConfig{ID: "claude", DynamicWorkflows: &enabled}}},
		{name: "Codex", data: &WorkflowData{EngineConfig: &EngineConfig{ID: "codex"}}},
		{name: "no engine", data: &WorkflowData{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, batchMode := range []bool{false, true} {
				t.Run(map[bool]string{false: "single", true: "batch"}[batchMode], func(t *testing.T) {
					compiler := NewCompiler()
					compiler.SetBatchMode(batchMode)
					var output bytes.Buffer
					compiler.emitExperimentalFeatureWarningsTo(tt.data, &output)

					if tt.expect {
						assert.Equal(t, 1, compiler.GetWarningCount())
						if batchMode {
							assert.Empty(t, output.String())
							assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()[warning])
						} else {
							assert.Contains(t, output.String(), warning)
							assert.Zero(t, compiler.GetExperimentalFeatureUsage()[warning])
						}
					} else {
						assert.Empty(t, output.String())
						assert.Zero(t, compiler.GetWarningCount())
						assert.Zero(t, compiler.GetExperimentalFeatureUsage()[warning])
					}
				})
			}
		})
	}
}

func TestCopilotDynamicWorkflowsDryRunNotice(t *testing.T) {
	const notice = "Using experimental feature: copilot.dynamic-workflows"
	for _, enabled := range []bool{false, true} {
		for _, sdk := range []bool{false, true} {
			for _, batch := range []bool{false, true} {
				compiler := NewCompiler()
				compiler.SetDryRun(true)
				compiler.SetBatchMode(batch)
				data := &WorkflowData{EngineConfig: &EngineConfig{
					ID: "copilot", DynamicWorkflows: &enabled, CopilotSDK: sdk,
				}}
				var output bytes.Buffer
				compiler.emitExperimentalFeatureWarningsTo(data, &output)

				assert.Zero(t, compiler.GetWarningCount(), "dry-run supports explicitly enabled Copilot dynamic workflows")
				if enabled && batch {
					assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()[notice])
				} else {
					assert.Zero(t, compiler.GetExperimentalFeatureUsage()[notice])
				}
				if enabled && !batch {
					assert.Contains(t, output.String(), notice)
					assert.Contains(t, output.String(), "i ")
				} else {
					assert.Empty(t, output.String())
				}
			}
		}
	}
}

func TestCopilotDynamicWorkflowsDryRunPreservesOtherWarnings(t *testing.T) {
	enabled := true
	for _, batch := range []bool{false, true} {
		compiler := NewCompiler()
		compiler.SetDryRun(true)
		compiler.SetBatchMode(batch)
		compiler.IncrementWarningCount()
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot", DynamicWorkflows: &enabled},
			LSP:          map[string]LSPServerConfig{"typescript": {Command: "typescript-language-server"}},
		}
		var output bytes.Buffer
		compiler.emitExperimentalFeatureWarningsTo(data, &output)

		assert.Equal(t, 2, compiler.GetWarningCount(), "existing diagnostics and LSP must remain blocking")
		if batch {
			assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()["Using experimental feature: lsp"])
		} else {
			assert.Contains(t, output.String(), "Using experimental feature: lsp")
		}
	}
}

type failingExperimentalFeatureWriter struct{}

func (failingExperimentalFeatureWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestCopilotDynamicWorkflowsDryRunDiagnosticWriteFailure(t *testing.T) {
	enabled := true
	compiler := NewCompiler()
	compiler.SetDryRun(true)
	data := &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot", DynamicWorkflows: &enabled}}
	compiler.emitExperimentalFeatureWarningsTo(data, failingExperimentalFeatureWriter{})
	assert.Equal(t, 1, compiler.GetWarningCount(), "a failed informational diagnostic must block dry-run compilation")
}
