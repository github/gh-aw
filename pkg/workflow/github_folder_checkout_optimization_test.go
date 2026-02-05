//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGitHubFolderCheckoutOptimization verifies that the .github folder sparse
// checkout is skipped when a full repository checkout will be performed, avoiding
// redundant checkout operations.
func TestGitHubFolderCheckoutOptimization(t *testing.T) {
	tests := []struct {
		name                 string
		workflowContent      string
		expectGitHubCheckout bool
		expectFullCheckout   bool
		description          string
	}{
		{
			name: "full_checkout_no_github_sparse",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
---

# Test workflow with full checkout
Test workflow that should get a full repository checkout.
`,
			expectGitHubCheckout: false,
			expectFullCheckout:   true,
			description:          "When full repo checkout is added, .github sparse checkout should be skipped",
		},
		{
			name: "custom_checkout_skips_github_sparse",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
steps:
  - name: Custom checkout
    uses: actions/checkout@v5
---

# Test workflow with custom checkout
Test workflow with custom checkout in steps.
`,
			expectGitHubCheckout: false,
			expectFullCheckout:   false,
			description:          "When custom checkout exists, .github sparse checkout is skipped (redundant with full checkout)",
		},
		{
			name: "no_permissions_no_checkouts",
			workflowContent: `---
on: push
permissions: {}
engine: copilot
---

# Test workflow without permissions
Test workflow without contents permission.
`,
			expectGitHubCheckout: false,
			expectFullCheckout:   true, // Changed: In dev mode, contents:read is added for local actions, triggering full checkout for runtime-import
			description:          "Without explicit contents permission, full checkout is added in dev mode for runtime-import (contents:read added automatically for local actions)",
		},
		{
			name: "runtime_imports_trigger_full_checkout",
			workflowContent: `---
on: push
permissions:
  contents: read
engine: copilot
---

# Test workflow with runtime imports

This workflow uses runtime imports: {{runtime-import:shared/example.md}}
`,
			expectGitHubCheckout: false,
			expectFullCheckout:   true,
			description:          "Runtime imports trigger full checkout, making .github sparse checkout redundant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir, err := os.MkdirTemp("", "github-folder-checkout-test-")
			require.NoError(t, err, "Failed to create temp dir")
			defer os.RemoveAll(tempDir)

			// Create workflows directory
			workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
			require.NoError(t, os.MkdirAll(workflowsDir, 0755), "Failed to create workflows directory")

			// Write test workflow file
			workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(tt.workflowContent), 0644), "Failed to write workflow file")

			// Compile workflow
			compiler := NewCompiler()
			compiler.SetActionMode(ActionModeDev) // Use dev mode with local action paths
			err = compiler.CompileWorkflow(workflowPath)
			require.NoError(t, err, "Failed to compile workflow")

			// Read generated lock file
			lockPath := filepath.Join(workflowsDir, "test-workflow.lock.yml")
			lockContent, err := os.ReadFile(lockPath)
			require.NoError(t, err, "Failed to read lock file")
			lockStr := string(lockContent)

			// Extract the agent job section
			agentJobStart := strings.Index(lockStr, "  agent:")
			require.NotEqual(t, -1, agentJobStart, "Could not find agent job in compiled workflow")

			// Find the next job (starts with "  " followed by a non-space character)
			remainingContent := lockStr[agentJobStart+10:]
			nextJobStart := -1
			lines := strings.Split(remainingContent, "\n")
			for i, line := range lines {
				if len(line) > 2 && line[0] == ' ' && line[1] == ' ' && line[2] != ' ' && line[2] != '\t' {
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

			// Check for .github and .agents folders checkout
			hasGitHubCheckout := strings.Contains(agentJobSection, "Checkout .github and .agents folders")
			assert.Equal(t, tt.expectGitHubCheckout, hasGitHubCheckout,
				"Test case: %s - Expected .github and .agents checkout: %t, got: %t\nDescription: %s",
				tt.name, tt.expectGitHubCheckout, hasGitHubCheckout, tt.description)

			// Check for full repository checkout
			hasFullCheckout := strings.Contains(agentJobSection, "Checkout repository")
			assert.Equal(t, tt.expectFullCheckout, hasFullCheckout,
				"Test case: %s - Expected full checkout: %t, got: %t\nDescription: %s",
				tt.name, tt.expectFullCheckout, hasFullCheckout, tt.description)

			// Verify they're not both present (redundant)
			if hasGitHubCheckout && hasFullCheckout {
				t.Errorf("Test case: %s - Both .github sparse checkout and full checkout are present (redundant!)\nDescription: %s",
					tt.name, tt.description)
			}

			// If .github checkout is expected, verify that .agents folder is also included
			if tt.expectGitHubCheckout {
				assert.Contains(t, agentJobSection, ".github",
					"Test case: %s - Sparse checkout should include .github folder", tt.name)
				assert.Contains(t, agentJobSection, ".agents",
					"Test case: %s - Sparse checkout should include .agents folder", tt.name)
			}

			t.Logf("✓ Test case passed: %s", tt.description)
		})
	}
}
