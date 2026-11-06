//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestCreateIssueHandlerConfigIncludesAssignees verifies that the assignees field
// is properly passed to the handler config JSON (GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG)
func TestCreateIssueHandlerConfigIncludesAssignees(t *testing.T) {
	tmpDir := testutil.TempDir(t, "handler-config-test")

	testContent := `---
name: Test Handler Config
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 1
    labels: [test-label]
    allowed-fields: [Priority, Iteration]
    max-issue-artifacts: 3
    title-prefix: "[Test] "
    assignees: [user1, user2]
---

Create an issue with title "Test" and body "Test body".
`

	testFile := filepath.Join(tmpDir, "test-handler-config.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-handler-config.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read compiled output: %v", err)
	}

	compiledStr := string(compiledContent)

	// Find the GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG line
	if !strings.Contains(compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Fatal("Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG in compiled workflow")
	}

	// Extract the JSON config from the YAML
	// Format: GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: "{...json...}"
	lines := strings.Split(compiledStr, "\n")
	var configJSON string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:") {
			// Extract JSON from the line
			parts := strings.SplitN(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:", 2)
			if len(parts) == 2 {
				configJSON = strings.TrimSpace(parts[1])
				// Remove surrounding quotes
				configJSON = strings.Trim(configJSON, "\"")
				// Unescape JSON
				configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				break
			}
		}
	}

	if configJSON == "" {
		t.Fatal("Could not extract handler config JSON")
	}

	// Parse the JSON
	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("Failed to parse handler config JSON: %v\nJSON: %s", err, configJSON)
	}

	// Verify create_issue config exists
	createIssueConfig, ok := config["create_issue"].(map[string]any)
	if !ok {
		t.Fatal("Expected create_issue in handler config")
	}

	// Verify max is present
	if max, ok := createIssueConfig["max"].(float64); !ok || max != 1 {
		t.Errorf("Expected max=1 in create_issue config, got: %v", createIssueConfig["max"])
	}
	if max, ok := createIssueConfig["max_issue_artifacts"].(float64); !ok || max != 3 {
		t.Errorf("Expected max_issue_artifacts=3, got: %v", createIssueConfig["max_issue_artifacts"])
	}

	// Verify labels are present
	labels, ok := createIssueConfig["labels"].([]any)
	if !ok {
		t.Fatal("Expected labels array in create_issue config")
	}

	if len(labels) != 1 || labels[0] != "test-label" {
		t.Errorf("Expected labels=[test-label] in create_issue config, got: %v", labels)
	}

	// Verify title_prefix is present
	if titlePrefix, ok := createIssueConfig["title_prefix"].(string); !ok || titlePrefix != "[Test] " {
		t.Errorf("Expected title_prefix='[Test] ' in create_issue config, got: %v", createIssueConfig["title_prefix"])
	}

	// Verify allowed_fields are present
	allowedFields, ok := createIssueConfig["allowed_fields"].([]any)
	if !ok {
		t.Fatal("Expected allowed_fields array in create_issue config")
	}
	if len(allowedFields) != 2 || allowedFields[0] != "Priority" || allowedFields[1] != "Iteration" {
		t.Errorf("Expected allowed_fields=[Priority, Iteration] in create_issue config, got: %v", allowedFields)
	}

	// Verify assignees are present (this is the main test)
	assignees, ok := createIssueConfig["assignees"].([]any)
	if !ok {
		t.Fatal("Expected assignees array in create_issue config")
	}
	if len(assignees) != 2 {
		t.Errorf("Expected 2 assignees in create_issue config, got: %d", len(assignees))
	}
	if assignees[0] != "user1" || assignees[1] != "user2" {
		t.Errorf("Expected assignees=[user1, user2] in create_issue config, got: %v", assignees)
	}
}

func TestCreateIssueArtifactLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   int
	}{
		{"default", "", 10},
		{"positive", "max-issue-artifacts: 2", 2},
		{"zero", "max-issue-artifacts: 0", 0},
		{"negative", "max-issue-artifacts: -2", -2},
		{"alias", "max-isssue-artifacts: 4", 4},
		{"canonical-precedence", "max-issue-artifacts: 3\n    max-isssue-artifacts: 4", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "artifact-limit")
			path := filepath.Join(dir, "limit.md")
			source := fmt.Sprintf("---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\nsafe-outputs:\n  create-issue:\n    max: 1\n    %s\n---\nCreate an issue with an artifact.\n", tc.config)
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := NewCompiler().CompileWorkflow(path); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(dir, "limit.lock.yml"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.ReplaceAll(string(content), `\"`, `"`), fmt.Sprintf(`"max_issue_artifacts":%d`, tc.want)) {
				t.Errorf("Compiled handler config does not contain max_issue_artifacts=%d", tc.want)
			}
			constraint := "Issue artifacts are disabled; omit artifacts."
			if tc.want > 0 {
				constraint = fmt.Sprintf("Maximum %d artifact(s) can be attached per issue.", tc.want)
			}
			if !strings.Contains(string(content), constraint) {
				t.Errorf("Compiled tool description does not contain %q", constraint)
			}
		})
	}
}

// TestCreateIssueHandlerConfigWithSingleStringAssignee verifies that single string assignee
// is properly converted to an array and passed to handler config
func TestCreateIssueHandlerConfigWithSingleStringAssignee(t *testing.T) {
	tmpDir := testutil.TempDir(t, "single-assignee-test")

	testContent := `---
name: Test Single Assignee
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    assignees: single-user
---

Create an issue.
`

	testFile := filepath.Join(tmpDir, "test-single.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-single.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read compiled output: %v", err)
	}

	compiledStr := string(compiledContent)

	// Extract handler config JSON
	lines := strings.Split(compiledStr, "\n")
	var configJSON string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:") {
			parts := strings.SplitN(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:", 2)
			if len(parts) == 2 {
				configJSON = strings.TrimSpace(parts[1])
				configJSON = strings.Trim(configJSON, "\"")
				configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				break
			}
		}
	}

	if configJSON == "" {
		t.Fatal("Could not extract handler config JSON")
	}

	// Parse the JSON
	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("Failed to parse handler config JSON: %v\nJSON: %s", err, configJSON)
	}

	// Verify assignees are present as array
	createIssueConfig, ok := config["create_issue"].(map[string]any)
	if !ok {
		t.Fatal("Expected create_issue in handler config")
	}

	assignees, ok := createIssueConfig["assignees"].([]any)
	if !ok {
		t.Fatal("Expected assignees array in create_issue config")
	}
	if len(assignees) != 1 {
		t.Errorf("Expected 1 assignee in create_issue config, got: %d", len(assignees))
	}
	if assignees[0] != "single-user" {
		t.Errorf("Expected assignees=[single-user] in create_issue config, got: %v", assignees)
	}
}

// TestCreateIssueHandlerConfigWithoutAssignees verifies that handler config
// doesn't include assignees field when not configured
func TestCreateIssueHandlerConfigWithoutAssignees(t *testing.T) {
	tmpDir := testutil.TempDir(t, "no-assignees-config-test")

	testContent := `---
name: Test Without Assignees
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 1
---

Create an issue.
`

	testFile := filepath.Join(tmpDir, "test-no-assignees.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-no-assignees.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read compiled output: %v", err)
	}

	compiledStr := string(compiledContent)

	// Extract handler config JSON
	lines := strings.Split(compiledStr, "\n")
	var configJSON string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:") {
			parts := strings.SplitN(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:", 2)
			if len(parts) == 2 {
				configJSON = strings.TrimSpace(parts[1])
				configJSON = strings.Trim(configJSON, "\"")
				configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				break
			}
		}
	}

	if configJSON == "" {
		t.Fatal("Could not extract handler config JSON")
	}

	// Parse the JSON
	var config map[string]any
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		t.Fatalf("Failed to parse handler config JSON: %v\nJSON: %s", err, configJSON)
	}

	// Verify create_issue config exists
	createIssueConfig, ok := config["create_issue"].(map[string]any)
	if !ok {
		t.Fatal("Expected create_issue in handler config")
	}

	// Verify assignees field is not present (or is empty)
	if assignees, ok := createIssueConfig["assignees"]; ok {
		if arr, ok := assignees.([]any); ok && len(arr) > 0 {
			t.Errorf("Expected no assignees in create_issue config, got: %v", assignees)
		}
	}
}
