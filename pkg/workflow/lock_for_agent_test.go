//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestLockForAgentWorkflow(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "lock-for-agent-test")

	// Create a test markdown file with lock-for-agent enabled
	testContent := `---
on:
  issues:
    types: [opened]
    lock-for-agent: true
  reaction: eyes
engine: copilot
safe-outputs:
  add-comment: {}
---

# Lock For Agent Test

Test workflow with lock-for-agent enabled.
`

	testFile := filepath.Join(tmpDir, "test-lock-for-agent.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify lock-for-agent field is parsed correctly
	if !workflowData.LockForAgent {
		t.Error("Expected LockForAgent to be true")
	}

	// Generate YAML and verify it contains lock/unlock steps
	yamlContent, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Check for lock-specific content in generated YAML
	expectedStrings := []string{
		"Lock issue for agent workflow",
		"Unlock issue after agent workflow",
		"GH_AW_LOCK_FOR_AGENT: \"true\"",
		"lock-issue.cjs",   // Check for require() call to lock-issue script
		"unlock-issue.cjs", // Check for require() call to unlock-issue script
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(yamlContent, expected) {
			t.Errorf("Generated YAML does not contain expected string: %s", expected)
		}
	}

	// Verify lock step is in activation job
	activationJobSection := extractJobSection(yamlContent, "activation")
	if !strings.Contains(activationJobSection, "Lock issue for agent workflow") {
		t.Error("Activation job should contain the lock step")
	}

	// Verify unlock step is in conclusion job
	conclusionJobSection := extractJobSection(yamlContent, "conclusion")
	if !strings.Contains(conclusionJobSection, "Unlock issue after agent workflow") {
		t.Error("Conclusion job should contain the unlock step")
	}

	// Verify unlock step has always() condition
	if !strings.Contains(conclusionJobSection, "if: (always())") {
		t.Error("Unlock step should have always() condition")
	}
}

func TestLockForAgentWithoutReaction(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "lock-for-agent-no-reaction-test")

	// Create a test markdown file with lock-for-agent but no reaction
	testContent := `---
on:
  issues:
    types: [opened]
    lock-for-agent: true
engine: copilot
safe-outputs:
  add-comment: {}
---

# Lock For Agent Test Without Reaction

Test workflow with lock-for-agent but no reaction.
`

	testFile := filepath.Join(tmpDir, "test-lock-no-reaction.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify lock-for-agent field is parsed correctly
	if !workflowData.LockForAgent {
		t.Error("Expected LockForAgent to be true")
	}

	// Generate YAML and verify it contains lock/unlock steps
	yamlContent, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Lock and unlock steps should still be present
	if !strings.Contains(yamlContent, "Lock issue for agent workflow") {
		t.Error("Generated YAML should contain lock step even without reaction")
	}

	if !strings.Contains(yamlContent, "Unlock issue after agent workflow") {
		t.Error("Generated YAML should contain unlock step even without reaction")
	}

	// The GH_AW_LOCK_FOR_AGENT env var should not be set (no reaction step to set it)
	if strings.Contains(yamlContent, "GH_AW_LOCK_FOR_AGENT: \"true\"") {
		t.Error("Generated YAML should not set GH_AW_LOCK_FOR_AGENT env var without reaction step")
	}

	// Verify activation job has issues: write permission for locking
	activationJobSection := extractJobSection(yamlContent, "activation")
	if !strings.Contains(activationJobSection, "issues: write") {
		t.Error("Activation job should have issues: write permission when lock-for-agent is enabled")
	}

	// Verify conclusion job has issues: write permission for unlocking
	conclusionJobSection := extractJobSection(yamlContent, "conclusion")
	if !strings.Contains(conclusionJobSection, "issues: write") {
		t.Error("Conclusion job should have issues: write permission when lock-for-agent is enabled")
	}
}

func TestLockForAgentDisabled(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "lock-for-agent-disabled-test")

	// Create a test markdown file without lock-for-agent
	testContent := `---
on:
  issues:
    types: [opened]
  reaction: eyes
engine: copilot
safe-outputs:
  add-comment: {}
---

# Test Without Lock For Agent

Test workflow without lock-for-agent.
`

	testFile := filepath.Join(tmpDir, "test-no-lock.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify lock-for-agent field is false by default
	if workflowData.LockForAgent {
		t.Error("Expected LockForAgent to be false by default")
	}

	// Generate YAML and verify it does not contain lock/unlock steps
	yamlContent, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Lock and unlock steps should not be present
	if strings.Contains(yamlContent, "Lock issue for agent workflow") {
		t.Error("Generated YAML should not contain lock step when lock-for-agent is disabled")
	}

	if strings.Contains(yamlContent, "Unlock issue after agent workflow") {
		t.Error("Generated YAML should not contain unlock step when lock-for-agent is disabled")
	}

	// The JavaScript code checking for GH_AW_LOCK_FOR_AGENT will still be in the script,
	// but the environment variable itself should not be set
	if strings.Contains(yamlContent, "GH_AW_LOCK_FOR_AGENT: \"true\"") {
		t.Error("Generated YAML should not set GH_AW_LOCK_FOR_AGENT env var when lock-for-agent is disabled")
	}

	// Verify activation job has issues: write permission due to reaction (not lock-for-agent)
	activationJobSection := extractJobSection(yamlContent, "activation")
	if !strings.Contains(activationJobSection, "issues: write") {
		t.Error("Activation job should have issues: write permission when reaction is enabled")
	}
}

