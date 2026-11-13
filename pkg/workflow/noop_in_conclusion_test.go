//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestNoopCommentBodyOutputRequiresNoopStep(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{NoOp: &NoOpConfig{}},
		Tools: map[string]any{
			"work-queue": map[string]any{"worker": true},
		},
	}
	if _, exists := buildConclusionJobOutputs(data)["noop_comment_body"]; exists {
		t.Error("Work-queue participants have no noop step and must not export its categorized body")
	}
}

func TestCompletionCommentFooterMetadata(t *testing.T) {
	data := &WorkflowData{
		Name:         "Test workflow",
		WorkflowID:   "test-workflow",
		EngineConfig: &EngineConfig{ID: "copilot"},
		Model:        "gpt-5.6-luna",
		SafeOutputs:  &SafeOutputsConfig{NoOp: &NoOpConfig{}, ThreatDetection: &ThreatDetectionConfig{}},
		Evals:        &EvalsConfig{Questions: []EvalDefinition{{ID: "result", Question: "Done?"}}},
	}
	env := strings.Join(NewCompiler().buildConclusionScriptEnvVars(data, "agent", nil, ""), "")
	for _, expected := range []string{
		`GH_AW_ENGINE_ID: "copilot"`,
		`GH_AW_ENGINE_MODEL: "gpt-5.6-luna"`,
		`GH_AW_AIC: ${{ needs.agent.outputs.aic }}`,
		`GH_AW_THREAT_DETECTION_AIC: ${{ needs.detection.outputs.aic }}`,
		`GH_AW_EVALS_AIC: ${{ needs.evals.outputs.aic }}`,
		`GH_AW_AMBIENT_CONTEXT: ${{ needs.agent.outputs.ambient_context }}`,
		`GH_AW_WORKFLOW_ID: "test-workflow"`,
	} {
		if !strings.Contains(env, expected) {
			t.Errorf("Completion comment footer is missing %s", expected)
		}
	}
}

func TestCompletionCommentFooterMetadataUsesMainJobName(t *testing.T) {
	tests := []struct {
		name         string
		engineConfig *EngineConfig
		model        string
		expected     map[string]string
	}{
		{
			name:         "explicit model",
			engineConfig: &EngineConfig{ID: "copilot", Version: "1.0.0"},
			model:        "gpt-5.6-luna",
			expected: map[string]string{
				"GH_AW_ENGINE_ID":      "copilot",
				"GH_AW_ENGINE_VERSION": "1.0.0",
				"GH_AW_ENGINE_MODEL":   "gpt-5.6-luna",
			},
		},
		{
			name:         "model routing",
			engineConfig: &EngineConfig{ID: "copilot", ModelRouting: &CopilotModelRoutingConfig{}},
			model:        "must-not-be-baked-in",
			expected: map[string]string{
				"GH_AW_ENGINE_ID":            "copilot",
				"GH_AW_ENGINE_MODEL":         "${{ needs.custom-agent.outputs.model }}",
				"GH_AW_ENGINE_MODEL_EFFORT":  "${{ needs.custom-agent.outputs.model_effort }}",
				"GH_AW_MODEL_ROUTING_STATUS": "${{ needs.custom-agent.outputs.model_routing_status }}",
			},
		},
		{
			name:         "empty model",
			engineConfig: &EngineConfig{ID: "copilot"},
			expected: map[string]string{
				"GH_AW_ENGINE_ID":    "copilot",
				"GH_AW_ENGINE_MODEL": "${{ needs.custom-agent.outputs.model }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusComment := true
			data := &WorkflowData{
				EngineConfig:  tt.engineConfig,
				Model:         tt.model,
				StatusComment: &statusComment,
				SafeOutputs: &SafeOutputsConfig{
					NoOp:             &NoOpConfig{},
					MissingTool:      &MissingToolConfig{},
					ReportIncomplete: &ReportIncompleteConfig{},
				},
			}
			job, err := NewCompiler().buildConclusionJob(data, "custom-agent", nil)
			require.NoError(t, err)
			require.NotNil(t, job)
			require.Contains(t, job.Needs, "custom-agent")
			require.NotContains(t, job.Needs, "agent")

			steps := strings.Join(job.Steps, "")
			for _, match := range regexp.MustCompile(`needs\.([a-zA-Z0-9_-]+)\.`).FindAllStringSubmatch(job.If+steps, -1) {
				require.Contains(t, job.Needs, match[1], "Conclusion references an undeclared job")
			}
			var compiled struct {
				Steps []struct {
					ID  string            `yaml:"id"`
					Env map[string]string `yaml:"env"`
				} `yaml:"steps"`
			}
			require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+steps), &compiled))
			metadataSteps := map[string]bool{"conclusion": false, "noop": false, "missing_tool": false, "report_incomplete": false}
			for _, step := range compiled.Steps {
				if _, ok := metadataSteps[step.ID]; !ok {
					continue
				}
				metadataSteps[step.ID] = true
				metadata := make(map[string]string)
				for key, value := range step.Env {
					if strings.HasPrefix(key, "GH_AW_ENGINE_") || key == "GH_AW_MODEL_ROUTING_STATUS" {
						metadata[key] = value
					}
				}
				require.Equal(t, tt.expected, metadata, "Incorrect footer metadata in step %s", step.ID)
			}
			for id, found := range metadataSteps {
				require.True(t, found, "Missing footer step %s", id)
			}
		})
	}
}

