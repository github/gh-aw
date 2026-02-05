//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCheckoutOptimization(t *testing.T) {
	// Representative sample of checkout optimization scenarios
	tests := []struct {
		name                string
		frontmatter         string
		expectedHasCheckout bool
		description         string
	}{
		{
			name: "no permissions specified - agent job gets contents:read in dev mode",
			frontmatter: `---
on:
  issues:
    types: [opened]
tools:
  github:
    toolsets: [issues]
engine: claude
strict: false
---`,
			expectedHasCheckout: true,
			description:         "When no permissions are specified, agent job adds contents:read in dev mode for local actions",
		},
		{
			name: "permissions without contents should include checkout for .github access",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  issues: write
  pull-requests: read
tools:
  github:
    toolsets: [issues, pull_requests]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---`,
			expectedHasCheckout: true,
			description:         "Even when permissions don't include contents, checkout is added for .github and .actions access",
		},
		{
			name: "permissions with contents read should include checkout",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
tools:
  github:
    toolsets: [repos, issues, pull_requests]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---`,
			expectedHasCheckout: true,
			description:         "When permissions include contents: read, checkout should be included",
		},
		{
			name: "custom steps with checkout should omit default checkout",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
steps:
  - name: Custom checkout
    uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd
    with:
      token: ${{ secrets.CUSTOM_TOKEN }}
  - name: Setup
    run: echo "custom setup"
tools:
  github:
    toolsets: [issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---`,
			expectedHasCheckout: false,
			description:         "When custom steps already contain checkout, default checkout should be omitted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "checkout-optimization-test")

			// Create test workflow file
			testContent := tt.frontmatter + "\n\n# Test Workflow\n\nThis is a test workflow to check checkout optimization.\n"
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			// Compile the workflow
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Calculate the lock file path
			lockFile := stringutil.MarkdownToLockFile(testFile)

			// Read the generated lock file
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContentStr := string(lockContent)

			// For the test case with custom checkout, we need to verify that
			// only the custom checkout is present, not a default generated one
			if tt.name == "custom steps with checkout should omit default checkout" {
				// Check that the custom checkout with token is present
				hasCustomCheckout := strings.Contains(lockContentStr, "token: ${{ secrets.CUSTOM_TOKEN }}")
				// Check that there's no "Checkout repository" step (which is the default name)
				hasDefaultCheckout := strings.Contains(lockContentStr, "name: Checkout repository")

				if !hasCustomCheckout {
					t.Errorf("%s: Custom checkout with token not found", tt.description)
				}
				if hasDefaultCheckout {
					t.Errorf("%s: Default checkout step should not be present when custom steps have checkout", tt.description)
				}
			} else {
				// For other test cases, check if checkout step is present in the agent job
				// Extract the agent job section
				agentJobStart := strings.Index(lockContentStr, "agent:")
				if agentJobStart == -1 {
					t.Fatalf("Agent job not found in compiled workflow")
				}

				// Find the next job or end of file to bound the agent job section
				agentJobEnd := len(lockContentStr)
				nextJobIdx := strings.Index(lockContentStr[agentJobStart+6:], "\n  ")
				if nextJobIdx != -1 {
					// Look for the start of the next job (a line starting with two spaces followed by a word and colon)
					searchStart := agentJobStart + 6 + nextJobIdx
					for idx := searchStart; idx < len(lockContentStr); idx++ {
						if lockContentStr[idx] == '\n' {
							// Check if the next line starts a new job (at same indentation level as "agent:")
							lineStart := idx + 1
							if lineStart < len(lockContentStr) && lineStart+2 < len(lockContentStr) {
								if lockContentStr[lineStart:lineStart+2] == "  " && lockContentStr[lineStart+2] != ' ' {
									// Found a line that starts with exactly 2 spaces (not more)
									// and has a non-space character after, indicating a new job
									colonIdx := strings.Index(lockContentStr[lineStart:], ":")
									if colonIdx > 0 && colonIdx < 50 { // Job names are typically short
										agentJobEnd = idx
										break
									}
								}
							}
						}
					}
				}

				agentJobSection := lockContentStr[agentJobStart:agentJobEnd]
				// Check for repository checkout specifically (not actions folder checkout for local actions)
				hasCheckout := strings.Contains(agentJobSection, "Checkout repository")

				if hasCheckout != tt.expectedHasCheckout {
					t.Errorf("%s: Expected hasCheckout=%v in agent job, got %v\nAgent job section:\n%s",
						tt.description, tt.expectedHasCheckout, hasCheckout, agentJobSection)
				}
			}
		})
	}
}

func TestShouldAddCheckoutStep(t *testing.T) {
	tests := []struct {
		name        string
		permissions string
		customSteps string
		expected    bool
	}{
		{
			name:        "default permissions should include checkout",
			permissions: "permissions: read-all", // Default applied by compiler
			customSteps: "",
			expected:    true,
		},
		{
			name:        "contents read permission specified, no custom steps",
			permissions: "permissions:\n  contents: read",
			customSteps: "",
			expected:    true,
		},
		{
			name:        "contents write permission specified, no custom steps",
			permissions: "permissions:\n  contents: write",
			customSteps: "",
			expected:    true,
		},
		{
			name:        "no contents permission specified, no custom steps - checkout added for .github access",
			permissions: "permissions:\n  issues: write",
			customSteps: "",
			expected:    true,
		},
		{
			name:        "contents read permission, custom steps with checkout",
			permissions: "permissions:\n  contents: read",
			customSteps: "steps:\n  - uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd",
			expected:    false,
		},
		{
			name:        "contents read permission, custom steps without checkout",
			permissions: "permissions:\n  contents: read",
			customSteps: "steps:\n  - uses: actions/setup-node@v6",
			expected:    true,
		},
		{
			name:        "read-all shorthand permission specified",
			permissions: "permissions: read-all",
			customSteps: "",
			expected:    true,
		},
		{
			name:        "write-all shorthand permission specified",
			permissions: "permissions: write-all",
			customSteps: "",
			expected:    true,
		},
	}

	compiler := NewCompiler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				Permissions: tt.permissions,
				CustomSteps: tt.customSteps,
			}

			result := compiler.shouldAddCheckoutStep(data)
			if result != tt.expected {
				t.Errorf("shouldAddCheckoutStep() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
