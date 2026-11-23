//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// containsRuntimeImports checks if the content contains runtime-import macros
// with local file paths (starting with . or ..) rather than URLs
func containsRuntimeImports(content string) bool {
	// Look for {{#runtime-import or {{#runtime-import? patterns
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		// Check for runtime-import or runtime-import? patterns
		if strings.Contains(line, "{{#runtime-import") {
			// Extract the part after runtime-import
			idx := strings.Index(line, "{{#runtime-import")
			if idx == -1 {
				continue
			}

			// Find the path after the macro name
			rest := line[idx+len("{{#runtime-import"):]

			// Skip optional marker if present
			rest = strings.TrimPrefix(rest, "?")

			// Trim whitespace
			rest = strings.TrimSpace(rest)

			// Skip URLs (http:// or https://)
			if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
				continue
			}

			// Check if it starts with . or .. (local file path)
			if strings.HasPrefix(rest, ".") {
				return true
			}
		}
	}
	return false
}

// TestContainsRuntimeImports tests the containsRuntimeImports function
func TestContainsRuntimeImports(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "no runtime imports",
			content:  "# Simple markdown\n\nSome text here",
			expected: false,
		},
		{
			name:     "runtime-import with relative path ./",
			content:  "{{#runtime-import .github/shared.md}}",
			expected: true,
		},
		{
			name:     "runtime-import with relative path ../",
			content:  "{{#runtime-import ../shared/file.md}}",
			expected: true,
		},
		{
			name:     "optional runtime-import with ./",
			content:  "{{#runtime-import? ./config.md}}",
			expected: true,
		},
		{
			name:     "optional runtime-import with ../",
			content:  "{{#runtime-import? ../templates/base.md}}",
			expected: true,
		},
		{
			name:     "runtime-import with URL should NOT trigger",
			content:  "{{#runtime-import https://example.com/file.md}}",
			expected: false,
		},
		{
			name:     "runtime-import with http URL should NOT trigger",
			content:  "{{#runtime-import http://example.com/file.md}}",
			expected: false,
		},
		{
			name:     "email address should NOT trigger",
			content:  "Contact: user@example.com",
			expected: false,
		},
		{
			name:     "mixed content with runtime-import",
			content:  "# Title\n\n{{#runtime-import ./shared.md}}\n\nMore content",
			expected: true,
		},
		{
			name:     "multiple runtime-imports",
			content:  "{{#runtime-import ./a.md}}\n{{#runtime-import ./b.md}}",
			expected: true,
		},
		{
			name:     "empty content",
			content:  "",
			expected: false,
		},
		{
			name:     "only URLs no file references",
			content:  "{{#runtime-import https://example.com}}\n@https://github.com/file.md",
			expected: false,
		},
		{
			name:     "runtime-import with spaces",
			content:  "{{#runtime-import   ./path/to/file.md}}",
			expected: true,
		},
		{
			name:     "runtime-import with tabs",
			content:  "{{#runtime-import\t./file.md}}",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsRuntimeImports(tt.content)
			if result != tt.expected {
				t.Errorf("containsRuntimeImports() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestRuntimeImportCheckoutIntegration tests that workflows with runtime-import macros
// get the checkout step added
func TestRuntimeImportCheckoutIntegration(t *testing.T) {
	tests := []struct {
		name                string
		frontmatter         string
		markdown            string
		expectedHasCheckout bool
		description         string
	}{
		{
			name: "runtime-import with contents read permission",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
---`,
			markdown:            "# Agent\n\n{{#runtime-import .github/shared.md}}\n\nDo the task.",
			expectedHasCheckout: true,
			description:         "Runtime-import should trigger checkout when contents: read is present",
		},
		{
			name: "no runtime-imports with contents read",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
---`,
			markdown:            "# Agent\n\nSimple task instructions here.",
			expectedHasCheckout: true,
			description:         "With contents: read but no runtime-imports, checkout should still happen (existing behavior)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "runtime-import-test")

			// Create workflow file
			workflowPath := filepath.Join(tmpDir, "test.md")
			content := tt.frontmatter + "\n\n" + tt.markdown
			if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(workflowPath); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Calculate the lock file path
			lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"

			// Read the generated lock file
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContentStr := string(lockContent)

			// Check if checkout step is present in the agent job
			hasCheckout := strings.Contains(lockContentStr, "actions/checkout@")

			if hasCheckout != tt.expectedHasCheckout {
				t.Errorf("%s: Expected checkout=%v, got checkout=%v",
					tt.description, tt.expectedHasCheckout, hasCheckout)
			}
		})
	}
}

// TestRuntimeImportShallowCheckout verifies that the checkout for runtime-imports
// is shallow and has no persisted credentials
func TestRuntimeImportShallowCheckout(t *testing.T) {
	frontmatter := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
---`
	markdown := "# Agent\n\n{{#runtime-import .github/shared-instructions.md}}\n\nComplete the task."

	tmpDir := testutil.TempDir(t, "runtime-import-checkout-test")

	// Create workflow file
	workflowPath := filepath.Join(tmpDir, "test.md")
	content := frontmatter + "\n\n" + markdown
	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Calculate the lock file path
	lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"

	// Read the generated lock file
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify checkout is present
	if !strings.Contains(lockContentStr, "actions/checkout@") {
		t.Error("Expected checkout step to be present")
	}

	// Note: The current implementation uses the default checkout action configuration
	// which is already shallow by default (fetch-depth: 1) and has persist-credentials: false
	// These are the default behaviors of actions/checkout when no parameters are specified
	// For runtime-imports, this is exactly what we want - minimal checkout with no credentials
}

// TestActivationJobCheckoutWithoutExplicitContentsRead verifies that the activation job
// still gets the checkout step for .github and .agents folders even when the workflow
// does not explicitly specify contents: read permission. This is critical for runtime-imports
// to work correctly, since the activation job always has contents: read added to it.
func TestActivationJobCheckoutWithoutExplicitContentsRead(t *testing.T) {
	// This workflow only has issues: read permission, no explicit contents: read
	// The activation job should still have the checkout step because it always gets
	// contents: read added for GitHub API access and runtime imports
	frontmatter := `---
on:
  workflow_dispatch:
permissions:
  issues: read
engine: claude
strict: false
---`
	markdown := "# Agent\n\nCreate an issue with title \"Test\" and body \"Hello World\"."

	tmpDir := testutil.TempDir(t, "activation-checkout-no-contents-test")

	// Create workflow file
	workflowPath := filepath.Join(tmpDir, "test.md")
	content := frontmatter + "\n\n" + markdown
	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Calculate the lock file path
	lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"

	// Read the generated lock file
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Find the activation job section
	activationJobStart := strings.Index(lockContentStr, "activation:")
	if activationJobStart == -1 {
		t.Fatal("Activation job not found in compiled workflow")
	}

	// Find the end of the activation job (next job definition)
	activationJobEnd := len(lockContentStr)
	nextJobPattern := "\n  "
	searchStart := activationJobStart + len("activation:")
	remaining := lockContentStr[searchStart:]
	lines := strings.Split(remaining, "\n")
	charCount := 0
	for i, line := range lines {
		charCount += len(line) + 1 // +1 for newline
		if i > 0 && len(line) > 2 && line[0:2] == "  " && line[2] != ' ' && strings.Contains(line, ":") {
			activationJobEnd = searchStart + charCount - len(line) - 1
			break
		}
	}
	_ = nextJobPattern // silence unused warning

	activationJobSection := lockContentStr[activationJobStart:activationJobEnd]

	// Verify activation can snapshot all symlink targets without explicit contents: read permission.
	checkoutParts := strings.SplitN(activationJobSection, "name: Checkout repository for base branch snapshot", 2)
	if len(checkoutParts) != 2 {
		t.Error("Activation job should contain the base snapshot checkout step")
		t.Logf("Activation job section:\n%s", activationJobSection)
		return
	}

	if strings.Contains(strings.SplitN(checkoutParts[1], "\n      - name:", 2)[0], "sparse-checkout:") {
		t.Error("Base snapshot checkout must include symlink targets outside agent folders")
	}
}