func TestNoopStepInConclusionJob(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "noop-in-conclusion-test")

	// Create a test markdown file with noop safe output
	testContent := `---
on:
  issues:
    types: [opened]
  status-comment: true
permissions:
  contents: read
engine: copilot
safe-outputs:
  noop:
    max: 5
---

# Test Noop in Conclusion

Test that noop step is generated inside the conclusion job.
`

	testFile := filepath.Join(tmpDir, "test-noop.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	lockFile := filepath.Join(tmpDir, "test-noop.lock.yml")
	compiledBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}
	compiled := string(compiledBytes)

	// Verify that there is NO separate noop job
	if strings.Contains(compiled, "\n  noop:") {
		t.Error("There should NOT be a separate noop job")
	}

	// Verify that conclusion job exists
	if !strings.Contains(compiled, "\n  conclusion:") {
		t.Error("Conclusion job should exist")
	}

	// Verify that "Process no-op messages" step is in the conclusion job
	conclusionSection := extractJobSection(compiled, "conclusion")
	if !strings.Contains(conclusionSection, "Process no-op messages") {
		t.Error("Conclusion job should contain 'Process no-op messages' step")
	}

	// Verify that conclusion job has noop_message output
	if !strings.Contains(conclusionSection, "noop_message:") {
		t.Error("Conclusion job should have 'noop_message' output")
	}
	if !strings.Contains(conclusionSection, "noop_comment_body: ${{ steps.noop.outputs.noop_comment_body }}") {
		t.Error("Conclusion job should expose the categorized no-op comment body")
	}
	if !strings.Contains(conclusionSection, "GH_AW_NOOP_COMMENT_BODY: ${{ steps.noop.outputs.noop_comment_body }}") {
		t.Error("Completion status comment should consume the categorized no-op body")
	}

	// Verify that conclusion job does NOT depend on noop job
	if strings.Contains(conclusionSection, "- noop") {
		t.Error("Conclusion job should NOT depend on 'noop' job")
	}

	// Verify that conclusion job depends on agent job
	if !strings.Contains(conclusionSection, "- agent") {
		t.Error("Conclusion job should depend on 'agent' job")
	}
}

func TestMissingToolStepInConclusionJob(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "missing-tool-in-conclusion-test")

	// Create a test markdown file with missing-tool safe output
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
engine: copilot
safe-outputs:
  missing-tool:
    max: 10
---

# Test Missing Tool in Conclusion

