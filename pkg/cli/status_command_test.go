//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
)

func TestStatusWorkflows_JSONOutput(t *testing.T) {

	// Save current directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Failed to change to repository root: %v", err)
	}
	defer os.Chdir(originalDir)

	// Test JSON output without pattern
	t.Run("JSON output without pattern", func(t *testing.T) {
		err := StatusWorkflows("", false, true, "", "", "")
		if err != nil {
			t.Errorf("StatusWorkflows with JSON flag failed: %v", err)
		}
		// Note: We can't easily capture stdout in this test,
		// but we verify it doesn't error
	})

	// Test JSON output with pattern
	t.Run("JSON output with pattern", func(t *testing.T) {
		err := StatusWorkflows("smoke", false, true, "", "", "")
		if err != nil {
			t.Errorf("StatusWorkflows with JSON flag and pattern failed: %v", err)
		}
	})
}

func TestWorkflowStatus_JSONMarshaling(t *testing.T) {
	// Test that WorkflowStatus can be marshaled to JSON
	status := WorkflowStatus{
		Workflow:      "test-workflow",
		EngineID:      "copilot",
		Compiled:      "Yes",
		Status:        "active",
		TimeRemaining: "N/A",
		On: map[string]any{
			"workflow_dispatch": nil,
		},
	}

	jsonBytes, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Failed to marshal WorkflowStatus: %v", err)
	}

	// Verify JSON contains expected fields
	var unmarshaled map[string]any
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if unmarshaled["workflow"] != "test-workflow" {
		t.Errorf("Expected workflow='test-workflow', got %v", unmarshaled["workflow"])
	}
	if unmarshaled["engine_id"] != "copilot" {
		t.Errorf("Expected engine_id='copilot', got %v", unmarshaled["engine_id"])
	}
	if unmarshaled["compiled"] != "Yes" {
		t.Errorf("Expected compiled='Yes', got %v", unmarshaled["compiled"])
	}
	if unmarshaled["status"] != "active" {
		t.Errorf("Expected status='active', got %v", unmarshaled["status"])
	}
	if unmarshaled["time_remaining"] != "N/A" {
		t.Errorf("Expected time_remaining='N/A', got %v", unmarshaled["time_remaining"])
	}

	// Verify "on" field is included
	onField, ok := unmarshaled["on"].(map[string]any)
	if !ok {
		t.Fatalf("Expected 'on' to be a map, got %T", unmarshaled["on"])
	}
	if _, exists := onField["workflow_dispatch"]; !exists {
		t.Errorf("Expected 'on' to contain 'workflow_dispatch' key")
	}
}

// TestStatusCommand_JSONOutputValidation tests that the status command with --json flag returns valid JSON
func TestStatusCommand_JSONOutputValidation(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Get the current directory for proper path resolution
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Failed to change to repository root: %v", err)
	}
	defer os.Chdir(originalDir)

	// Run the status command with --json flag
	cmd := exec.Command(filepath.Join(originalDir, binaryPath), "status", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Logf("Command stderr: %s", stderr.String())
		t.Fatalf("Failed to run status command: %v", err)
	}

	// Verify the output is valid JSON
	output := stdout.String()
	if output == "" {
		t.Fatal("Expected non-empty JSON output")
	}

	// Try to parse as JSON array
	var statuses []WorkflowStatus
	if err := json.Unmarshal([]byte(output), &statuses); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Verify we got an array (even if empty)
	if statuses == nil {
		t.Error("Expected JSON array, got nil")
	}

	// If we have workflows, verify structure
	if len(statuses) > 0 {
		firstStatus := statuses[0]

		// Verify all required fields are present
		if firstStatus.Workflow == "" {
			t.Error("Expected 'workflow' field to be non-empty")
		}
		if firstStatus.EngineID == "" {
			t.Error("Expected 'engine_id' field to be non-empty")
		}
		if firstStatus.Compiled == "" {
			t.Error("Expected 'compiled' field to be non-empty")
		}
		if firstStatus.Status == "" {
			t.Error("Expected 'status' field to be non-empty")
		}
		if firstStatus.TimeRemaining == "" {
			t.Error("Expected 'time_remaining' field to be non-empty")
		}

		t.Logf("Successfully parsed %d workflow status entries", len(statuses))
		t.Logf("First entry: workflow=%s, engine_id=%s, compiled=%s",
			firstStatus.Workflow, firstStatus.EngineID, firstStatus.Compiled)
	}
}

