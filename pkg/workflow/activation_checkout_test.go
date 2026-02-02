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

// TestActivationJobNoCheckoutStep tests that the activation job uses GitHub API
// instead of checking out the repository for the timestamp check
func TestActivationJobNoCheckoutStep(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		description string
	}{
		{
			name: "basic workflow has no checkout in activation",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---`,
			description: "Activation job should not include checkout step - uses GitHub API instead",
		},
		{
			name: "workflow without contents permission has no checkout in activation",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---`,
			description: "Activation job should not include checkout - uses GitHub API instead",
		},
		{
			name: "workflow with reaction has no checkout in activation",
			frontmatter: `---
on:
  issues:
    types: [opened]
  reaction: eyes
permissions:
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---`,
			description: "Activation job with reaction should not include checkout - uses GitHub API instead",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "activation-checkout-test")

			testContent := tt.frontmatter + "\n\n# Test Workflow\n\nTest workflow content."
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompilerWithVersion("dev")
			// Use dev mode to use local action paths
			compiler.SetActionMode(ActionModeDev)

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

			// Verify activation job exists
			if !strings.Contains(lockContentStr, "activation:") {
				t.Error("Expected activation job to be present")
			}

			// Extract the activation job section
			activationJobStart := strings.Index(lockContentStr, "activation:")
			if activationJobStart == -1 {
				t.Fatal("Activation job not found in compiled workflow")
			}

			// Find the next job or end of file
			activationJobEnd := len(lockContentStr)
			nextJobIdx := strings.Index(lockContentStr[activationJobStart+11:], "\n  ")
			if nextJobIdx != -1 {
				searchStart := activationJobStart + 11 + nextJobIdx
				for idx := searchStart; idx < len(lockContentStr); idx++ {
					if lockContentStr[idx] == '\n' {
						lineStart := idx + 1
						if lineStart < len(lockContentStr) && lineStart+2 < len(lockContentStr) {
							if lockContentStr[lineStart:lineStart+2] == "  " && lockContentStr[lineStart+2] != ' ' {
								colonIdx := strings.Index(lockContentStr[lineStart:], ":")
								if colonIdx > 0 && colonIdx < 50 {
									activationJobEnd = idx
									break
								}
							}
						}
					}
				}
			}

			activationJobSection := lockContentStr[activationJobStart:activationJobEnd]

			// In dev mode, checkout may be present for setup action, but should be minimal
			// In release mode (which we no longer test here), there would be no checkout
			// The key is that we're NOT checking out the full .github/workflows directory
			// for timestamp checking - that uses GitHub API instead

			// Verify it does NOT checkout .github/workflows for timestamp checking
			if strings.Contains(activationJobSection, "Checkout workflows") {
				t.Errorf("%s: Should not have 'Checkout workflows' step - uses GitHub API for timestamp checking", tt.description)
			}

			// Verify timestamp check step is present
			if !strings.Contains(activationJobSection, "Check workflow file timestamps") {
				t.Errorf("%s: Should contain timestamp check step", tt.description)
			}

			// Verify scripts are loaded via require() (not inlined)
			if !strings.Contains(activationJobSection, "require(") {
				t.Errorf("%s: Should load scripts via require()", tt.description)
			}
		})
	}
}
