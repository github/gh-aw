//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestTempDirectoryBeforeCustomSteps verifies that the temp directory creation
// step is added BEFORE any custom steps, ensuring custom steps can use /tmp/gh-aw/
func TestTempDirectoryBeforeCustomSteps(t *testing.T) {
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
steps:
  - name: My custom step
    run: |
      echo "Using temp directory"
      ls -la /tmp/gh-aw/ || echo "Directory does not exist"
---

# Test workflow with custom steps
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "temp-dir-order-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflows directory
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write test workflow file
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read generated lock file
	lockPath := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// Extract the agent job section
	agentJobStart := strings.Index(lockStr, "  agent:")
	if agentJobStart == -1 {
		t.Fatal("Could not find agent job in compiled workflow")
	}

	// Find the next job (starts with "  " followed by a non-space character)
	remainingContent := lockStr[agentJobStart+10:]
	nextJobStart := -1
	lines := strings.Split(remainingContent, "\n")
	for i, line := range lines {
		// A new job starts with exactly 2 spaces followed by a letter/number (not more spaces)
		if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && line[2] != '\t' {
			// Calculate the position in the original string
			nextJobStart = 0
			for j := 0; j < i; j++ {
				nextJobStart += len(lines[j]) + 1 // +1 for newline
			}
			break
		}
	}

	var agentJobSection string
	if nextJobStart == -1 {
		agentJobSection = lockStr[agentJobStart:]
	} else {
		agentJobSection = lockStr[agentJobStart : agentJobStart+10+nextJobStart]
	}

	// Find all step names in order
	stepNames := []string{}
	stepLines := strings.Split(agentJobSection, "\n")
	for _, line := range stepLines {
		// Check if line contains "- name:" (with any amount of leading whitespace)
		if strings.Contains(line, "- name:") {
			// Extract the name part after "- name:"
			parts := strings.SplitN(line, "- name:", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				stepNames = append(stepNames, name)
			}
		}
	}

	t.Logf("Found %d steps: %v", len(stepNames), stepNames)

	// Find the indices of temp directory creation and custom step
	tempDirIndex := -1
	customStepIndex := -1

	for i, name := range stepNames {
		if name == "Create gh-aw temp directory" {
			tempDirIndex = i
		}
		if name == "My custom step" {
			customStepIndex = i
		}
	}

	// Verify both steps were found
	if tempDirIndex == -1 {
		t.Fatal("Could not find 'Create gh-aw temp directory' step in agent job")
	}

	if customStepIndex == -1 {
		t.Fatal("Could not find 'My custom step' in agent job")
	}

	// Verify temp directory creation comes before custom step
	if tempDirIndex >= customStepIndex {
		t.Errorf("Temp directory creation (index %d) should come before custom step (index %d)", tempDirIndex, customStepIndex)
	}

	t.Logf("✓ Temp directory creation (step %d) comes before custom step (step %d)", tempDirIndex+1, customStepIndex+1)
}

// TestTempDirectoryWithCheckoutInCustomSteps verifies the ordering when custom steps
// include a checkout step
func TestTempDirectoryWithCheckoutInCustomSteps(t *testing.T) {
	workflowContent := `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Checkout code
    uses: actions/checkout@v5
  - name: Custom step after checkout
    run: echo "Using /tmp/gh-aw/"
---

# Test workflow with checkout in custom steps
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "temp-dir-checkout-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflows directory
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write test workflow file
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read generated lock file
	lockPath := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// Find indices of key steps in the full content (excluding comment lines where frontmatter is embedded)
	tempDirIndex := indexInNonCommentLines(lockStr, "Create gh-aw temp directory")
	checkoutIndex := indexInNonCommentLines(lockStr, "Checkout code")
	customStepIndex := indexInNonCommentLines(lockStr, "Custom step after checkout")

	// Verify all steps were found
	if tempDirIndex == -1 {
		t.Fatal("Could not find 'Create gh-aw temp directory' step")
	}

	if checkoutIndex == -1 {
		t.Fatal("Could not find 'Checkout code' step")
	}

	if customStepIndex == -1 {
		t.Fatal("Could not find 'Custom step after checkout'")
	}

	// Verify ordering: temp directory -> checkout -> custom step
	if tempDirIndex >= checkoutIndex {
		t.Errorf("Temp directory creation should come before checkout")
	}

	if checkoutIndex >= customStepIndex {
		t.Errorf("Checkout should come before custom step")
	}

	t.Logf("✓ Correct ordering: temp directory -> checkout -> custom step")
}
