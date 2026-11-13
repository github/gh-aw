//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestUpdateIssueOperation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "omitted"},
		{name: "append", value: "append"},
		{name: "prepend", value: "prepend"},
		{name: "replace", value: "replace"},
		{name: "replace-island", value: "replace-island"},
		{name: "invalid", value: "merge", wantError: true},
		{name: "null", value: "null", wantError: true},
		{name: "number", value: "123", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operationLine := ""
			if tc.value != "" {
				operationLine = "    operation: " + tc.value + "\n"
			}
			content := fmt.Sprintf(`---
on: workflow_dispatch
engine: claude
strict: false
safe-outputs:
  update-issue:
    body: true
%s---
Update the tracker issue.
`, operationLine)
			testFile := filepath.Join(testutil.TempDir(t, "update-issue-operation"), "workflow.md")
			if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			compiler := NewCompiler()
			data, err := compiler.ParseWorkflowFile(testFile)
			if tc.wantError {
				if err == nil {
					t.Fatal("Expected invalid operation to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			operation := data.SafeOutputs.UpdateIssues.Operation
			if tc.value == "" {
				if operation != nil {
					t.Fatalf("Expected no configured operation, got %q", *operation)
				}
			} else if operation == nil || *operation != tc.value {
				t.Fatalf("Expected configured operation %q, got %v", tc.value, operation)
			}
			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, data)
			config := extractHandlerConfig(t, strings.Join(steps, ""))["update_issue"]
			if tc.value == "" {
				if _, exists := config["default_operation"]; exists {
					t.Fatal("Expected omitted operation to preserve the runtime default")
				}
			} else if config["default_operation"] != tc.value {
				t.Fatalf("Expected serialized default operation %q, got %v", tc.value, config["default_operation"])
			}
		})
	}
}

func TestUpdateIssueConfigParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-issue-test")

	// Test case with basic update-issue configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-issue:
---

# Test Update Issue Configuration

This workflow tests the update-issue configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-update-issue.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with update-issue config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	// Check defaults
	if templatableIntValue(workflowData.SafeOutputs.UpdateIssues.Max) != 1 {
		t.Fatalf("Expected max to be 1, got %v", workflowData.SafeOutputs.UpdateIssues.Max)
	}

	if workflowData.SafeOutputs.UpdateIssues.Target != "" {
		t.Fatalf("Expected target to be empty (default), got '%s'", workflowData.SafeOutputs.UpdateIssues.Target)
	}

	if workflowData.SafeOutputs.UpdateIssues.Status != nil {
		t.Fatal("Expected status to be nil by default (not updatable)")
	}

	if workflowData.SafeOutputs.UpdateIssues.Title != nil {
		t.Fatal("Expected title to be nil by default (not updatable)")
	}

	if workflowData.SafeOutputs.UpdateIssues.Body != nil {
		t.Fatal("Expected body to be nil by default (not updatable)")
	}
}

func TestUpdateIssueConfigWithAllOptions(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-issue-all-test")

	// Test case with all options configured
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-issue:
    max: 3
    target: "*"
    status:
    title:
    body: true
---

# Test Update Issue Full Configuration

This workflow tests the update-issue configuration with all options.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-full.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with full update-issue config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	// Check all options
	if templatableIntValue(workflowData.SafeOutputs.UpdateIssues.Max) != 3 {
		t.Fatalf("Expected max to be 3, got %v", workflowData.SafeOutputs.UpdateIssues.Max)
	}

	if workflowData.SafeOutputs.UpdateIssues.Target != "*" {
		t.Fatalf("Expected target to be '*', got '%s'", workflowData.SafeOutputs.UpdateIssues.Target)
	}

	if workflowData.SafeOutputs.UpdateIssues.Status == nil {
		t.Fatal("Expected status to be non-nil (updatable)")
	}

	if workflowData.SafeOutputs.UpdateIssues.Title == nil {
		t.Fatal("Expected title to be non-nil (updatable)")
	}

	if workflowData.SafeOutputs.UpdateIssues.Body == nil {
		t.Fatal("Expected body to be non-nil (updatable)")
	}

	// Verify body is set to true
	if !*workflowData.SafeOutputs.UpdateIssues.Body {
		t.Fatal("Expected body to be true")
	}
}

func TestUpdateIssueConfigTargetParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-issue-target-test")

	// Test case with specific target number
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-issue:
    target: "123"
    title:
---

# Test Update Issue Target Configuration

