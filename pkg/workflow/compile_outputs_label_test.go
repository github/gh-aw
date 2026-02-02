//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestOutputLabelConfigParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-config-test")

	// Test case with add-labels configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
    allowed: [triage, bug, enhancement, needs-review]
---

# Test Output Label Configuration

This workflow tests the output labels configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-output-labels.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with output labels config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddLabels == nil {
		t.Fatal("Expected labels configuration to be parsed")
	}

	// Verify allowed labels
	expectedLabels := []string{"triage", "bug", "enhancement", "needs-review"}
	if len(workflowData.SafeOutputs.AddLabels.Allowed) != len(expectedLabels) {
		t.Errorf("Expected %d allowed labels, got %d", len(expectedLabels), len(workflowData.SafeOutputs.AddLabels.Allowed))
	}

	for i, expectedLabel := range expectedLabels {
		if i >= len(workflowData.SafeOutputs.AddLabels.Allowed) || workflowData.SafeOutputs.AddLabels.Allowed[i] != expectedLabel {
			t.Errorf("Expected label[%d] to be '%s', got '%s'", i, expectedLabel, workflowData.SafeOutputs.AddLabels.Allowed[i])
		}
	}
}

func TestOutputLabelJobGeneration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-job-test")

	// Test case with add-labels configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
tools:
  github:
    allowed: [issue_read]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
    allowed: [triage, bug, enhancement]
---

# Test Output Label Job Generation

This workflow tests the safe_outputs job generation.
`

	testFile := filepath.Join(tmpDir, "test-output-labels.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output labels: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-output-labels.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify safe_outputs job exists
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Error("Expected 'add_labels' job to be in generated workflow")
	}

	// Verify job properties
	if !strings.Contains(lockContent, "timeout-minutes: 15") {
		t.Error("Expected 10-minute timeout in safe_outputs job")
	}

	if !strings.Contains(lockContent, "permissions:\n      contents: read\n      issues: write\n      pull-requests: write") {
		t.Error("Expected correct permissions in safe_outputs job")
	}

	// Verify the job uses github-script
	if !strings.Contains(lockContent, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
		t.Error("Expected github-script action to be used in safe_outputs job")
	}

	// Verify job has conditional execution with detection
	expectedConditionParts := []string{
		"!cancelled()",
		"needs.detection.outputs.success == 'true'",
	}
	conditionFound := true
	for _, part := range expectedConditionParts {
		if !strings.Contains(lockContent, part) {
			conditionFound = false
			break
		}
	}
	if !conditionFound {
		t.Error("Expected safe_outputs job to have conditional execution with detection check")
	}
	if !strings.Contains(lockContent, "needs:") {
		t.Error("Expected safe_outputs job to depend on main job")
	}

	// Verify JavaScript content includes environment variables for configuration
	if !strings.Contains(lockContent, "GH_AW_AGENT_OUTPUT:") {
		t.Error("Expected agent output content to be passed as environment variable")
	}

	// Verify handler config with allowed labels
	if !strings.Contains(lockContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler config to be passed as environment variable")
	}
	if !strings.Contains(lockContent, `\"add_labels\":{\"allowed\":[\"triage\",\"bug\",\"enhancement\"]}`) {
		t.Error("Expected allowed labels to be in handler config")
	}

	// Verify output variables for the unified handler
	if !strings.Contains(lockContent, "process_safe_outputs_processed_count:") {
		t.Error("Expected processed_count output to be available")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}

