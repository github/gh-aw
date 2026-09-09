//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckoutDisabled(t *testing.T) {
	tests := []struct {
		name                       string
		frontmatter                string
		expectedHasDefaultCheckout bool
		expectedHasDevModeCheckout bool
		expectTargetRepo           string
		description                string
	}{
		{
			name: "checkout: false disables agent job default checkout",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    toolsets: [issues]
engine: claude
checkout: false
strict: false
---`,
			expectedHasDefaultCheckout: false,
			expectedHasDevModeCheckout: true,
			description:                "checkout: false should disable the default repository checkout step but leave dev-mode checkouts intact",
		},
		{
			name: "checkout absent still adds checkout by default",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    toolsets: [issues]
engine: claude
strict: false
---`,
			expectedHasDefaultCheckout: true,
			expectedHasDevModeCheckout: true,
			description:                "When checkout is not set, the default checkout step is included",
		},
		{
			name: "permissions.contents: none skips default checkout but keeps target checkout",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: none
  issues: read
tools:
  github:
    toolsets: [issues]
engine: claude
checkout:
  - repository: octo-org/target-repository
    path: target
strict: false
---`,
			expectedHasDefaultCheckout: false,
			expectedHasDevModeCheckout: true,
			expectTargetRepo:           "octo-org/target-repository",
			description:                "permissions.contents: none should skip only the default workflow-repository checkout while target-only checkout entries are still generated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "checkout-disabled-test")

			testContent := tt.frontmatter + "\n\n# Test Workflow\n\nThis is a test workflow.\n"
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644), "should write test file")

			compiler := NewCompiler()
			compiler.SetActionMode(ActionModeDev)
			require.NoError(t, compiler.CompileWorkflow(testFile), "should compile workflow")

			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			require.NoError(t, err, "should read lock file")

			lockContentStr := string(lockContent)

			// Find the agent job section
			agentJobStart := strings.Index(lockContentStr, "\n  agent:")
			require.NotEqual(t, -1, agentJobStart, "agent job not found in compiled workflow")

			// Extract from agent job to end
			agentSection := lockContentStr[agentJobStart:]

			// The default workspace checkout is identified by "name: Checkout repository"
			hasDefaultCheckout := strings.Contains(agentSection, "name: Checkout repository")

			// Dev-mode checkouts (e.g. "Checkout actions folder") should always be present
			hasDevModeCheckout := strings.Contains(agentSection, "name: Checkout actions folder")

			assert.Equal(t, tt.expectedHasDefaultCheckout, hasDefaultCheckout, "%s: default checkout presence mismatch", tt.description)
			assert.Equal(t, tt.expectedHasDevModeCheckout, hasDevModeCheckout, "%s: dev-mode checkout should not be affected", tt.description)
			if tt.expectTargetRepo != "" {
				assert.Contains(t, agentSection, tt.expectTargetRepo, "%s: target repository checkout should still be present", tt.description)
			}
		})
	}
}
