//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPRCheckout verifies that PR branch checkout is added for pull_request events
func TestPRCheckout(t *testing.T) {
	tests := []struct {
		name             string
		workflowContent  string
		expectPRCheckout bool
	}{
		{
			name: "pull_request with ready_for_review should add checkout",
			workflowContent: `---
on:
  pull_request:
    types: [ready_for_review]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
---

# Test Workflow
Test workflow with pull_request ready_for_review trigger.
`,
			expectPRCheckout: true,
		},
		{
			name: "pull_request with opened should add checkout",
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
---

# Test Workflow
Test workflow with pull_request opened trigger.
`,
			expectPRCheckout: true,
		},
		{
			name: "push trigger should add checkout (with condition)",
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
---

# Test Workflow
Test workflow with push trigger.
`,
			expectPRCheckout: true, // Step is added, but condition prevents execution
		},
		{
			name: "no contents permission should NOT add checkout",
			workflowContent: `---
on:
  pull_request:
    types: [ready_for_review]
permissions:
  issues: read
  contents: read
  pull-requests: read
engine: codex
strict: false
---

# Test Workflow
Test workflow without checkout (has permissions but checkout should be conditional).
`,
			expectPRCheckout: true, // Changed: now has contents permission, so checkout is added
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
			workflowsDir := filepath.Join(tempDir, ".github", "workflows")
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

			// Check for PR checkout step
			hasPRCheckout := strings.Contains(lockStr, "Checkout PR branch")
			if hasPRCheckout != tt.expectPRCheckout {
				t.Errorf("Expected PR checkout step: %v, got: %v", tt.expectPRCheckout, hasPRCheckout)
			}

			// If PR checkout is expected, verify it uses actions/github-script with require()
			if tt.expectPRCheckout {
				// Check for actions/github-script usage
				if !strings.Contains(lockStr, "uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3") {
					t.Error("PR checkout step should use actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3")
				}
				// Check for require() pattern to load the checkout module
				if !strings.Contains(lockStr, "require(") {
					t.Error("PR checkout step should load module via require()")
				}
				if !strings.Contains(lockStr, "checkout_pr_branch.cjs") {
					t.Error("PR checkout step should reference checkout_pr_branch.cjs module")
				}
			}
		})
	}
}

// TestPRCheckoutForAllPullRequestTypes verifies the conditional logic for PR checkout on all pull_request types
func TestPRCheckoutForAllPullRequestTypes(t *testing.T) {
	workflowContent := `---
on:
  pull_request:
    types: [ready_for_review, opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
---

# Test Workflow
Test workflow with pull_request triggers.
`

	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "pr-checkout-logic-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create workflows directory
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
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

	// Verify the checkout uses actions/github-script
	if !strings.Contains(lockStr, "uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3") {
		t.Error("Expected PR checkout to use actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3")
	}

	// Verify JavaScript loads the checkout module via require()
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

// TestPullRequestTargetCheckoutDisabledByDefault verifies that pull_request_target workflows
// do not generate a "Checkout PR branch" step when checkout is not explicitly configured.
// This prevents the step from hard-failing when head branches are deleted (merged PRs) or
// inaccessible (fork PRs).
func TestPullRequestTargetCheckoutDisabledByDefault(t *testing.T) {
	tests := []struct {
		name             string
		workflowContent  string
		expectPRCheckout bool
		description      string
	}{
		{
			name: "pull_request_target with no checkout key - no PR checkout step",
			workflowContent: `---
on:
  pull_request_target:
    types: [closed]
permissions:
  contents: read
  pull-requests: read
engine: claude
strict: false
---

# Thank you note workflow
Workflow triggered when a PR is closed.
`,
			expectPRCheckout: false,
			description:      "pull_request_target without explicit checkout key should not generate 'Checkout PR branch' step",
		},
		{
			name: "pull_request_target with checkout: false - no PR checkout step",
			workflowContent: `---
on:
  pull_request_target:
    types: [closed]
permissions:
  contents: read
  pull-requests: read
engine: claude
strict: false
checkout: false
---

# Thank you note workflow
Workflow triggered when a PR is closed.
`,
			expectPRCheckout: false,
			description:      "pull_request_target with explicit checkout: false should not generate 'Checkout PR branch' step",
		},
		{
			name: "pull_request_target with trusted checkout mapping - no PR checkout step",
			workflowContent: `---
on:
  pull_request_target:
    types: [opened]
permissions:
  contents: read
  pull-requests: read
engine: claude
checkout:
  ref: ${{ github.event.pull_request.base.sha }}
---

# PR review workflow
Workflow triggered when a PR is opened with a trusted base checkout.
`,
			expectPRCheckout: false,
			description:      "pull_request_target with explicit checkout mapping should NOT generate 'Checkout PR branch' step (checkout_pr_branch.cjs fetches refs/pull/<n>/head, not the trusted base SHA)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir, err := os.MkdirTemp("", "prt-checkout-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tempDir)

			// Create workflows directory
			workflowsDir := filepath.Join(tempDir, ".github", "workflows")
			if err := os.MkdirAll(workflowsDir, 0755); err != nil {
				t.Fatalf("Failed to create workflows directory: %v", err)
			}

			// Write test workflow file
			workflowPath := filepath.Join(workflowsDir, "test-prt-workflow.md")
			if err := os.WriteFile(workflowPath, []byte(tt.workflowContent), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile workflow
			compiler := NewCompiler()
			compiler.SetActionMode(ActionModeDev)
			if err := compiler.CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read generated lock file
			lockPath := filepath.Join(workflowsDir, "test-prt-workflow.lock.yml")
			lockContent, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockStr := string(lockContent)

			// Check for PR checkout step
			hasPRCheckout := strings.Contains(lockStr, "Checkout PR branch")
			if hasPRCheckout != tt.expectPRCheckout {
				t.Errorf("%s: expected PR checkout step: %v, got: %v", tt.description, tt.expectPRCheckout, hasPRCheckout)
			}

			// When a PR checkout step IS present, verify it does not fetch the insecure
			// PR-head ref (refs/pull/<n>/head). The trusted-base opt-in must use a safe ref
			// such as the base SHA — checkout_pr_branch.cjs always fetches refs/pull/N/head.
			// We check only for "refs/pull" because that is the specific pattern emitted by
			// checkout_pr_branch.cjs; broader substrings (e.g. "head_ref") could appear in
			// unrelated step names or comments and would produce false positives.
			if hasPRCheckout {
				if strings.Contains(lockStr, "refs/pull") {
					t.Errorf("%s: expected trusted base checkout but lock file contains insecure pattern \"refs/pull\"", tt.description)
				}
			}
		})
	}
}