func TestOutputLabelJobGenerationNoAllowedLabels(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-no-allowed-test")

	// Test workflow with no allowed labels (any labels permitted)
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
    max: 5
---

# Test Output Label No Allowed Labels

This workflow tests label addition with no allowed labels restriction.
Write your labels to ${{ env.GH_AW_SAFE_OUTPUTS }}, one per line.
`

	testFile := filepath.Join(tmpDir, "test-label-no-allowed.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	lockContent := string(lockBytes)

	// Verify step has conditional execution with detection
	expectedConditionParts := []string{
		"!cancelled()",
		"needs.detection.outputs.success == 'true'",
	}
	conditionFound := true
	for _, part := range expectedConditionParts {
		if !strings.Contains(lockContent, part) {
			conditionFound = false
			break
		}
	}
	if !conditionFound {
		t.Error("Expected safe_outputs job to have conditional execution with detection check")
	}

	// Verify JavaScript content includes environment variables for configuration
	if !strings.Contains(lockContent, "GH_AW_AGENT_OUTPUT:") {
		t.Error("Expected agent output content to be passed as environment variable")
	}

	// Verify max is set in handler config
	if !strings.Contains(lockContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler config to be passed as environment variable")
	}
	if !strings.Contains(lockContent, `\"add_labels\":{`) && !strings.Contains(lockContent, `\"max\":5`) {
		t.Error("Expected max to be set in handler config")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}

func TestOutputLabelJobGenerationNullConfig(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-null-config-test")

	// Test workflow with null add-labels configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
---

# Test Output Label Null Config

This workflow tests label addition with null configuration (any labels allowed).
Write your labels to ${{ env.GH_AW_SAFE_OUTPUTS }}, one per line.
`

	testFile := filepath.Join(tmpDir, "test-label-null-config.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockBytes, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	lockContent := string(lockBytes)

	// Verify safe_outputs job exists
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Error("Expected 'add_labels' job to be in generated workflow")
	}

	// Verify job has conditional execution with detection
	expectedConditionParts := []string{
		"!cancelled()",
		"needs.detection.outputs.success == 'true'",
	}
	conditionFound := true
	for _, part := range expectedConditionParts {
		if !strings.Contains(lockContent, part) {
			conditionFound = false
			break
		}
	}
	if !conditionFound {
		t.Error("Expected safe_outputs job to have conditional execution with detection check")
	}

	// Verify JavaScript content includes environment variables for configuration
	if !strings.Contains(lockContent, "GH_AW_AGENT_OUTPUT:") {
		t.Error("Expected agent output content to be passed as environment variable")
	}

	// Verify the handler config is present (with add_labels configuration)
	if !strings.Contains(lockContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler config to be present")
	}
	if !strings.Contains(lockContent, `\"add_labels\"`) {
		t.Error("Expected add_labels in handler config")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}

func TestOutputLabelConfigNullParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-null-parsing-test")

	// Test case with null add-labels configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
---

# Test Output Label Null Configuration Parsing

This workflow tests the output labels null configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-output-labels-null.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with null labels config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddLabels == nil {
		t.Fatal("Expected labels configuration to be parsed (not nil)")
	}

	// Verify allowed labels is empty (no restrictions)
	if len(workflowData.SafeOutputs.AddLabels.Allowed) != 0 {
		t.Errorf("Expected 0 allowed labels for null config, got %d", len(workflowData.SafeOutputs.AddLabels.Allowed))
	}

	// Verify max is 0 (will use default)
	if workflowData.SafeOutputs.AddLabels.Max != 0 {
		t.Errorf("Expected max to be 0 for null config, got %d", workflowData.SafeOutputs.AddLabels.Max)
	}
}

func TestOutputLabelConfigMaxCountParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-max-test")

	// Test case with add-labels configuration including max
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
    allowed: [triage, bug, enhancement, needs-review]
    max: 5
---

# Test Output Label Max Count Configuration

This workflow tests the output labels max configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-output-labels-max.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with output labels max config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.AddLabels == nil {
		t.Fatal("Expected labels configuration to be parsed")
	}

	// Verify allowed labels
	expectedLabels := []string{"triage", "bug", "enhancement", "needs-review"}
	if len(workflowData.SafeOutputs.AddLabels.Allowed) != len(expectedLabels) {
		t.Errorf("Expected %d allowed labels, got %d", len(expectedLabels), len(workflowData.SafeOutputs.AddLabels.Allowed))
	}

	for i, expectedLabel := range expectedLabels {
		if i >= len(workflowData.SafeOutputs.AddLabels.Allowed) || workflowData.SafeOutputs.AddLabels.Allowed[i] != expectedLabel {
			t.Errorf("Expected label[%d] to be '%s', got '%s'", i, expectedLabel, workflowData.SafeOutputs.AddLabels.Allowed[i])
		}
	}

	// Verify max
	expectedMaxCount := 5
	if workflowData.SafeOutputs.AddLabels.Max != expectedMaxCount {
		t.Errorf("Expected max to be %d, got %d", expectedMaxCount, workflowData.SafeOutputs.AddLabels.Max)
	}
}

