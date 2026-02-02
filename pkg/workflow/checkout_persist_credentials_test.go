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

func TestCheckoutPersistCredentials(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		description string
	}{
		{
			name: "main job checkout includes persist-credentials false",
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
    allowed: [list_issues]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---`,
			description: "Main job checkout step should include persist-credentials: false",
		},
		{
			name: "safe output create-issue checkout includes persist-credentials false",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
safe-outputs:
  create-issue:
    assignees: [user1]
engine: claude
strict: false
---`,
			description: "Create issue job checkout should include persist-credentials: false",
		},
		{
			name: "safe output create-pull-request checkout includes persist-credentials false",
			frontmatter: `---
on:
  push:
    branches: [main]
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
safe-outputs:
  create-pull-request:
engine: claude
strict: false
---`,
			description: "Create pull request job checkout should include persist-credentials: false",
		},
		{
			name: "safe output push-to-pull-request-branch checkout includes persist-credentials false",
			frontmatter: `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
safe-outputs:
  push-to-pull-request-branch:
engine: claude
strict: false
---`,
			description: "Push to PR branch job checkout should include persist-credentials: false",
		},
		{
			name: "safe output upload_assets checkout includes persist-credentials false",
			frontmatter: `---
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
safe-outputs:
  upload-asset:
engine: claude
strict: false
---`,
			description: "Upload assets job checkout should include persist-credentials: false",
		},
		{
			name: "safe output create-agent-session checkout includes persist-credentials false",
			frontmatter: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
safe-outputs:
  create-agent-session:
engine: claude
strict: false
---`,
			description: "Create agent session job checkout should include persist-credentials: false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "checkout-persist-credentials-test")

			// Create test workflow file
			testContent := tt.frontmatter + "\n\n# Test Workflow\n\nThis is a test workflow to check persist-credentials.\n"
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

			// Parse the workflow to find checkouts and their associated jobs
			checkoutsByJob := make(map[string][]string)
			var currentJob string
			lines := strings.Split(lockContentStr, "\n")

			for i, line := range lines {
				// Detect job names: they should be indented with exactly 2 spaces and end with :
				// and they should come under a "jobs:" section
				if len(line) >= 3 && line[0:2] == "  " && line[2] != ' ' && strings.Contains(line, ":") {
					// Extract the job name (everything before the colon, without leading spaces)
					jobNameWithColon := strings.TrimSpace(line)
					if strings.HasSuffix(jobNameWithColon, ":") {
						currentJob = strings.TrimSuffix(jobNameWithColon, ":")
					}
				}

				if strings.Contains(line, "actions/checkout@") {
					// Collect the next several lines to check for persist-credentials
					context := ""
					for j := i; j < len(lines) && j < i+10; j++ {
						context += lines[j] + "\n"
						if strings.TrimSpace(lines[j]) != "" && !strings.HasPrefix(strings.TrimSpace(lines[j]), "-") && j > i {
							// Stop if we hit a non-indented line or a new step
							if !strings.HasPrefix(lines[j], "      ") && !strings.HasPrefix(lines[j], "        ") {
								break
							}
						}
					}
					if currentJob != "" {
						checkoutsByJob[currentJob] = append(checkoutsByJob[currentJob], context)
					}
				}
			}

			if len(checkoutsByJob) == 0 {
				t.Logf("Note: No checkout steps found in workflow, which may be expected for some configurations")
				return
			}

			// Determine which job(s) we expect to have persist-credentials: true
			// All jobs now use persist-credentials: false and rely on git remote set-url for authentication
			expectTrueJobs := make(map[string]bool)

			// Verify each checkout has persist-credentials set correctly based on its job
			for jobName, checkouts := range checkoutsByJob {
				expectTrue := expectTrueJobs[jobName]

				for idx, checkoutContext := range checkouts {
					if expectTrue {
						if !strings.Contains(checkoutContext, "persist-credentials: true") {
							t.Errorf("%s (job: %s): Checkout #%d missing persist-credentials: true\nContext:\n%s",
								tt.description, jobName, idx+1, checkoutContext)
						}
					} else {
						if !strings.Contains(checkoutContext, "persist-credentials: false") {
							t.Errorf("%s (job: %s): Checkout #%d missing persist-credentials: false\nContext:\n%s",
								tt.description, jobName, idx+1, checkoutContext)
						}
					}
				}
			}
		})
	}
}
