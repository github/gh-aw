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

// TestLocalActionPermissions tests that jobs using local path actions (./actions/setup)
// handle permissions correctly based on what the user specifies.
// When permissions are explicitly specified without contents, the compiler respects
// that choice and does not auto-add contents: read, which means local actions checkout
// will be skipped (and the workflow will need to use action-tag to use remote actions).
func TestLocalActionPermissions(t *testing.T) {
	tests := []struct {
		name               string
		frontmatter        string
		description        string
		expectedPermission string
		jobName            string
		expectCheckout     bool
	}{
		{
			name: "pre-activation job with explicit permissions without contents",
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
command: /fix
---`,
			description:        "Pre-activation job should respect explicit permissions without contents",
			expectedPermission: "issues: write",
			jobName:            "pre_activation",
			expectCheckout:     false, // No checkout because no contents permission
		},
		{
			name: "main agent job with explicit permissions without contents",
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
			description:        "Main agent job should respect explicit permissions without contents",
			expectedPermission: "issues: write",
			jobName:            "agent",
			expectCheckout:     false, // No checkout because no contents permission
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "local-action-permissions-test")

			testContent := tt.frontmatter + "\n\n# Test Workflow\n\nTest workflow content."
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompilerWithVersion("dev")
			// Use dev mode to enable local action paths
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

			// Verify the job exists
			jobMarker := tt.jobName + ":"
			if !strings.Contains(lockContentStr, jobMarker) {
				t.Errorf("Expected %s job to be present", tt.jobName)
				return
			}

			// Extract the job section
			jobStart := strings.Index(lockContentStr, jobMarker)
			if jobStart == -1 {
				t.Fatalf("%s job not found in compiled workflow", tt.jobName)
			}

			// Find the next job or end of file
			jobEnd := len(lockContentStr)
			nextJobIdx := strings.Index(lockContentStr[jobStart+len(jobMarker):], "\n  ")
			if nextJobIdx != -1 {
				searchStart := jobStart + len(jobMarker) + nextJobIdx
				for idx := searchStart; idx < len(lockContentStr); idx++ {
					if lockContentStr[idx] == '\n' {
						lineStart := idx + 1
						if lineStart < len(lockContentStr) && lineStart+2 < len(lockContentStr) {
							if lockContentStr[lineStart:lineStart+2] == "  " && lockContentStr[lineStart+2] != ' ' {
								colonIdx := strings.Index(lockContentStr[lineStart:], ":")
								if colonIdx > 0 && colonIdx < 50 {
									jobEnd = idx
									break
								}
							}
						}
					}
				}
			}

			jobSection := lockContentStr[jobStart:jobEnd]

			// Verify checkout step expectation
			hasCheckout := strings.Contains(jobSection, "Checkout actions folder") || strings.Contains(jobSection, "actions/checkout@")
			if tt.expectCheckout && !hasCheckout {
				t.Errorf("%s: Expected checkout actions folder step to be present in %s job", tt.description, tt.jobName)
			} else if !tt.expectCheckout && hasCheckout {
				t.Errorf("%s: Did not expect checkout step in %s job (no contents permission)", tt.description, tt.jobName)
			}

			// Verify the expected permission is present (if permissions block exists)
			if strings.Contains(jobSection, "permissions:") {
				if !strings.Contains(jobSection, tt.expectedPermission) {
					t.Errorf("%s: Expected '%s' permission in %s job\nJob section:\n%s",
						tt.description, tt.expectedPermission, tt.jobName, jobSection)
				}
			} else if tt.expectedPermission != "" {
				// If we expect a specific permission but there's no permissions block,
				// that might be okay depending on the job type (e.g., pre-activation)
				t.Logf("%s: No permissions block in %s job, expected '%s'", tt.description, tt.jobName, tt.expectedPermission)
			}
		})
	}
}

// TestLocalActionPermissionsNotAddedInReleaseMode verifies that in release mode (production),
// where remote actions are used instead of local paths, the checkout step is not added
func TestLocalActionPermissionsNotAddedInReleaseMode(t *testing.T) {
	tmpDir := testutil.TempDir(t, "release-mode-test")

	frontmatter := `---
on:
  issues:
    types: [opened]
permissions:
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
command: /fix
---`

	testContent := frontmatter + "\n\n# Test Workflow\n\nTest workflow content."
	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompilerWithVersion("v1.0.0")
	// Use release mode to test production behavior (no local action checkouts)
	compiler.SetActionMode(ActionModeRelease)

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

	// In release mode, should NOT have "Checkout actions folder" step
	if strings.Contains(lockContentStr, "Checkout actions folder") {
		t.Error("Release mode should NOT include 'Checkout actions folder' step")
	}

	// Should use remote action references instead
	if !strings.Contains(lockContentStr, "github/gh-aw/actions/setup@v1.0.0") {
		t.Error("Release mode should use remote action references like 'github/gh-aw/actions/setup@v1.0.0'")
	}
}