func TestLockForAgentDisabledWithoutReaction(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "lock-disabled-no-reaction-test")

	// Create a test markdown file without lock-for-agent and without reaction
	testContent := `---
on:
  issues:
    types: [opened]
engine: copilot
safe-outputs:
  add-comment: {}
---

# Test Without Lock For Agent and Without Reaction

Test workflow without lock-for-agent and without reaction.
`

	testFile := filepath.Join(tmpDir, "test-no-lock-no-reaction.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify lock-for-agent field is false by default
	if workflowData.LockForAgent {
		t.Error("Expected LockForAgent to be false by default")
	}

	// Generate YAML and verify it does not contain lock/unlock steps
	yamlContent, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Lock and unlock steps should not be present
	if strings.Contains(yamlContent, "Lock issue for agent workflow") {
		t.Error("Generated YAML should not contain lock step when lock-for-agent is disabled")
	}

	if strings.Contains(yamlContent, "Unlock issue after agent workflow") {
		t.Error("Generated YAML should not contain unlock step when lock-for-agent is disabled")
	}

	// Verify activation job does NOT have issues: write permission (no reaction and no lock-for-agent)
	activationJobSection := extractJobSection(yamlContent, "activation")
	if strings.Contains(activationJobSection, "issues: write") {
		t.Error("Activation job should NOT have issues: write permission when lock-for-agent is disabled and no reaction is configured")
	}
}

func TestLockForAgentOnPullRequest(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "lock-for-agent-pr-test")

	// Create a test markdown file with pull_request event (should not cause errors)
	testContent := `---
on:
  pull_request:
    types: [opened]
  reaction: eyes
engine: copilot
safe-outputs:
  add-comment: {}
---

# Test Lock For Agent with PR

Test that lock-for-agent on issues doesn't break PR workflows.
`

	testFile := filepath.Join(tmpDir, "test-pr.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Generate YAML - should succeed without errors
	yamlContent, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Lock steps should not be present for PR event (no lock-for-agent in on.pull_request)
	if strings.Contains(yamlContent, "Lock issue for agent workflow") {
		t.Error("Generated YAML should not contain lock step for pull_request event")
	}
}

func TestLockForAgentWithIssueComment(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "lock-for-agent-issue-comment-test")

	// Create a test markdown file with lock-for-agent enabled on issue_comment
	testContent := `---
on:
  issue_comment:
    types: [created]
    lock-for-agent: true
  reaction: eyes
engine: copilot
safe-outputs:
  add-comment: {}
---

# Lock For Agent with Issue Comment Test

Test workflow with lock-for-agent enabled for issue_comment events.
`

	testFile := filepath.Join(tmpDir, "test-lock-for-agent-issue-comment.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify lock-for-agent field is parsed correctly
	if !workflowData.LockForAgent {
		t.Error("Expected LockForAgent to be true")
	}

	// Generate YAML and verify it contains lock/unlock steps
	yamlContent, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Check for lock-specific content in generated YAML
	expectedStrings := []string{
		"Lock issue for agent workflow",
		"Unlock issue after agent workflow",
		"GH_AW_LOCK_FOR_AGENT: \"true\"",
		"lock-issue.cjs",   // Check for require() call to lock-issue script
		"unlock-issue.cjs", // Check for require() call to unlock-issue script
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(yamlContent, expected) {
			t.Errorf("Generated YAML does not contain expected string: %s", expected)
		}
	}

	// Verify lock step is in activation job
	activationJobSection := extractJobSection(yamlContent, "activation")
	if !strings.Contains(activationJobSection, "Lock issue for agent workflow") {
		t.Error("Activation job should contain the lock step")
	}

	// Verify lock condition includes issue_comment
	if !strings.Contains(activationJobSection, "github.event_name == 'issue_comment'") {
		t.Error("Lock step condition should check for issue_comment event")
	}

	// Verify unlock step is in conclusion job
	conclusionJobSection := extractJobSection(yamlContent, "conclusion")
	if !strings.Contains(conclusionJobSection, "Unlock issue after agent workflow") {
		t.Error("Conclusion job should contain the unlock step")
	}

	// Verify unlock condition includes issue_comment
	if !strings.Contains(conclusionJobSection, "github.event_name == 'issue_comment'") {
		t.Error("Unlock step condition should check for issue_comment event")
	}

	// Verify unlock step has always() condition
	if !strings.Contains(conclusionJobSection, "if: (always())") {
		t.Error("Unlock step should have always() condition")
	}

	// Verify activation job has issues: write permission
	if !strings.Contains(activationJobSection, "issues: write") {
		t.Error("Activation job should have issues: write permission when lock-for-agent is enabled")
	}

	// Verify conclusion job has issues: write permission
	if !strings.Contains(conclusionJobSection, "issues: write") {
		t.Error("Conclusion job should have issues: write permission when lock-for-agent is enabled")
	}
}

