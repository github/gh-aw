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

func TestTrackerIDIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	tests := []struct {
		name               string
		workflowContent    string
		shouldCompile      bool
		shouldHaveEnvVar   bool
		shouldHaveInScript bool
		expectedTrackerID  string
	}{
		{
			name: "Workflow with valid tracker-id",
			workflowContent: `---
on: workflow_dispatch
permissions:
  contents: read
tracker-id: test-fp-12345
safe-outputs:
  create-issue:
---

# Test Tracker ID

Create a test issue.
`,
			shouldCompile:      true,
			shouldHaveEnvVar:   true,
			shouldHaveInScript: true,
			expectedTrackerID:  "test-fp-12345",
		},
		{
			name: "Workflow without tracker-id",
			workflowContent: `---
on: workflow_dispatch
permissions:
  contents: read
safe-outputs:
  create-issue:
---

# Test No Tracker ID

Create a test issue without tracker-id.
`,
			shouldCompile:      true,
			shouldHaveEnvVar:   false,
			shouldHaveInScript: false,
		},
		{
			name: "Workflow with tracker-id in pull request",
			workflowContent: `---
on: push
permissions:
  contents: read
tracker-id: pr-tracker-123
safe-outputs:
  create-pull-request:
---

# Test PR Tracker ID

Create a pull request.
`,
			shouldCompile:      true,
			shouldHaveEnvVar:   true,
			shouldHaveInScript: true,
			expectedTrackerID:  "pr-tracker-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowFile := filepath.Join(tmpDir, "test.md")
			err := os.WriteFile(workflowFile, []byte(tt.workflowContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write test workflow: %v", err)
			}

			compiler := NewCompiler()
			// Use dev mode to test with local action paths
			compiler.SetActionMode(ActionModeDev)
			compiler.verbose = false

			err = compiler.CompileWorkflow(workflowFile)

			if tt.shouldCompile && err != nil {
				t.Fatalf("Expected compilation to succeed, got error: %v", err)
			}
			if !tt.shouldCompile && err == nil {
				t.Fatal("Expected compilation to fail, but it succeeded")
			}

			if tt.shouldCompile {
				lockFile := stringutil.MarkdownToLockFile(workflowFile)
				content, err := os.ReadFile(lockFile)
				if err != nil {
					t.Fatalf("Failed to read lock file: %v", err)
				}

				contentStr := string(content)

				if tt.shouldHaveEnvVar {
					envVarLine := "GH_AW_TRACKER_ID: \"" + tt.expectedTrackerID + "\""
					if !strings.Contains(contentStr, envVarLine) {
						t.Errorf("Expected lock file to contain env var '%s', but it didn't", envVarLine)
					}
				} else {
					// The JavaScript code will always read process.env.GH_AW_TRACKER_ID
					// but the environment variable should not be set
					envVarLine := "GH_AW_TRACKER_ID: \""
					if strings.Contains(contentStr, envVarLine) {
						t.Error("Expected lock file to NOT set GH_AW_TRACKER_ID env var, but it did")
					}
				}

				if tt.shouldHaveInScript {
					// Check that tracker-id environment variable is set
					if !strings.Contains(contentStr, "GH_AW_TRACKER_ID") {
						t.Error("Expected GH_AW_TRACKER_ID environment variable to be set")
					}
					// Check that scripts are loaded using require() (file mode, not inline)
					if !strings.Contains(contentStr, "require(") {
						t.Error("Expected scripts to be loaded using require()")
					}
				}

				// Clean up lock file
				os.Remove(lockFile)
			}

			// Clean up workflow file
			os.Remove(workflowFile)
		})
	}
}
