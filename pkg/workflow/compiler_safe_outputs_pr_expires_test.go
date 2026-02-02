//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/stringutil"
	"github.com/githubnext/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreatePullRequestExpiresWithPushToPRBranch tests that the expires field
// is correctly included in the handler config when both create-pull-request
// and push-to-pull-request-branch are present (regression test for issue #13285)
func TestCreatePullRequestExpiresWithPushToPRBranch(t *testing.T) {
	tests := []struct {
		name                 string
		createPRConfig       string
		pushToPRBranchConfig string
		expectExpires        bool
		expectTitlePrefix    bool
		expectLabels         bool
	}{
		{
			name: "with reviewers field (reproduces bug from issue #13285)",
			createPRConfig: `create-pull-request:
    expires: 2d
    title-prefix: "[TEST] "
    labels: [test, automation]
    reviewers: copilot`,
			pushToPRBranchConfig: "push-to-pull-request-branch:",
			expectExpires:        true,
			expectTitlePrefix:    true,
			expectLabels:         true,
		},
		{
			name: "without reviewers field (baseline - should work)",
			createPRConfig: `create-pull-request:
    expires: 2d
    title-prefix: "[TEST] "
    labels: [test, baseline]`,
			pushToPRBranchConfig: "push-to-pull-request-branch:",
			expectExpires:        true,
			expectTitlePrefix:    true,
			expectLabels:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory and workflow file
			testDir := testutil.TempDir(t, "test-pr-expires-*")
			workflowFile := filepath.Join(testDir, "test-workflow.md")

			markdown := `---
description: Test workflow
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  ` + tt.createPRConfig + `
  ` + tt.pushToPRBranchConfig + `
---
Test workflow
`

			err := os.WriteFile(workflowFile, []byte(markdown), 0644)
			require.NoError(t, err, "Should write workflow file")

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(workflowFile)
			require.NoError(t, err, "Should compile successfully")

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(workflowFile)
			lockContent, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Should read lock file")

			// Find the GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG line
			lines := strings.Split(string(lockContent), "\n")
			var handlerConfigJSON string
			for _, line := range lines {
				if strings.Contains(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:") {
					// Extract the JSON value - it's in quotes and may have escaped characters
					start := strings.Index(line, ": ")
					if start >= 0 {
						// Get everything after ": " and trim quotes
						jsonPart := strings.TrimSpace(line[start+2:])
						if strings.HasPrefix(jsonPart, `"`) && strings.HasSuffix(jsonPart, `"`) {
							// Remove surrounding quotes
							jsonPart = jsonPart[1 : len(jsonPart)-1]
							// Unescape backslashes
							jsonPart = strings.ReplaceAll(jsonPart, `\"`, `"`)
							jsonPart = strings.ReplaceAll(jsonPart, `\\`, `\`)
							handlerConfigJSON = jsonPart
						}
						break
					}
				}
			}

			require.NotEmpty(t, handlerConfigJSON, "Should find GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG")

			// Parse the handler config JSON
			var config map[string]map[string]any
			err = json.Unmarshal([]byte(handlerConfigJSON), &config)
			require.NoError(t, err, "Should parse handler config JSON")

			// Check create_pull_request configuration
			prConfig, exists := config["create_pull_request"]
			require.True(t, exists, "Should have create_pull_request handler")
			require.NotNil(t, prConfig, "create_pull_request config should not be nil")

			// Check expires field (48 hours = 2 days)
			if tt.expectExpires {
				expires, hasExpires := prConfig["expires"]
				assert.True(t, hasExpires, "Should have expires field")
				if hasExpires {
					assert.InDelta(t, 48.0, expires, 0.1, "Expires should be 48 hours (2 days)")
				}
			}

			// Check title_prefix field
			if tt.expectTitlePrefix {
				titlePrefix, hasTitlePrefix := prConfig["title_prefix"]
				assert.True(t, hasTitlePrefix, "Should have title_prefix field")
				if hasTitlePrefix {
					assert.Equal(t, "[TEST] ", titlePrefix, "Title prefix should match")
				}
			}

			// Check labels field
			if tt.expectLabels {
				labels, hasLabels := prConfig["labels"]
				assert.True(t, hasLabels, "Should have labels field")
				if hasLabels {
					labelsList, ok := labels.([]any)
					require.True(t, ok, "Labels should be an array")
					assert.Len(t, labelsList, 2, "Should have 2 labels")
				}
			}
		})
	}
}