This workflow tests the update-issue target configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-target.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with target update-issue config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues.Target != "123" {
		t.Fatalf("Expected target to be '123', got '%s'", workflowData.SafeOutputs.UpdateIssues.Target)
	}

	if workflowData.SafeOutputs.UpdateIssues.Title == nil {
		t.Fatal("Expected title to be non-nil (updatable)")
	}
}

func TestUpdateIssueBodyBooleanTrue(t *testing.T) {
	// Test that body: true explicitly enables body updates
	tmpDir := testutil.TempDir(t, "output-update-issue-body-true-test")

	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-issue:
    body: true
---

# Test Update Issue Body True

This workflow tests body: true configuration.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-body-true.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow: %v", err)
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues.Body == nil {
		t.Fatal("Expected body to be non-nil")
	}

	if !*workflowData.SafeOutputs.UpdateIssues.Body {
		t.Fatal("Expected body to be true")
	}
}

func TestUpdateIssueBodyBooleanFalse(t *testing.T) {
	// Test that body: false explicitly disables body updates
	tmpDir := testutil.TempDir(t, "output-update-issue-body-false-test")

	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-issue:
    body: false
---

# Test Update Issue Body False

This workflow tests body: false configuration.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-body-false.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow: %v", err)
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues.Body == nil {
		t.Fatal("Expected body to be non-nil")
	}

	if *workflowData.SafeOutputs.UpdateIssues.Body {
		t.Fatal("Expected body to be false")
	}
}

func TestUpdateIssueBodyNullBackwardCompatibility(t *testing.T) {
	// Test that body: (null) maintains backward compatibility and defaults to true
	tmpDir := testutil.TempDir(t, "output-update-issue-body-null-test")

	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-issue:
    body:
---

# Test Update Issue Body Null

This workflow tests body: (null) for backward compatibility.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-body-null.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow: %v", err)
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	// With FieldParsingBoolValue mode, null values are treated as true (explicit enablement)
	// This maintains backward compatibility where body: enables body updates
	if workflowData.SafeOutputs.UpdateIssues.Body == nil {
		t.Fatal("Expected body to be non-nil when set to null")
	}

	if !*workflowData.SafeOutputs.UpdateIssues.Body {
		t.Fatal("Expected body to be true when set to null (backward compatibility)")
	}
}

func TestUpdateIssueTitlePrefix(t *testing.T) {
	// Test that title-prefix is parsed correctly
	tmpDir := testutil.TempDir(t, "output-update-issue-title-prefix-test")

	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-issue:
    title-prefix: "[bot] "
    body: true
---

# Test Update Issue Title Prefix

This workflow tests the update-issue title-prefix configuration.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-title-prefix.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with title-prefix: %v", err)
	}

	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues.TitlePrefix != "[bot] " {
		t.Fatalf("Expected title-prefix to be '[bot] ', got '%s'", workflowData.SafeOutputs.UpdateIssues.TitlePrefix)
	}

	if workflowData.SafeOutputs.UpdateIssues.RequiredTitlePrefix != "" {
		t.Errorf("Expected RequiredTitlePrefix to be empty when only title-prefix is set, got %q", workflowData.SafeOutputs.UpdateIssues.RequiredTitlePrefix)
	}
}

func TestUpdateIssueRequiredFilters(t *testing.T) {
	// Test that required-labels and required-title-prefix are parsed correctly
	tmpDir := testutil.TempDir(t, "output-update-issue-required-filters-test")

	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-issue:
    required-title-prefix: "[ci] "
    required-labels: [automation, bot]
    body: true
---

# Test Update Issue Required Filters

This workflow tests the update-issue required-labels and required-title-prefix configuration.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-required-filters.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with required filters: %v", err)
	}

	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues.RequiredTitlePrefix != "[ci] " {
		t.Fatalf("Expected required-title-prefix to be '[ci] ', got '%s'", workflowData.SafeOutputs.UpdateIssues.RequiredTitlePrefix)
	}

	if len(workflowData.SafeOutputs.UpdateIssues.RequiredLabels) != 2 {
		t.Fatalf("Expected 2 required-labels, got %d", len(workflowData.SafeOutputs.UpdateIssues.RequiredLabels))
	}

	if workflowData.SafeOutputs.UpdateIssues.RequiredLabels[0] != "automation" {
		t.Fatalf("Expected first required label to be 'automation', got '%s'", workflowData.SafeOutputs.UpdateIssues.RequiredLabels[0])
	}

	if workflowData.SafeOutputs.UpdateIssues.RequiredLabels[1] != "bot" {
		t.Fatalf("Expected second required label to be 'bot', got '%s'", workflowData.SafeOutputs.UpdateIssues.RequiredLabels[1])
	}
}
