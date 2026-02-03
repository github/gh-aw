//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestPRBranchCheckout verifies that PR branch checkout is added for comment triggers
func TestPRBranchCheckout(t *testing.T) {
	tests := []struct {
		name             string
		workflowContent  string
		expectPRCheckout bool
		expectPRPrompt   bool
	}{
		{
			name: "issue_comment trigger should add PR checkout",
			workflowContent: `---
on:
  issue_comment:
    types: [created]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow
Test workflow with issue_comment trigger.
`,
			expectPRCheckout: true,
			expectPRPrompt:   true,
		},
		{
			name: "pull_request_review_comment trigger should add PR checkout",
			workflowContent: `---
on:
  pull_request_review_comment:
    types: [created]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow
Test workflow with pull_request_review_comment trigger.
`,
			expectPRCheckout: true,
			expectPRPrompt:   true,
		},
		{
			name: "multiple comment triggers should add PR checkout",
			workflowContent: `---
on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow
Test workflow with multiple comment triggers.
`,
			expectPRCheckout: true,
			expectPRPrompt:   true,
		},
		{
			name: "command trigger should add PR checkout (expands to comments)",
			workflowContent: `---
on:
  command:
    name: test-bot
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow
Test workflow with command trigger.
`,
			expectPRCheckout: true,
			expectPRPrompt:   true,
		},
		{
			name: "push trigger should add PR checkout (with runtime condition)",
			workflowContent: `---
on:
  push:
    branches: [main]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow
Test workflow with push trigger only.
`,
			expectPRCheckout: true, // Step is added but runtime condition prevents execution
			expectPRPrompt:   false,
		},
		{
			name: "pull_request trigger should add PR checkout",
			workflowContent: `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow
Test workflow with pull_request trigger only.
`,
			expectPRCheckout: true, // Step is added and will execute for PR events
			expectPRPrompt:   false,
		},
		{
			name: "no contents permission should NOT add PR checkout",
			workflowContent: `---
on:
  issue_comment:
    types: [created]
permissions:
  issues: write
  contents: read
  pull-requests: read
engine: codex
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow
Test workflow with permissions but checkout should be conditional.
`,
			expectPRCheckout: true, // Changed: now has contents permission, so checkout is added
			expectPRPrompt:   true, // Changed: now has permissions, so PR prompt is added
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir, err := os.MkdirTemp("", "pr-checkout-test")
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
			if err := os.WriteFile(workflowPath, []byte(tt.workflowContent), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile workflow
			compiler := NewCompiler()
			// Use dev mode to test with local action paths
			compiler.SetActionMode(ActionModeDev)
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

			// Check for PR checkout step (now uses JavaScript)
			hasPRCheckout := strings.Contains(lockStr, "Checkout PR branch")
			if hasPRCheckout != tt.expectPRCheckout {
				t.Errorf("Expected PR checkout step: %v, got: %v", tt.expectPRCheckout, hasPRCheckout)
			}

			// Check for PR context prompt using cat command
			hasPRPrompt := strings.Contains(lockStr, "cat \"/opt/gh-aw/prompts/pr_context_prompt.md\"")
			if hasPRPrompt != tt.expectPRPrompt {
				t.Errorf("Expected PR context prompt: %v, got: %v", tt.expectPRPrompt, hasPRPrompt)
			}

			// If PR checkout is expected, verify it uses JavaScript with require()
			if tt.expectPRCheckout {
				if !strings.Contains(lockStr, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
					t.Error("PR checkout step should use actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd")
				}
				// In release mode, the script is loaded via require() from the custom action
				if !strings.Contains(lockStr, "require(") {
					t.Error("PR checkout step should load module via require()")
				}
				if !strings.Contains(lockStr, "checkout_pr_branch.cjs") {
					t.Error("PR checkout step should reference checkout_pr_branch.cjs module")
				}
			}

			// If PR prompt is expected, verify the cat command references the correct file
			if tt.expectPRPrompt {
				if !strings.Contains(lockStr, "cat \"/opt/gh-aw/prompts/pr_context_prompt.md\"") {
					t.Error("PR context prompt should reference pr_context_prompt.md file")
				}
			}
		})
	}
}

// TestPRCheckoutConditionalLogic verifies the conditional logic for PR checkout
func TestPRCheckoutConditionalLogic(t *testing.T) {
	workflowContent := `---
on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow
Test workflow with multiple comment triggers.
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "pr-checkout-logic-test")
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

	// Verify the checkout step uses actions/github-script
	if !strings.Contains(lockStr, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
		t.Error("Expected PR checkout to use actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd")
	}

	// Verify JavaScript code loads PR checkout module via require()
	// In dev mode (default), the script is loaded from a file via require()
	expectedPatterns := []string{
		"require(",
		"checkout_pr_branch.cjs",
		"await main()",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(lockStr, pattern) {
			t.Errorf("Expected JavaScript pattern not found: %s", pattern)
		}
	}
}

// TestPRCheckoutGHTokenConfiguration verifies that GH_TOKEN is correctly configured for gh CLI
func TestPRCheckoutGHTokenConfiguration(t *testing.T) {
	workflowContent := `---
on:
  issue_comment:
    types: [created]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
features:
  dangerous-permissions-write: true
---

# Test Workflow
Test workflow to verify GH_TOKEN configuration.
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "pr-checkout-token-test")
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

	// Find the Checkout PR branch step
	checkoutStepIndex := strings.Index(lockStr, "- name: Checkout PR branch")
	if checkoutStepIndex == -1 {
		t.Fatal("Checkout PR branch step not found")
	}

	// Extract the step section (until the next step)
	nextStepIndex := strings.Index(lockStr[checkoutStepIndex+1:], "- name:")
	var stepSection string
	if nextStepIndex == -1 {
		stepSection = lockStr[checkoutStepIndex:]
	} else {
		stepSection = lockStr[checkoutStepIndex : checkoutStepIndex+nextStepIndex]
	}

	// Verify env section with GH_TOKEN exists
	if !strings.Contains(stepSection, "env:") {
		t.Error("Expected env: section in PR checkout step")
	}

	// Verify GH_TOKEN is set in env section
	if !strings.Contains(stepSection, "GH_TOKEN:") {
		t.Error("Expected GH_TOKEN environment variable in PR checkout step")
	}

	// Verify github-token is set in with section
	if !strings.Contains(stepSection, "github-token:") {
		t.Error("Expected github-token parameter in with section of PR checkout step")
	}

	// Verify the token uses the standard fallback pattern
	if !strings.Contains(stepSection, "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}") {
		t.Error("Expected standard token fallback pattern in PR checkout step")
	}

	// Verify JavaScript loads the checkout module via require() in dev mode
	// In dev mode, the script is loaded from a file, not inlined
	if !strings.Contains(stepSection, "require(") {
		t.Error("Expected require() call to load checkout_pr_branch.cjs module")
	}

	if !strings.Contains(stepSection, "checkout_pr_branch.cjs") {
		t.Error("Expected reference to checkout_pr_branch.cjs module")
	}

	// Verify the module's main function is called
	if !strings.Contains(stepSection, "await main()") {
		t.Error("Expected call to main() function from checkout module")
	}
}