// TestStatusCommand_JSONOutputWithPattern tests that status --json works with a pattern filter
func TestStatusCommand_JSONOutputWithPattern(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Get the current directory for proper path resolution
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Failed to change to repository root: %v", err)
	}
	defer os.Chdir(originalDir)

	// Run the status command with --json flag and pattern
	cmd := exec.Command(filepath.Join(originalDir, binaryPath), "status", "smoke", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Logf("Command stderr: %s", stderr.String())
		t.Fatalf("Failed to run status command with pattern: %v", err)
	}

	// Verify the output is valid JSON
	output := stdout.String()
	if output == "" {
		t.Fatal("Expected non-empty JSON output")
	}

	// Try to parse as JSON array
	var statuses []WorkflowStatus
	if err := json.Unmarshal([]byte(output), &statuses); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// All filtered results should contain "smoke" in the workflow name
	for _, status := range statuses {
		if !strings.Contains(strings.ToLower(status.Workflow), "smoke") {
			t.Errorf("Expected workflow name to contain 'smoke', got: %s", status.Workflow)
		}
	}

	t.Logf("Successfully parsed %d filtered workflow status entries", len(statuses))
}

// TestStatusCommand_JSONOutputIncludesOnField tests that the "on" field is included in JSON output
func TestStatusCommand_JSONOutputIncludesOnField(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Get the current directory for proper path resolution
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("Failed to change to repository root: %v", err)
	}
	defer os.Chdir(originalDir)

	// Run the status command with --json flag
	cmd := exec.Command(filepath.Join(originalDir, binaryPath), "status", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Logf("Command stderr: %s", stderr.String())
		t.Fatalf("Failed to run status command: %v", err)
	}

	// Verify the output is valid JSON
	output := stdout.String()
	if output == "" {
		t.Fatal("Expected non-empty JSON output")
	}

	// Try to parse as JSON array
	var statuses []WorkflowStatus
	if err := json.Unmarshal([]byte(output), &statuses); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// If we have workflows, verify "on" field is present
	if len(statuses) > 0 {
		firstStatus := statuses[0]

		// Verify "on" field is present
		if firstStatus.On == nil {
			t.Error("Expected 'on' field to be present")
		} else {
			t.Logf("'on' field for workflow '%s': %v", firstStatus.Workflow, firstStatus.On)
		}

		t.Logf("Successfully verified 'on' field for %d workflow(s)", len(statuses))
	} else {
		t.Skip("No workflows found to test 'on' field")
	}
}

// TestWorkflowStatus_ConsoleRendering tests that WorkflowStatus uses console.RenderStruct correctly
func TestWorkflowStatus_ConsoleRendering(t *testing.T) {
	// Create test data
	statuses := []WorkflowStatus{
		{
			Workflow:      "test-workflow-1",
			EngineID:      "copilot",
			Compiled:      "Yes",
			Status:        "active",
			TimeRemaining: "N/A",
		},
		{
			Workflow:      "test-workflow-2",
			EngineID:      "claude",
			Compiled:      "No",
			Status:        "disabled",
			TimeRemaining: "2h 30m",
		},
	}

	// Render using console.RenderStruct
	output := console.RenderStruct(statuses)

	// Verify the output contains table headers from console tags
	expectedHeaders := []string{"Workflow", "Engine", "Compiled", "Status", "Time Remaining"}
	for _, header := range expectedHeaders {
		if !strings.Contains(output, header) {
			t.Errorf("Expected output to contain header '%s', got:\n%s", header, output)
		}
	}

	// Verify the output contains the data values
	expectedValues := []string{
		"test-workflow-1", "copilot", "Yes", "active",
		"test-workflow-2", "claude", "No", "disabled", "2h 30m",
	}
	for _, value := range expectedValues {
		if !strings.Contains(output, value) {
			t.Errorf("Expected output to contain value '%s', got:\n%s", value, output)
		}
	}

	// Verify it's formatted as a table (contains separators)
	if !strings.Contains(output, "-") {
		t.Error("Expected table output to contain separator lines")
	}
}

// TestWorkflowStatus_JSONMarshalingWithRunStatus tests that RunStatus and RunConclusion are included in JSON output
func TestWorkflowStatus_JSONMarshalingWithRunStatus(t *testing.T) {
	// Test that WorkflowStatus with run status can be marshaled to JSON
	status := WorkflowStatus{
		Workflow:      "test-workflow",
		EngineID:      "copilot",
		Compiled:      "Yes",
		Status:        "active",
		TimeRemaining: "N/A",
		RunStatus:     "completed",
		RunConclusion: "success",
	}

	jsonBytes, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Failed to marshal WorkflowStatus: %v", err)
	}

	// Verify JSON contains run status fields
	var unmarshaled map[string]any
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if unmarshaled["run_status"] != "completed" {
		t.Errorf("Expected run_status='completed', got %v", unmarshaled["run_status"])
	}
	if unmarshaled["run_conclusion"] != "success" {
		t.Errorf("Expected run_conclusion='success', got %v", unmarshaled["run_conclusion"])
	}
}

