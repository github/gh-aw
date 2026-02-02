//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
)

func TestPullRequestForksArrayFilter(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "forks-array-filter-test")

	compiler := NewCompiler()

	tests := []struct {
		name               string
		frontmatter        string
		expectedConditions []string // Expected substrings in the generated condition
		shouldHaveIf       bool     // Whether an if condition should be present
	}{
		{
			name: "pull_request without forks field (default: disallow all forks)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{
				"github.event.pull_request.head.repo.id == github.repository_id",
			},
			shouldHaveIf: true,
		},
		{
			name: "pull_request with forks array (exact matches)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks:
      - "githubnext/test-repo"
      - "octocat/hello-world"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{
				"github.event.pull_request.head.repo.id == github.repository_id",
				"github.event.pull_request.head.repo.full_name == 'githubnext/test-repo'",
				"github.event.pull_request.head.repo.full_name == 'octocat/hello-world'",
			},
			shouldHaveIf: true,
		},
		{
			name: "pull_request with forks array (glob patterns)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks:
      - "githubnext/*"
      - "octocat/*"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{
				"github.event.pull_request.head.repo.id == github.repository_id",
				"startsWith(github.event.pull_request.head.repo.full_name, 'githubnext/')",
				"startsWith(github.event.pull_request.head.repo.full_name, 'octocat/')",
			},
			shouldHaveIf: true,
		},
		{
			name: "pull_request with forks array (mixed exact and glob)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks:
      - "githubnext/test-repo"
      - "octocat/*"
      - "microsoft/vscode"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{
				"github.event.pull_request.head.repo.id == github.repository_id",
				"github.event.pull_request.head.repo.full_name == 'githubnext/test-repo'",
				"startsWith(github.event.pull_request.head.repo.full_name, 'octocat/')",
				"github.event.pull_request.head.repo.full_name == 'microsoft/vscode'",
			},
			shouldHaveIf: true,
		},
		{
			name: "pull_request with empty forks array",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks: []

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{
				"github.event.pull_request.head.repo.id == github.repository_id",
			},
			shouldHaveIf: true,
		},
		{
			name: "pull_request with forks array and existing if condition",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks:
      - "trusted-org/*"

if: github.actor != 'dependabot[bot]'

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{
				"github.actor != 'dependabot[bot]'",
				"startsWith(github.event.pull_request.head.repo.full_name, 'trusted-org/')",
			},
			shouldHaveIf: true,
		},
		{
			name: "pull_request with forks single string (exact match)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks: "githubnext/test-repo"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{
				"github.event.pull_request.head.repo.id == github.repository_id",
				"github.event.pull_request.head.repo.full_name == 'githubnext/test-repo'",
			},
			shouldHaveIf: true,
		},
		{
			name: "pull_request with forks single string (glob pattern)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks: "githubnext/*"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{
				"github.event.pull_request.head.repo.id == github.repository_id",
				"startsWith(github.event.pull_request.head.repo.full_name, 'githubnext/')",
			},
			shouldHaveIf: true,
		},
		{
			name: "pull_request with forks wildcard string (allow all forks)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks: "*"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{},
			shouldHaveIf:       false, // No fork filtering should be applied
		},
		{
			name: "pull_request with forks array containing wildcard",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    forks:
      - "*"
      - "githubnext/test-repo"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedConditions: []string{},
			shouldHaveIf:       false, // No fork filtering should be applied due to "*"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Forks Array Filter Workflow

This is a test workflow for forks array filtering with glob support.
`

			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := testFile[:len(testFile)-3] + ".lock.yml"
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockContent := string(content)

			if tt.shouldHaveIf {
				// Check that each expected condition is present
				for _, expectedCondition := range tt.expectedConditions {
					if !strings.Contains(lockContent, expectedCondition) {
						t.Errorf("Expected lock file to contain '%s' but it didn't.\nContent:\n%s", expectedCondition, lockContent)
					}
				}
			} else {
				// Check that no fork-related if condition is present in the main job
				for _, condition := range tt.expectedConditions {
					if strings.Contains(lockContent, condition) {
						t.Errorf("Expected no fork filter condition but found '%s' in lock file.\nContent:\n%s", condition, lockContent)
					}
				}
			}
		})
	}
}

// TestForksArrayFieldCommentingInOnSection specifically tests that the forks array field is commented out in the on section
func TestForksArrayFieldCommentingInOnSection(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "forks-array-commenting-test")

	compiler := NewCompiler()

	tests := []struct {
		name         string
		frontmatter  string
		expectedYAML string // Expected YAML structure with commented forks
		description  string
	}{
		{
			name: "pull_request with forks array and types",
			frontmatter: `---
on:
  pull_request:
    types: [opened]
    paths: ["src/**"]
    forks:
      - "org/repo"
      - "trusted/*"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedYAML: `  pull_request:
    # forks: # Fork filtering applied via job conditions
    # - org/repo # Fork filtering applied via job conditions
    # - trusted/* # Fork filtering applied via job conditions
    paths:
    - src/**
    types:
    - opened`,
			description: "Should comment out entire forks array but keep paths and types",
		},
		{
			name: "pull_request with only forks array",
			frontmatter: `---
on:
  pull_request:
    forks:
      - "specific/repo"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedYAML: `  pull_request:
    # forks: # Fork filtering applied via job conditions
    # - specific/repo # Fork filtering applied via job conditions`,
			description: "Should comment out forks array even when it's the only field",
		},
		{
			name: "pull_request with forks single string",
			frontmatter: `---
on:
  pull_request:
    forks: "specific/repo"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedYAML: `  pull_request:
    # forks: specific/repo # Fork filtering applied via job conditions`,
			description: "Should comment out forks single string",
		},
		{
			name: "pull_request with forks wildcard string",
			frontmatter: `---
on:
  pull_request:
    forks: "*"

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedYAML: `  pull_request:
    # forks: "*" # Fork filtering applied via job conditions`,
			description: "Should comment out forks wildcard string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Forks Array Field Commenting Workflow

This workflow tests that forks array fields are properly commented out in the on section.
`

			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := testFile[:len(testFile)-3] + ".lock.yml"
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockContent := string(content)

			// Check that the expected YAML structure is present
			if !strings.Contains(lockContent, tt.expectedYAML) {
				t.Errorf("Expected YAML structure not found in lock file.\nExpected:\n%s\nActual content:\n%s", tt.expectedYAML, lockContent)
			}

			// For test cases with forks field, ensure specific checks
			if strings.Contains(tt.frontmatter, "forks:") {
				// Check that the forks field is commented out
				if !strings.Contains(lockContent, "# forks:") {
					t.Errorf("Expected commented forks field but not found in lock file.\nContent:\n%s", lockContent)
				}

				// Check that the comment includes the explanation
				if !strings.Contains(lockContent, "# Fork filtering applied via job conditions") {
					t.Errorf("Expected forks comment to include explanation but not found in lock file.\nContent:\n%s", lockContent)
				}

				// Parse the generated YAML to ensure the forks field is not active in the parsed structure
				var workflow map[string]any
				if err := yaml.Unmarshal(content, &workflow); err != nil {
					t.Fatalf("Failed to parse generated YAML: %v", err)
				}

				if onSection, exists := workflow["on"]; exists {
					if onMap, ok := onSection.(map[string]any); ok {
						if prSection, hasPR := onMap["pull_request"]; hasPR {
							if prMap, isPRMap := prSection.(map[string]any); isPRMap {
								// The forks field should NOT be present in the parsed YAML (since it's commented)
								if _, hasForks := prMap["forks"]; hasForks {
									t.Errorf("Forks field found in parsed YAML pull_request section (should be commented): %v", prMap)
								}
							}
						}
					}
				}
			}

			// Ensure that active forks field is never present in the compiled YAML
			if strings.Contains(lockContent, "forks:") && !strings.Contains(lockContent, "# forks:") {
				t.Errorf("Active (non-commented) forks field found in compiled workflow content:\n%s", lockContent)
			}
		})
	}
}

func TestOnSectionWithQuotes(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "on-quotes-test")

	tests := []struct {
		name        string
		frontmatter string
		description string
	}{
		{
			name: "on section with reaction",
			frontmatter: `---
on:
  issues:
    types: [opened]
  pull_request:
    types: [opened]
  reaction: eyes
permissions:
  contents: read
  issues: write
  pull-requests: read
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			description: "Test that 'on' IS quoted when reaction is present",
		},
		{
			name: "on section with stop-after",
			frontmatter: `---
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  stop-after: +1h
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_commits]
---`,
			description: "Test that 'on' IS quoted when stop-after is present",
		},
		{
			name: "on section with both reaction and stop-after",
			frontmatter: `---
on:
  workflow_dispatch:
  issues:
    types: [opened]
  reaction: rocket
  stop-after: +3h
permissions:
  contents: read
  issues: write
  pull-requests: read
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			description: "Test that 'on' IS quoted when both reaction and stop-after are present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Workflow

` + tt.description

			testFile := filepath.Join(tmpDir, tt.name+".md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			// Parse the workflow
			workflowData, err := compiler.ParseWorkflowFile(testFile)
			if err != nil {
				t.Fatalf("Failed to parse workflow: %v", err)
			}

			// Generate YAML
			yamlContent, err := compiler.generateYAML(workflowData, testFile)
			if err != nil {
				t.Fatalf("Failed to generate YAML: %v", err)
			}

			// Check that "on": IS present (quoted form)
			if !strings.Contains(yamlContent, `"on":`) {
				t.Errorf("Generated YAML does not contain quoted 'on' keyword:\n%s", yamlContent)
			}

			// Additional verification: parse the generated YAML to ensure it's valid
			var workflow map[string]any
			if err := yaml.Unmarshal([]byte(yamlContent), &workflow); err != nil {
				t.Fatalf("Failed to parse generated YAML: %v", err)
			}

			// Verify the on section exists and is valid
			if _, hasOn := workflow["on"]; !hasOn {
				t.Error("Generated workflow missing 'on' section")
			}
		})
	}
}

// extractJobSection extracts a specific job section from the YAML content