func TestOutputLabelConfigDefaultMaxCount(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-default-max-test")

	// Test case with add-labels configuration without max (should use default)
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
    allowed: [triage, bug, enhancement]
---

# Test Output Label Default Max Count

This workflow tests the default max behavior.
`

	testFile := filepath.Join(tmpDir, "test-output-labels-default.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow without max: %v", err)
	}

	// Verify max is 0 (will use default in job generation)
	if workflowData.SafeOutputs.AddLabels.Max != 0 {
		t.Errorf("Expected max to be 0 (default), got %d", workflowData.SafeOutputs.AddLabels.Max)
	}
}

func TestOutputLabelJobGenerationWithMaxCount(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-job-max-test")

	// Test case with add-labels configuration including max
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
tools:
  github:
    allowed: [issue_read]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
    allowed: [triage, bug, enhancement]
    max: 2
---

# Test Output Label Job Generation with Max Count

This workflow tests the safe_outputs job generation with max.
`

	testFile := filepath.Join(tmpDir, "test-output-labels-max.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output labels max: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-output-labels-max.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify safe_outputs job exists
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Error("Expected 'add_labels' job to be in generated workflow")
	}

	// Verify handler config contains the configuration
	if !strings.Contains(lockContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler config to be set as environment variable")
	}
	// Verify allowed labels in handler config
	if !strings.Contains(lockContent, `\"add_labels\":{\"allowed\":[\"triage\",\"bug\",\"enhancement\"]`) {
		t.Error("Expected allowed labels to be in handler config")
	}
	// Verify max in handler config
	if !strings.Contains(lockContent, `\"max\":2`) {
		t.Error("Expected max to be in handler config")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}

func TestOutputLabelJobGenerationWithDefaultMaxCount(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-job-default-max-test")

	// Test case with add-labels configuration without max (should use default of 3)
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
tools:
  github:
    allowed: [issue_read]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
    allowed: [triage, bug, enhancement]
---

# Test Output Label Job Generation with Default Max Count

This workflow tests the safe_outputs job generation with default max.
`

	testFile := filepath.Join(tmpDir, "test-output-labels-default-max.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with output labels default max: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-output-labels-default-max.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify safe_outputs job exists with handler
	if !strings.Contains(lockContent, "safe_outputs:") {
		t.Error("Expected 'safe_outputs' job to be in generated workflow")
	}
	// Verify handler config is present (process_safe_outputs handles all outputs)
	if !strings.Contains(lockContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected handler config to be in generated workflow")
	}
	if !strings.Contains(lockContent, `\"add_labels\"`) {
		t.Error("Expected add_labels in handler config")
	}

	// t.Logf("Generated workflow content:\n%s", lockContent)
}

func TestOutputLabelConfigValidation(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-validation-test")

	// Test case with empty allowed labels (should fail)
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels:
    allowed: []
---

# Test Output Label Validation

This workflow tests validation of empty allowed labels.
`

	testFile := filepath.Join(tmpDir, "test-label-validation.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow - should fail with empty allowed labels
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("Expected error when compiling workflow with empty allowed labels")
	}

	if !strings.Contains(err.Error(), "minItems: got 0, want 1") {
		t.Errorf("Expected schema validation error about minItems, got: %v", err)
	}
}

func TestOutputLabelConfigMissingAllowed(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-label-missing-test")

	// Test case with missing allowed field (should now succeed)
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  add-labels: {}
---

# Test Output Label Missing Allowed

This workflow tests that missing allowed field is now optional.
`

	testFile := filepath.Join(tmpDir, "test-label-missing.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow - should now succeed with missing allowed labels
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Expected compilation to succeed with missing allowed labels, got error: %v", err)
	}

	// Verify the workflow was compiled successfully
	lockFile := stringutil.MarkdownToLockFile(testFile)
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}
}