// TestWorkflowStatus_JSONMarshalingWithEmptyRunStatus tests that empty RunStatus and RunConclusion are omitted
func TestWorkflowStatus_JSONMarshalingWithEmptyRunStatus(t *testing.T) {
	// Test that WorkflowStatus without run status omits those fields
	status := WorkflowStatus{
		Workflow:      "test-workflow",
		EngineID:      "copilot",
		Compiled:      "Yes",
		Status:        "active",
		TimeRemaining: "N/A",
		// RunStatus and RunConclusion are empty
	}

	jsonBytes, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Failed to marshal WorkflowStatus: %v", err)
	}

	// Verify JSON omits empty run status fields (due to omitempty)
	var unmarshaled map[string]any
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if _, exists := unmarshaled["run_status"]; exists {
		t.Errorf("Expected run_status to be omitted when empty, but it was present with value: %v", unmarshaled["run_status"])
	}
	if _, exists := unmarshaled["run_conclusion"]; exists {
		t.Errorf("Expected run_conclusion to be omitted when empty, but it was present with value: %v", unmarshaled["run_conclusion"])
	}
}

// TestWorkflowStatus_JSONMarshalingWithLabels tests that labels are included in JSON output
func TestWorkflowStatus_JSONMarshalingWithLabels(t *testing.T) {
	// Test that WorkflowStatus with labels can be marshaled to JSON
	status := WorkflowStatus{
		Workflow:      "test-workflow",
		EngineID:      "copilot",
		Compiled:      "Yes",
		Status:        "active",
		TimeRemaining: "N/A",
		Labels:        []string{"automation", "testing"},
	}

	jsonBytes, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Failed to marshal WorkflowStatus: %v", err)
	}

	// Verify JSON contains labels field
	var unmarshaled map[string]any
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	labels, ok := unmarshaled["labels"].([]any)
	if !ok {
		t.Fatalf("Expected labels to be an array, got %T", unmarshaled["labels"])
	}

	if len(labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(labels))
	}

	if labels[0] != "automation" {
		t.Errorf("Expected first label to be 'automation', got %v", labels[0])
	}
	if labels[1] != "testing" {
		t.Errorf("Expected second label to be 'testing', got %v", labels[1])
	}
}

// TestWorkflowStatus_JSONMarshalingWithEmptyLabels tests that empty labels are omitted
func TestWorkflowStatus_JSONMarshalingWithEmptyLabels(t *testing.T) {
	// Test that WorkflowStatus without labels omits the field
	status := WorkflowStatus{
		Workflow:      "test-workflow",
		EngineID:      "copilot",
		Compiled:      "Yes",
		Status:        "active",
		TimeRemaining: "N/A",
		// Labels is empty/nil
	}

	jsonBytes, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Failed to marshal WorkflowStatus: %v", err)
	}

	// Verify JSON omits empty labels field (due to omitempty)
	var unmarshaled map[string]any
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if _, exists := unmarshaled["labels"]; exists {
		t.Errorf("Expected labels to be omitted when empty, but it was present with value: %v", unmarshaled["labels"])
	}
}

// TestWorkflowStatus_ConsoleRenderingWithRunStatus tests that RunStatus and RunConclusion are rendered when present
func TestWorkflowStatus_ConsoleRenderingWithRunStatus(t *testing.T) {
	// Create test data with run status
	statuses := []WorkflowStatus{
		{
			Workflow:      "test-workflow-1",
			EngineID:      "copilot",
			Compiled:      "Yes",
			Status:        "active",
			TimeRemaining: "N/A",
			RunStatus:     "completed",
			RunConclusion: "success",
		},
		{
			Workflow:      "test-workflow-2",
			EngineID:      "claude",
			Compiled:      "No",
			Status:        "disabled",
			TimeRemaining: "2h 30m",
			RunStatus:     "completed",
			RunConclusion: "failure",
		},
	}

	// Render using console.RenderStruct
	output := console.RenderStruct(statuses)

	// Verify the output contains run status headers
	expectedHeaders := []string{"Workflow", "Engine", "Compiled", "Status", "Time Remaining", "Run Status", "Run Conclusion"}
	for _, header := range expectedHeaders {
		if !strings.Contains(output, header) {
			t.Errorf("Expected output to contain header '%s', got:\n%s", header, output)
		}
	}

	// Verify the output contains the run status values
	expectedValues := []string{
		"completed", "success", "failure",
	}
	for _, value := range expectedValues {
		if !strings.Contains(output, value) {
			t.Errorf("Expected output to contain value '%s', got:\n%s", value, output)
		}
	}
}

// TestStatusWorkflows_WithRepoOverride tests that the repoOverride parameter is accepted
func TestStatusWorkflows_WithRepoOverride(t *testing.T) {
	// This test verifies that the function accepts the repoOverride parameter
	// and doesn't error out. It should work in the current repository context.
	err := StatusWorkflows("", false, true, "", "", "")
	if err != nil {
		t.Errorf("StatusWorkflows with empty repoOverride should not error: %v", err)
	}

	// Test with a non-empty repo override (will fail gracefully if repo doesn't exist)
	// We expect this to either succeed or fail gracefully without panicking
	_ = StatusWorkflows("", false, true, "", "", "nonexistent/repo")
	// Note: We don't check error here because it's expected to fail for a nonexistent repo
	// The important part is that the parameter is accepted and used
}