Test that missing_tool step is generated inside the conclusion job.
`

	testFile := filepath.Join(tmpDir, "test-missing-tool.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	lockFile := filepath.Join(tmpDir, "test-missing-tool.lock.yml")
	compiledBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}
	compiled := string(compiledBytes)

	// Verify that there is NO separate missing_tool job
	if strings.Contains(compiled, "\n  missing_tool:") {
		t.Error("There should NOT be a separate missing_tool job")
	}

	// Verify that conclusion job exists
	if !strings.Contains(compiled, "\n  conclusion:") {
		t.Error("Conclusion job should exist")
	}

	// Verify that "Record missing tool" step is in the conclusion job
	conclusionSection := extractJobSection(compiled, "conclusion")
	if !strings.Contains(conclusionSection, "Record missing tool") {
		t.Error("Conclusion job should contain 'Record missing tool' step")
	}

	// Verify that conclusion job has missing_tool outputs
	if !strings.Contains(conclusionSection, "tools_reported:") {
		t.Error("Conclusion job should have 'tools_reported' output")
	}
	if !strings.Contains(conclusionSection, "total_count:") {
		t.Error("Conclusion job should have 'total_count' output")
	}

	// Verify that conclusion job does NOT depend on missing_tool job
	if strings.Contains(conclusionSection, "- missing_tool") {
		t.Error("Conclusion job should NOT depend on 'missing_tool' job")
	}

	// Verify that conclusion job depends on agent job
	if !strings.Contains(conclusionSection, "- agent") {
		t.Error("Conclusion job should depend on 'agent' job")
	}
}

func TestBothNoopAndMissingToolInConclusionJob(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "both-in-conclusion-test")

	// Create a test markdown file with both noop and missing-tool safe outputs
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
engine: copilot
safe-outputs:
  noop:
    max: 5
  missing-tool:
    max: 10
---

# Test Both Noop and Missing Tool in Conclusion

Test that both noop and missing_tool steps are generated inside the conclusion job.
`

	testFile := filepath.Join(tmpDir, "test-both.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	lockFile := filepath.Join(tmpDir, "test-both.lock.yml")
	compiledBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}
	compiled := string(compiledBytes)

	// Verify that there are NO separate jobs
	if strings.Contains(compiled, "\n  noop:") {
		t.Error("There should NOT be a separate noop job")
	}
	if strings.Contains(compiled, "\n  missing_tool:") {
		t.Error("There should NOT be a separate missing_tool job")
	}

	// Verify that conclusion job exists and contains both steps
	conclusionSection := extractJobSection(compiled, "conclusion")
	if !strings.Contains(conclusionSection, "Process no-op messages") {
		t.Error("Conclusion job should contain 'Process no-op messages' step")
	}
	if !strings.Contains(conclusionSection, "Record missing tool") {
		t.Error("Conclusion job should contain 'Record missing tool' step")
	}

	// Verify that conclusion job has all outputs
	if !strings.Contains(conclusionSection, "noop_message:") {
		t.Error("Conclusion job should have 'noop_message' output")
	}
	if !strings.Contains(conclusionSection, "tools_reported:") {
		t.Error("Conclusion job should have 'tools_reported' output")
	}
	if !strings.Contains(conclusionSection, "total_count:") {
		t.Error("Conclusion job should have 'total_count' output")
	}
}

func TestReportIncompleteStepInConclusionJob(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "report-incomplete-in-conclusion-test")

	// Create a test markdown file with report-incomplete safe output
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
engine: copilot
safe-outputs:
  report-incomplete:
    max: 5
---

# Test Report Incomplete in Conclusion

Test that report_incomplete step is generated inside the conclusion job.
`

	testFile := filepath.Join(tmpDir, "test-report-incomplete.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	lockFile := filepath.Join(tmpDir, "test-report-incomplete.lock.yml")
	compiledBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}
	compiled := string(compiledBytes)

	// Verify that there is NO separate report_incomplete job
	if strings.Contains(compiled, "\n  report_incomplete:") {
		t.Error("There should NOT be a separate report_incomplete job")
	}

	// Verify that conclusion job exists
	if !strings.Contains(compiled, "\n  conclusion:") {
		t.Error("Conclusion job should exist")
	}

	// Verify that "Record incomplete" step is in the conclusion job
	conclusionSection := extractJobSection(compiled, "conclusion")
	if !strings.Contains(conclusionSection, "Record incomplete") {
		t.Error("Conclusion job should contain 'Record incomplete' step")
	}

	// Verify that conclusion job has report_incomplete output
	if !strings.Contains(conclusionSection, "incomplete_count:") {
		t.Error("Conclusion job should have 'incomplete_count' output")
	}

	// Verify that conclusion job does NOT depend on report_incomplete job
	if strings.Contains(conclusionSection, "- report_incomplete") {
		t.Error("Conclusion job should NOT depend on 'report_incomplete' job")
	}

	// Verify that conclusion job depends on agent job
	if !strings.Contains(conclusionSection, "- agent") {
		t.Error("Conclusion job should depend on 'agent' job")
	}
}
