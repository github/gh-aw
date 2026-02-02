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

func TestAllowGitHubReferencesEnvVar(t *testing.T) {
	tests := []struct {
		name                  string
		workflow              string
		expectedEnvVarPresent bool
		expectedEnvVarValue   string
	}{
		{
			name: "env var set with single repo",
			workflow: `---
on: push
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
permissions:
  contents: read
  issues: write
safe-outputs:
  allowed-github-references: ["repo"]
  create-issue: {}
---

# Test Workflow

Test workflow with allowed-github-references.
`,
			expectedEnvVarPresent: true,
			expectedEnvVarValue:   "repo",
		},
		{
			name: "env var set with multiple repos",
			workflow: `---
on: push
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
permissions:
  contents: read
  issues: write
safe-outputs:
  allowed-github-references: ["repo", "org/repo2", "org/repo3"]
  create-issue: {}
---

# Test Workflow

Test workflow with multiple allowed repos.
`,
			expectedEnvVarPresent: true,
			expectedEnvVarValue:   "repo,org/repo2,org/repo3",
		},
		{
			name: "env var not set when allowed-github-references is absent",
			workflow: `---
on: push
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
permissions:
  contents: read
  issues: write
safe-outputs:
  create-issue: {}
---

# Test Workflow

Test workflow without allowed-github-references.
`,
			expectedEnvVarPresent: false,
		},
		{
			name: "env var with repos containing special characters",
			workflow: `---
on: push
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
permissions:
  contents: read
  issues: write
safe-outputs:
  allowed-github-references: ["my-org/my-repo", "test-org/test.repo"]
  create-issue: {}
---

# Test Workflow

Test workflow with special characters in repo names.
`,
			expectedEnvVarPresent: true,
			expectedEnvVarValue:   "my-org/my-repo,test-org/test.repo",
		},
		{
			name: "env var with mix of repo keyword and specific repos",
			workflow: `---
on: push
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
permissions:
  contents: read
  issues: write
safe-outputs:
  allowed-github-references: ["repo", "microsoft/vscode"]
  create-issue: {}
---

# Test Workflow

Test workflow mixing repo keyword with specific repos.
`,
			expectedEnvVarPresent: true,
			expectedEnvVarValue:   "repo,microsoft/vscode",
		},
		{
			name: "env var with only specific repos (no repo keyword)",
			workflow: `---
on: push
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
permissions:
  contents: read
  issues: write
safe-outputs:
  allowed-github-references: ["octocat/hello-world", "github/copilot"]
  create-issue: {}
---

# Test Workflow

Test workflow with only specific repos allowed.
`,
			expectedEnvVarPresent: true,
			expectedEnvVarValue:   "octocat/hello-world,github/copilot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test files
			tmpDir := testutil.TempDir(t, "allow-github-refs-test")

			// Write workflow file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflow), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockStr := string(lockContent)

			// Check for env var presence
			if tt.expectedEnvVarPresent {
				if !strings.Contains(lockStr, "GH_AW_ALLOWED_GITHUB_REFS:") {
					t.Error("Expected GH_AW_ALLOWED_GITHUB_REFS environment variable in lock file")
				}

				// Verify the value
				expectedLine := `GH_AW_ALLOWED_GITHUB_REFS: "` + tt.expectedEnvVarValue + `"`
				if !strings.Contains(lockStr, expectedLine) {
					t.Errorf("Expected GH_AW_ALLOWED_GITHUB_REFS value to be %q, but it was not found in lock file", tt.expectedEnvVarValue)
				}
			} else {
				if strings.Contains(lockStr, "GH_AW_ALLOWED_GITHUB_REFS:") {
					t.Error("Expected no GH_AW_ALLOWED_GITHUB_REFS environment variable in lock file")
				}
			}
		})
	}
}
