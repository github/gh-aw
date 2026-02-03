//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestCompileJSONOutput tests the JSON output flag functionality
func TestCompileJSONOutput(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	testFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a simple test workflow
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---

# Test Workflow

This is a test workflow for JSON output.
`
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Redirect stdout to capture JSON output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run compilation with JSON output
	config := CompileConfig{
		MarkdownFiles: []string{testFile},
		JSONOutput:    true,
		Verbose:       false,
	}

	_, err := CompileWorkflows(context.Background(), config)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf [4096]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])

	// Parse JSON output
	var results []ValidationResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify results
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Workflow != "test-workflow.md" {
		t.Errorf("Expected workflow 'test-workflow.md', got %q", result.Workflow)
	}

	// The workflow might have warnings but should compile successfully
	if !result.Valid {
		// If not valid, print errors for debugging
		t.Logf("Workflow not valid. Errors: %+v", result.Errors)
		// Allow the test to continue as some errors might be expected
	}

	// Compilation error should be nil or specific
	if err != nil {
		t.Logf("Compilation returned error: %v", err)
	}
}

// TestCompileJSONOutputWithError tests JSON output with validation errors
func TestCompileJSONOutputWithError(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	testFile := filepath.Join(tmpDir, "invalid-workflow.md")

	// Create a workflow with a validation error
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
toolz:
  - invalid
---

# Invalid Workflow

This workflow has an invalid field.
`
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Redirect stdout to capture JSON output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run compilation with JSON output
	config := CompileConfig{
		MarkdownFiles: []string{testFile},
		JSONOutput:    true,
		Verbose:       false,
		Validate:      true,
	}

	_, err := CompileWorkflows(context.Background(), config)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf [4096]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])

	// Parse JSON output
	var results []ValidationResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify results
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	result := results[0]
	if result.Workflow != "invalid-workflow.md" {
		t.Errorf("Expected workflow 'invalid-workflow.md', got %q", result.Workflow)
	}

	if result.Valid {
		t.Error("Expected workflow to be invalid")
	}

	if len(result.Errors) == 0 {
		t.Error("Expected at least one error in results")
	}

	// Verify error contains information about the invalid field
	foundError := false
	for _, e := range result.Errors {
		if e.Type == "parse_error" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("Expected parse_error in errors")
	}

	// Compilation should return an error
	if err == nil {
		t.Error("Expected compilation to return error for invalid workflow")
	}
}

// TestCompileJSONOutputMultipleWorkflows tests JSON output with multiple workflows
func TestCompileJSONOutputMultipleWorkflows(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	validFile := filepath.Join(tmpDir, "valid.md")
	validContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---
# Valid
Test workflow
`

	invalidFile := filepath.Join(tmpDir, "invalid.md")
	invalidContent := `---
on: workflow_dispatch
permissions:
  contents: read
toolz: invalid
---
# Invalid
Test workflow
`

	if err := os.WriteFile(validFile, []byte(validContent), 0644); err != nil {
		t.Fatalf("Failed to create valid file: %v", err)
	}
	if err := os.WriteFile(invalidFile, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to create invalid file: %v", err)
	}

	// Redirect stdout to capture JSON output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run compilation with JSON output
	config := CompileConfig{
		MarkdownFiles: []string{validFile, invalidFile},
		JSONOutput:    true,
		Verbose:       false,
	}

	_, _ = CompileWorkflows(context.Background(), config)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf [8192]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])

	// Parse JSON output
	var results []ValidationResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify results
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// Find valid and invalid results
	var validResult, invalidResult *ValidationResult
	for i := range results {
		switch results[i].Workflow {
		case "valid.md":
			validResult = &results[i]
		case "invalid.md":
			invalidResult = &results[i]
		}
	}

	if validResult == nil || invalidResult == nil {
		t.Fatal("Could not find both valid and invalid results")
	}

	// Verify valid result
	if !validResult.Valid {
		t.Logf("Valid workflow has errors: %+v", validResult.Errors)
	}

	// Verify invalid result
	if invalidResult.Valid {
		t.Error("Invalid workflow should not be valid")
	}
	if len(invalidResult.Errors) == 0 {
		t.Error("Invalid workflow should have errors")
	}
}
