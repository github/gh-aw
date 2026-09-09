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

// writeCustomEngineWorkflow writes a shared engine definition plus a workflow that uses
// it with safe outputs enabled, and returns the workflow path.
func writeCustomEngineWorkflow(t *testing.T, detectionEngineKey string) string {
	t.Helper()

	tmpDir := testutil.TempDir(t, "threat-detection-custom-engine")
	sharedContent := `---
engine:
  id: harness-engine
  display-name: Harness Engine
` + detectionEngineKey + `  behaviors:
    secret-strategy: universal-llm-consumer
    execution:
      command-name: harness-engine
      step-name: Execute Harness Engine
---

# Shared Engine Definition
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "engine.md"), []byte(sharedContent), 0644))

	workflowContent := `---
on: push
engine:
  id: harness-engine
  model: copilot/gpt-5
imports:
  - shared/engine.md
safe-outputs:
  create-issue:
---

# Test Workflow
`
	workflowPath := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644))
	return workflowPath
}

// TestCustomEngineThreatDetectionFallsBackToBuiltinEngine verifies that a workflow using a
// custom engine does not emit `threat-detect --engine <custom-id>`, which the detection
// binary rejects with a config_error at runtime.
func TestCustomEngineThreatDetectionFallsBackToBuiltinEngine(t *testing.T) {
	workflowPath := writeCustomEngineWorkflow(t, "")

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)

	detectionSection := extractJobSection(string(lockContent), "detection")
	require.NotEmpty(t, detectionSection, "detection job should be generated")
	assert.NotContains(t, detectionSection, "--engine harness-engine",
		"detection must not run with the custom engine id")
	assert.Contains(t, detectionSection, "--engine "+defaultThreatDetectionEngineID,
		"detection must fall back to the default built-in engine")
}

// TestCustomEngineDetectionEngineFrontmatter verifies that an engine definition can declare
// its own detection engine via `detection-engine`.
func TestCustomEngineDetectionEngineFrontmatter(t *testing.T) {
	workflowPath := writeCustomEngineWorkflow(t, "  detection-engine: claude\n")

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)

	detectionSection := extractJobSection(string(lockContent), "detection")
	require.NotEmpty(t, detectionSection, "detection job should be generated")
	assert.Contains(t, detectionSection, "--engine claude",
		"detection must use the engine declared by the engine definition")
}

// TestGetThreatDetectionEngineIDNormalization covers the engine-id resolution rules used by
// both the inline and external detection paths.
func TestGetThreatDetectionEngineIDNormalization(t *testing.T) {
	compiler := NewCompiler()
	compiler.engineCatalog.Register(&EngineDefinition{ID: "custom-declared", DetectionEngine: "codex"})
	compiler.engineCatalog.Register(&EngineDefinition{ID: "custom-invalid", DetectionEngine: "harness-engine"})

	tests := []struct {
		name     string
		data     *WorkflowData
		expected string
	}{
		{name: "built-in engine unchanged", data: &WorkflowData{AI: "claude"}, expected: "claude"},
		{name: "pi normalized to copilot", data: &WorkflowData{AI: "pi"}, expected: "copilot"},
		{name: "embedded unsupported engine falls back", data: &WorkflowData{AI: "gemini"}, expected: defaultThreatDetectionEngineID},
		{name: "unknown custom engine falls back", data: &WorkflowData{AI: "harness-engine"}, expected: defaultThreatDetectionEngineID},
		{name: "declared detection engine wins", data: &WorkflowData{AI: "custom-declared"}, expected: "codex"},
		{name: "unsupported declared engine falls back", data: &WorkflowData{AI: "custom-invalid"}, expected: defaultThreatDetectionEngineID},
		{
			name: "explicit threat-detection engine wins",
			data: &WorkflowData{
				AI: "harness-engine",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{EngineConfig: &EngineConfig{ID: "claude"}},
				},
			},
			expected: "claude",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, compiler.getThreatDetectionEngineID(tt.data))
		})
	}
}

// TestCustomEngineThreatDetectionWarning verifies that compiling a custom-engine workflow
// with threat detection enabled surfaces a warning naming the configuration keys that fix it.
func TestCustomEngineThreatDetectionWarning(t *testing.T) {
	workflowPath := writeCustomEngineWorkflow(t, "")

	compiler := NewCompiler()
	stderr := testutil.CaptureStderr(t, func() {
		require.NoError(t, compiler.CompileWorkflow(workflowPath))
	})

	if !strings.Contains(stderr, "safe-outputs.threat-detection.engine") ||
		!strings.Contains(stderr, "harness-engine") {
		t.Errorf("expected a threat detection warning naming the custom engine and the override key, got:\n%s", stderr)
	}
}
