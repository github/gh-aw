//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestBotsFieldExtraction tests the extraction of the bots field from frontmatter
func TestBotsFieldExtraction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-bots-test")

	compiler := NewCompiler()

	tests := []struct {
		name         string
		frontmatter  string
		filename     string
		expectedBots []string
	}{
		{
			name: "workflow with bots array",
			frontmatter: `---
on:
  issues:
    types: [opened]
bots: ["dependabot[bot]", "renovate[bot]"]
---

# Test Workflow
Test workflow content.`,
			filename:     "bots-array.md",
			expectedBots: []string{"dependabot[bot]", "renovate[bot]"},
		},
		{
			name: "workflow with single bot",
			frontmatter: `---
on:
  pull_request:
    types: [opened]
bots: ["github-actions[bot]"]
---

# Test Workflow
Test workflow content.`,
			filename:     "single-bot.md",
			expectedBots: []string{"github-actions[bot]"},
		},
		{
			name: "workflow without bots field",
			frontmatter: `---
on:
  push:
    branches: [main]
---

# Test Workflow
Test workflow content.`,
			filename:     "no-bots.md",
			expectedBots: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write the workflow file
			workflowPath := filepath.Join(tmpDir, tt.filename)
			err := os.WriteFile(workflowPath, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Parse the workflow
			workflowData, err := compiler.ParseWorkflowFile(workflowPath)
			if err != nil {
				t.Fatalf("Failed to parse workflow: %v", err)
			}

			// Check the extracted bots
			if len(workflowData.Bots) != len(tt.expectedBots) {
				t.Errorf("Expected %d bots, got %d", len(tt.expectedBots), len(workflowData.Bots))
			}

			for i, expectedBot := range tt.expectedBots {
				if i >= len(workflowData.Bots) {
					t.Errorf("Expected bot '%s' at index %d, but only got %d bots", expectedBot, i, len(workflowData.Bots))
					continue
				}
				if workflowData.Bots[i] != expectedBot {
					t.Errorf("Expected bot '%s' at index %d, got '%s'", expectedBot, i, workflowData.Bots[i])
				}
			}
		})
	}
}

// TestBotsEnvironmentVariableGeneration tests that bots are passed via environment variable
func TestBotsEnvironmentVariableGeneration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-bots-env-test")

	compiler := NewCompiler()

	frontmatter := `---
on:
  issues:
    types: [opened]
roles: [triage]
bots: ["dependabot[bot]", "renovate[bot]"]
---

# Test Workflow with Bots
Test workflow content.`

	workflowPath := filepath.Join(tmpDir, "workflow-with-bots.md")
	err := os.WriteFile(workflowPath, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	outputPath := filepath.Join(tmpDir, "workflow-with-bots.lock.yml")
	compiledContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledContent)

	// Check that the bots environment variable is set
	if !strings.Contains(compiledStr, "GH_AW_ALLOWED_BOTS: dependabot[bot],renovate[bot]") {
		t.Errorf("Expected compiled workflow to contain GH_AW_ALLOWED_BOTS environment variable")
	}

	// Also check that roles are still present
	if !strings.Contains(compiledStr, "GH_AW_REQUIRED_ROLES: triage") {
		t.Errorf("Expected compiled workflow to contain GH_AW_REQUIRED_ROLES environment variable")
	}
}

// TestBotsWithDefaultRoles tests that bots work with default roles
func TestBotsWithDefaultRoles(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-bots-default-roles-test")

	compiler := NewCompiler()

	frontmatter := `---
on:
  pull_request:
    types: [opened]
bots: ["dependabot[bot]"]
---

# Test Workflow
Test workflow content with bot and default roles.`

	workflowPath := filepath.Join(tmpDir, "workflow-bots-default-roles.md")
	err := os.WriteFile(workflowPath, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	outputPath := filepath.Join(tmpDir, "workflow-bots-default-roles.lock.yml")
	compiledContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledContent)

	// Check that default roles are present (admin, maintainer, write)
	if !strings.Contains(compiledStr, "GH_AW_REQUIRED_ROLES: admin,maintainer,write") {
		t.Errorf("Expected compiled workflow to contain default GH_AW_REQUIRED_ROLES")
	}

	// Check that bots environment variable is set
	if !strings.Contains(compiledStr, "GH_AW_ALLOWED_BOTS: dependabot[bot]") {
		t.Errorf("Expected compiled workflow to contain GH_AW_ALLOWED_BOTS environment variable")
	}
}

// TestBotsWithRolesAll tests that bots field works even when roles: all is set
func TestBotsWithRolesAll(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-bots-roles-all-test")

	compiler := NewCompiler()

	frontmatter := `---
on:
  issues:
    types: [opened]
roles: all
bots: ["dependabot[bot]"]
---

# Test Workflow
Test workflow content.`

	workflowPath := filepath.Join(tmpDir, "workflow-bots-roles-all.md")
	err := os.WriteFile(workflowPath, []byte(frontmatter), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	outputPath := filepath.Join(tmpDir, "workflow-bots-roles-all.lock.yml")
	compiledContent, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}

	compiledStr := string(compiledContent)

	// When roles: all is set, no check_membership job should be generated
	// so the bots environment variable shouldn't appear
	if strings.Contains(compiledStr, "check_membership") {
		t.Errorf("Expected no check_membership job when roles: all is set")
	}
}
