//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestCheckoutRuntimeOrderInCustomSteps verifies that when custom steps contain
// a checkout step, the temp directory is created first, then the checkout step
// runs, and runtime setup steps are inserted AFTER the checkout step. This ensures
// that the temp directory is available to all steps, checkout happens before
// runtime setup, and runtime tools are available to subsequent custom steps.
func TestCheckoutRuntimeOrderInCustomSteps(t *testing.T) {
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
steps:
  - name: Checkout code
    uses: actions/checkout@v5
  - name: Use Node
    run: node --version
---

# Test workflow with checkout in custom steps
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "checkout-runtime-order-test")
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
	compiler.SetActionMode(ActionModeDev) // Use dev mode with local action paths
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

	// Find the next job (starts with "  " followed by a non-space character, e.g., "  activation:")
	// We need to skip the agent job content which has more indentation
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

	// Debug: print first 1000 chars of agent job section
	sampleSize := min(1000, len(agentJobSection))
	t.Logf("Agent job section (first %d chars):\n%s", sampleSize, agentJobSection[:sampleSize])

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

	if len(stepNames) < 5 {
		t.Fatalf("Expected at least 5 steps, got %d: %v", len(stepNames), stepNames)
	}

	// Verify the order in dev mode (when local actions are used):
	// 1. First step should be "Checkout actions folder" (checkout local actions)
	// 2. Second step should be "Setup Scripts" (use the checked out action)
	// 3. Third step should be "Create gh-aw temp directory" (before custom steps)
	// 4. Fourth step should be "Checkout code" (from custom steps - full checkout, no separate .github checkout needed)
	// 5. Fifth step should be "Setup Node.js" (runtime setup, inserted after checkout)
	// 6. Sixth step should be "Use Node" (from custom steps)
	// NOTE: The .github sparse checkout is skipped because custom steps contain a full checkout

	if stepNames[0] != "Checkout actions folder" {
		t.Errorf("First step should be 'Checkout actions folder', got '%s'", stepNames[0])
	}

	if stepNames[1] != "Setup Scripts" {
		t.Errorf("Second step should be 'Setup Scripts', got '%s'", stepNames[1])
	}

	if stepNames[2] != "Create gh-aw temp directory" {
		t.Errorf("Third step should be 'Create gh-aw temp directory', got '%s'", stepNames[2])
	}

	if stepNames[3] != "Checkout code" {
		t.Errorf("Fourth step should be 'Checkout code', got '%s'", stepNames[3])
	}

	if stepNames[4] != "Setup Node.js" {
		t.Errorf("Fifth step should be 'Setup Node.js' (runtime setup after checkout), got '%s'", stepNames[4])
	}

	if stepNames[5] != "Use Node" {
		t.Errorf("Sixth step should be 'Use Node', got '%s'", stepNames[5])
	}

	// Verify that .github checkout is NOT present (redundant with full checkout in custom steps)
	for _, name := range stepNames {
		if name == "Checkout .github folder" {
			t.Error("Checkout .github folder should not be present when custom steps contain full repository checkout")
		}
	}

	// Additional check: verify temp directory creation is first
	tempDirIndex := strings.Index(agentJobSection, "Create gh-aw temp directory")
	checkoutIndex := strings.Index(agentJobSection, "Checkout code")
	setupNodeIndex := strings.Index(agentJobSection, "Setup Node.js")

	if tempDirIndex == -1 {
		t.Fatal("Could not find 'Create gh-aw temp directory' step in agent job")
	}

	if checkoutIndex == -1 {
		t.Fatal("Could not find 'Checkout code' step in agent job")
	}

	if setupNodeIndex == -1 {
		t.Fatal("Could not find 'Setup Node.js' step in agent job")
	}

	if tempDirIndex > checkoutIndex {
		t.Error("Create gh-aw temp directory appears after Checkout code, should be before")
	}

	if setupNodeIndex < checkoutIndex {
		t.Error("Setup Node.js appears before Checkout code, should be after")
	}

	t.Logf("Step order is correct:")
	for i, name := range stepNames[:6] {
		t.Logf("  %d. %s", i+1, name)
	}
}

// TestCheckoutFirstWhenNoCustomSteps verifies that when there are no custom steps,
// the automatic checkout is added first.
func TestCheckoutFirstWhenNoCustomSteps(t *testing.T) {
	workflowContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Test workflow without custom steps

Run node --version to check the Node.js version.
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "checkout-first-test")
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
	compiler.SetActionMode(ActionModeDev) // Use dev mode with local action paths
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

	// Find the next job (starts with "  " followed by a non-space character, e.g., "  activation:")
	// We need to skip the agent job content which has more indentation
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

	if len(stepNames) < 3 {
		t.Fatalf("Expected at least 3 steps, got %d: %v", len(stepNames), stepNames)
	}

	// Verify the order in dev mode:
	// 1. First step should be "Checkout actions folder" (checkout local actions)
	// 2. Second step should be "Setup Scripts" (use the checked out action)
	// 3. Third step should be "Checkout repository" (automatic full checkout - no separate .github checkout needed)
	// NOTE: The .github sparse checkout is skipped when full repository checkout is performed

	if stepNames[0] != "Checkout actions folder" {
		t.Errorf("First step should be 'Checkout actions folder', got '%s'", stepNames[0])
	}

	if stepNames[1] != "Setup Scripts" {
		t.Errorf("Second step should be 'Setup Scripts', got '%s'", stepNames[1])
	}

	if stepNames[2] != "Checkout repository" {
		t.Errorf("Third step should be 'Checkout repository', got '%s'", stepNames[2])
	}

	// Verify that .github checkout is NOT present (redundant with full checkout)
	for _, name := range stepNames {
		if name == "Checkout .github folder" {
			t.Error("Checkout .github folder should not be present when full repository checkout is performed")
		}
	}

	t.Logf("Step order is correct:")
	t.Logf("  1. %s", stepNames[0])
	t.Logf("  2. %s", stepNames[1])
	t.Logf("  3. %s", stepNames[2])
}
