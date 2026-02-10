//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestDefaultPermissionRestriction tests the new default permission restrictions from issue #567
func TestDefaultPermissionRestriction(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-permission-restriction-test")

	compiler := NewCompiler()

	tests := []struct {
		name                  string
		frontmatter           string
		filename              string
		expectPermissionCheck bool
		expectedPermissions   []string
	}{
		{
			name: "workflow with push trigger should include permission check (default)",
			frontmatter: `---
on:
  push:
    branches: [main]
tools:
  github:
    allowed: [list_issues]
---

# Push Workflow
Test workflow content.`,
			filename:              "push-workflow.md",
			expectPermissionCheck: true,
			expectedPermissions:   []string{"admin", "maintainer", "write"},
		},
		{
			name: "workflow with issues trigger should include permission check (default)",
			frontmatter: `---
on:
  issues:
    types: [opened]
tools:
  github:
    allowed: [list_issues]
---

# Issues Workflow
Test workflow content.`,
			filename:              "issues-workflow.md",
			expectPermissionCheck: true,
			expectedPermissions:   []string{"admin", "maintainer", "write"},
		},
		{
			name: "workflow with roles: all should not include permission check",
			frontmatter: `---
on:
  push:
    branches: [main]
roles: all
tools:
  github:
    allowed: [list_issues]
---

# Unrestricted Workflow
Test workflow content.`,
			filename:              "unrestricted-workflow.md",
			expectPermissionCheck: false,
			expectedPermissions:   []string{"all"},
		},
		{
			name: "workflow with custom permissions should include permission check",
			frontmatter: `---
on:
  pull_request:
    types: [opened]
roles: [admin, maintainer, write]
tools:
  github:
    allowed: [list_issues]
---

# Custom Permission Workflow
Test workflow content.`,
			filename:              "custom-permission-workflow.md",
			expectPermissionCheck: true,
			expectedPermissions:   []string{"admin", "maintainer", "write"},
		},
		{
			name: "workflow with workflow_dispatch only should NOT include permission check (safe event)",
			frontmatter: `---
on:
  workflow_dispatch:
roles: [admin, maintainer, write]
tools:
  github:
    allowed: [list_issues]
---

# Manual Workflow
Test workflow content.`,
			filename:              "manual-workflow.md",
			expectPermissionCheck: false,
			expectedPermissions:   []string{"admin", "maintainer", "write"},
		},
		{
			name: "workflow with workflow_dispatch without write role should include permission check",
			frontmatter: `---
on:
  workflow_dispatch:
roles: [admin, maintainer]
tools:
  github:
    allowed: [list_issues]
---

# Manual Workflow Restricted
Test workflow content.`,
			filename:              "manual-workflow-restricted.md",
			expectPermissionCheck: true,
			expectedPermissions:   []string{"admin", "maintainer"},
		},
		{
			name: "workflow with schedule only should NOT include permission check (safe event)",
			frontmatter: `---
on:
  schedule:
    - cron: "0 9 * * 1"
tools:
  github:
    allowed: [list_issues]
---

# Scheduled Workflow
Test workflow content.`,
			filename:              "schedule-workflow.md",
			expectPermissionCheck: false,
			expectedPermissions:   []string{"admin", "maintainer", "write"},
		},
		{
			name: "workflow with workflow_run should INCLUDE permission check (unsafe event)",
			frontmatter: `---
on:
  workflow_run:
    workflows: ["build"]
    types: [completed]
tools:
  github:
    allowed: [list_issues]
---

# Workflow Run Trigger
Test workflow content.`,
			filename:              "workflow-run-workflow.md",
			expectPermissionCheck: true,
			expectedPermissions:   []string{"admin", "maintainer", "write"},
		},
		{
			name: "workflow with mixed safe and unsafe events should include permission check",
			frontmatter: `---
on:
  workflow_dispatch:
  push:
    branches: [main]
tools:
  github:
    allowed: [list_issues]
---

# Mixed Event Workflow
Test workflow content.`,
			filename:              "mixed-workflow.md",
			expectPermissionCheck: true,
			expectedPermissions:   []string{"admin", "maintainer", "write"},
		},
		{
			name: "workflow with command and workflow_dispatch with write role should include permission check for command",
			frontmatter: `---
on:
  command:
    name: scout
  workflow_dispatch:
roles: [admin, maintainer, write]
tools:
  github:
    allowed: [list_issues]
---

# Scout-like Workflow
Test workflow content.`,
			filename:              "scout-like-workflow.md",
			expectPermissionCheck: true,
			expectedPermissions:   []string{"admin", "maintainer", "write"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, tt.filename)
			err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContentStr := string(lockContent)

			// Check if permission check is present
			// For command workflows, look for command-specific check text
			hasPermissionCheck := strings.Contains(lockContentStr, "Check team membership for workflow") ||
				strings.Contains(lockContentStr, "Check team membership for command workflow")

			if tt.expectPermissionCheck {
				if !hasPermissionCheck {
					t.Errorf("Expected permission check to be present but not found")
				}

				// Verify the expected permission levels are mentioned in the script
				for _, perm := range tt.expectedPermissions {
					if perm != "all" && !strings.Contains(lockContentStr, perm) {
						t.Errorf("Expected permission '%s' to be mentioned in the permission check", perm)
					}
				}
			} else {
				if hasPermissionCheck {
					t.Errorf("Did not expect permission check in workflow with roles: all but found it")
				}
			}
		})
	}
}

// TestCommandWorkflowStillWorks tests that existing command workflows still work with the new logic
func TestCommandWorkflowStillWorks(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-command-compatibility-test")

	compiler := NewCompiler()

	frontmatter := `---
on:
  command:
    name: test-bot
tools:
  github:
    allowed: [list_issues]
---

# Test Bot
Test workflow content.`

	// Create test file
	testFile := filepath.Join(tmpDir, "command-workflow.md")
	var err error
	err = os.WriteFile(testFile, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Check that permission check is present
	if !strings.Contains(lockContentStr, "Check team membership for command workflow") {
		t.Errorf("Expected permission check to be present in command workflow")
	}

	// Check that it includes command-specific condition logic (strict matching)
	hasStartsWith := strings.Contains(lockContentStr, "startsWith(github.event")
	hasExactMatch := strings.Contains(lockContentStr, ".body == '/")

	if !hasStartsWith && !hasExactMatch {
		t.Errorf("Expected command-specific conditional logic (startsWith or exact match) in permission check")
	}
}