func TestLockForAgentCommentedInYAML(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "lock-for-agent-commented-test")

	// Create a test markdown file with lock-for-agent enabled
	testContent := `---
on:
  issues:
    types: [opened]
    lock-for-agent: true
  issue_comment:
    types: [created]
    lock-for-agent: true
engine: copilot
safe-outputs:
  add-comment: {}
---

# Lock For Agent Commented Test

Test that lock-for-agent is commented out in generated YAML.
`

	testFile := filepath.Join(tmpDir, "test-lock-for-agent-commented.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify lock-for-agent field is parsed correctly
	if !workflowData.LockForAgent {
		t.Error("Expected LockForAgent to be true")
	}

	// Generate YAML
	yamlContent, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Verify lock-for-agent is commented out in the on section
	expectedComments := []string{
		"# lock-for-agent: true # Lock-for-agent processed as issue locking in activation job",
	}

	for _, comment := range expectedComments {
		if !strings.Contains(yamlContent, comment) {
			t.Errorf("Generated YAML should contain commented lock-for-agent: %s", comment)
		}
	}

	// Verify lock-for-agent does not appear uncommented
	// Look for lines that have "lock-for-agent:" but not starting with "#"
	lines := strings.Split(yamlContent, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Check if line contains "lock-for-agent:" but is not a comment
		if strings.Contains(trimmed, "lock-for-agent:") && !strings.HasPrefix(trimmed, "#") {
			t.Errorf("Line %d contains uncommented lock-for-agent: %s", i+1, line)
		}
	}
}

func TestLockForAgentUnlocksInSafeOutputsJob(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "lock-for-agent-safe-outputs-test")

	// Create a test markdown file with lock-for-agent and safe-outputs
	testContent := `---
on:
  issues:
    types: [opened]
    lock-for-agent: true
  reaction: eyes
engine: copilot
safe-outputs:
  add-comment:
    max: 5
---

# Lock For Agent with Safe Outputs Test

Test that issue is unlocked in safe_outputs job before processing safe outputs.
`

	testFile := filepath.Join(tmpDir, "test-lock-for-agent-safe-outputs.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	// Verify lock-for-agent field is parsed correctly
	if !workflowData.LockForAgent {
		t.Error("Expected LockForAgent to be true")
	}

	// Generate YAML
	yamlContent, err := compiler.generateYAML(workflowData, testFile)
	if err != nil {
		t.Fatalf("Failed to generate YAML: %v", err)
	}

	// Check for lock and unlock steps in the workflow
	expectedStrings := []string{
		"Lock issue for agent workflow",                  // In activation job
		"Unlock issue after agent workflow",              // In conclusion job
		"Unlock issue for safe output operations",        // NEW: In safe_outputs job
		"id: unlock-issue-for-safe-outputs",              // NEW: Step ID
		"needs.activation.outputs.issue_locked == 'true", // Condition check
		"unlock-issue.cjs",                               // Script reference
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(yamlContent, expected) {
			t.Errorf("Generated YAML does not contain expected string: %s", expected)
		}
	}

	// Verify lock step is in activation job
	activationJobSection := extractJobSection(yamlContent, "activation")
	if !strings.Contains(activationJobSection, "Lock issue for agent workflow") {
		t.Error("Activation job should contain the lock step")
	}

	// Verify unlock step is in safe_outputs job
	safeOutputsJobSection := extractJobSection(yamlContent, "safe_outputs")
	if !strings.Contains(safeOutputsJobSection, "Unlock issue for safe output operations") {
		t.Error("safe_outputs job should contain the unlock step")
	}

	// Verify the unlock step appears BEFORE the safe output processing steps
	unlockPos := strings.Index(safeOutputsJobSection, "Unlock issue for safe output operations")
	processPos := strings.Index(safeOutputsJobSection, "Process Safe Outputs")
	if unlockPos == -1 || processPos == -1 {
		t.Error("Both unlock and process steps should exist in safe_outputs job")
	} else if unlockPos > processPos {
		t.Error("Unlock step should appear BEFORE Process Safe Outputs step")
	}

	// Verify unlock step in conclusion job still exists
	conclusionJobSection := extractJobSection(yamlContent, "conclusion")
	if !strings.Contains(conclusionJobSection, "Unlock issue after agent workflow") {
		t.Error("Conclusion job should still contain the unlock step")
	}

	// Verify conclusion unlock has always() condition
	if !strings.Contains(conclusionJobSection, "if: (always())") {
		t.Error("Conclusion unlock step should have always() condition")
	}
}
