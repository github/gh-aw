//go:build integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestManualApprovalEnvironmentInActivationJob(t *testing.T) {
	tests := []struct {
		name                 string
		frontmatter          string
		wantEnvironmentInJob bool
		wantEnvironmentValue string
		wantCommentInYAML    bool
	}{
		{
			name: "manual-approval sets environment in activation job",
			frontmatter: `---
on:
  workflow_dispatch:
  manual-approval: production
permissions:
  contents: read
engine: copilot
strict: false
---`,
			wantEnvironmentInJob: true,
			wantEnvironmentValue: "production",
			wantCommentInYAML:    true,
		},
		{
			name: "no manual-approval means no environment",
			frontmatter: `---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
strict: false
---`,
			wantEnvironmentInJob: false,
			wantEnvironmentValue: "",
			wantCommentInYAML:    false,
		},
		{
			name: "manual-approval with different environment name",
			frontmatter: `---
on:
  issues:
    types: [opened]
  manual-approval: staging
permissions:
  contents: read
  issues: write
engine: copilot
features:
  dangerous-permissions-write: true
strict: false
---`,
			wantEnvironmentInJob: true,
			wantEnvironmentValue: "staging",
			wantCommentInYAML:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tmpDir := testutil.TempDir(t, "test-*")
			mdFile := tmpDir + "/test-workflow.md"
			lockFile := tmpDir + "/test-workflow.lock.yml"

			// Write the workflow file
			content := tt.frontmatter + "\n\n# Test Workflow\n\nTest content.\n"
			err := os.WriteFile(mdFile, []byte(content), 0644)
			if err != nil {
				t.Fatalf("failed to write test workflow: %v", err)
			}

			// Compile the workflow
			c := NewCompiler()
			if err := c.CompileWorkflow(mdFile); err != nil {
				t.Fatalf("CompileWorkflow() error = %v", err)
			}

			// Read the compiled lock file
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("failed to read lock file: %v", err)
			}

			lockStr := string(lockContent)

			// Check for environment field in activation job
			if tt.wantEnvironmentInJob {
				expectedEnv := "environment: " + tt.wantEnvironmentValue
				if !strings.Contains(lockStr, expectedEnv) {
					t.Errorf("Expected '%s' in activation job, but it was not found", expectedEnv)
				}
			} else {
				// Should not have environment field
				activationSection := extractActivationJobSection(lockStr)
				if strings.Contains(activationSection, "environment:") {
					t.Errorf("Did not expect environment field in activation job, but found one")
				}
			}

			// Check for comment in YAML
			if tt.wantCommentInYAML {
				expectedComment := "# Manual approval required: environment '" + tt.wantEnvironmentValue + "'"
				if !strings.Contains(lockStr, expectedComment) {
					t.Errorf("Expected comment '%s' in lock file, but it was not found", expectedComment)
				}
			} else {
				if strings.Contains(lockStr, "# Manual approval required:") {
					t.Errorf("Did not expect manual approval comment in lock file, but found one")
				}
			}
		})
	}
}

// extractActivationJobSection extracts the activation job section from the compiled YAML
func extractActivationJobSection(yaml string) string {
	lines := strings.Split(yaml, "\n")
	var activationLines []string
	inActivation := false

	for _, line := range lines {
		if strings.HasPrefix(line, "  activation:") {
			inActivation = true
		} else if inActivation && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			// Next job started
			break
		}

		if inActivation {
			activationLines = append(activationLines, line)
		}
	}

	return strings.Join(activationLines, "\n")
}
