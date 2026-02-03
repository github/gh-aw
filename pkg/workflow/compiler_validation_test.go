//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
)

func TestExtractTopLevelYAMLSection_NestedEnvIssue(t *testing.T) {
	// This test verifies the fix for the nested env issue where
	// tools.mcps.*.env was being confused with top-level env
	compiler := NewCompiler()

	// Create frontmatter with nested env under tools.notionApi.env
	// but NO top-level env section
	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_dispatch": nil,
		},
		"timeout-minutes": 15,
		"permissions": map[string]any{
			"contents": "read",
			"models":   "read",
		},
		"tools": map[string]any{
			"notionApi": map[string]any{
				"type":    "stdio",
				"command": "docker",
				"args": []any{
					"run",
					"--rm",
					"-i",
					"-e", "NOTION_TOKEN",
					"mcp/notion",
				},
				"env": map[string]any{
					"NOTION_TOKEN": "{{ secrets.NOTION_TOKEN }}",
				},
			},
			"github": map[string]any{
				"allowed": []any{},
			},
			"claude": map[string]any{
				"allowed": map[string]any{
					"Read":  nil,
					"Write": nil,
					"Grep":  nil,
					"Glob":  nil,
				},
			},
		},
	}

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "top-level on section should be found",
			key:      "on",
			expected: "\"on\":\n  workflow_dispatch:",
		},
		{
			name:     "top-level timeout-minutes should be found",
			key:      "timeout-minutes",
			expected: "timeout-minutes: 15",
		},
		{
			name:     "top-level permissions should be found",
			key:      "permissions",
			expected: "permissions:\n  contents: read\n  models: read",
		},
		{
			name:     "nested env should NOT be found as top-level env",
			key:      "env",
			expected: "", // Should be empty since there's no top-level env
		},
		{
			name:     "top-level tools should be found",
			key:      "tools",
			expected: "tools:", // Should start with tools:
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.extractTopLevelYAMLSection(frontmatter, tt.key)

			if tt.expected == "" {
				if result != "" {
					t.Errorf("Expected empty result for key '%s', but got: %s", tt.key, result)
				}
			} else {
				if !strings.Contains(result, tt.expected) {
					t.Errorf("Expected result for key '%s' to contain '%s', but got: %s", tt.key, tt.expected, result)
				}
			}
		})
	}
}

func TestCompileWorkflowWithNestedEnv_NoOrphanedEnv(t *testing.T) {
	// This test verifies that workflows with nested env sections (like mcp-servers.*.env)
	// don't create orphaned env blocks in the generated YAML
	tmpDir := testutil.TempDir(t, "nested-env-test")

	// Create a workflow with nested env (similar to the original bug report)
	testContent := `---
on:
  workflow_dispatch:

timeout-minutes: 15
strict: false

permissions:
  contents: read
  models: read
  issues: read
  pull-requests: read

mcp-servers:
  notionApi:
    container: "mcp/notion"
    env:
      NOTION_TOKEN: "${{ secrets.NOTION_TOKEN }}"
tools:
  github:
    allowed: []
  edit:
---

# Test Workflow

This is a test workflow with nested env.
`

	testFile := filepath.Join(tmpDir, "test-nested-env.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Verify the generated YAML is valid by parsing it
	var yamlData map[string]any
	err = yaml.Unmarshal(content, &yamlData)
	if err != nil {
		t.Fatalf("Generated YAML is invalid: %v\nContent:\n%s", err, lockContent)
	}

	// Verify there's no orphaned env block at the top level
	// Look for the specific pattern that was causing the issue
	orphanedEnvPattern := `            env:
                NOTION_TOKEN:`
	if strings.Contains(lockContent, orphanedEnvPattern) {
		t.Errorf("Found orphaned env block in generated YAML:\n%s", lockContent)
	}

	// Verify the env section is properly placed in the MCP config
	if !strings.Contains(lockContent, `NOTION_TOKEN`) {
		t.Errorf("Expected MCP env configuration not found in generated YAML:\n%s", lockContent)
	}

	// Verify the workflow has the expected basic structure
	expectedSections := []string{
		"name:",
		"on:",
		"  workflow_dispatch:",
		"permissions:",
		"  contents: read",
		"  models: read",
		"jobs:",
		"  agent:",
		"    runs-on: ubuntu-latest",
	}

	for _, section := range expectedSections {
		if !strings.Contains(lockContent, section) {
			t.Errorf("Expected section '%s' not found in generated YAML:\n%s", section, lockContent)
		}
	}
}

func TestGeneratedDisclaimerInLockFile(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a simple test workflow
	testContent := `---
name: Test Workflow
on:
  schedule:
    - cron: "0 9 * * 1"
engine: claude
strict: false
tools:
  bash: ["echo 'hello'"]
---

# Test Workflow

This is a test workflow.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompilerWithVersion("v1.0.0")
	err := compiler.CompileWorkflow(testFile)
	if err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Verify the disclaimer is present
	// The first line may or may not include a version, so we check for the base message
	// For dev builds: "# This file was automatically generated by gh-aw. DO NOT EDIT."
	// For release builds: "# This file was automatically generated by gh-aw (version). DO NOT EDIT."
	if !strings.Contains(lockContent, "# This file was automatically generated by gh-aw") ||
		!strings.Contains(lockContent, "DO NOT EDIT") {
		t.Errorf("Expected auto-generated disclaimer not found in generated YAML:\n%s", lockContent)
	}

	expectedDisclaimer := []string{
		"# To update this file, edit the corresponding .md file and run:",
		"#   gh aw compile",
		"# For more information: https://github.com/github/gh-aw/blob/main/.github/aw/github-agentic-workflows.md",
	}

	for _, line := range expectedDisclaimer {
		if !strings.Contains(lockContent, line) {
			t.Errorf("Expected disclaimer line '%s' not found in generated YAML:\n%s", line, lockContent)
		}
	}

	// Verify the disclaimer appears at the beginning of the file
	lines := strings.Split(lockContent, "\n")
	if len(lines) < 4 {
		t.Fatalf("Generated file too short, expected at least 4 lines")
	}

	// Check that the first 4 lines are comment lines (disclaimer)
	for i := 0; i < 4; i++ {
		if !strings.HasPrefix(lines[i], "#") {
			t.Errorf("Line %d should be a comment (disclaimer), but got: %s", i+1, lines[i])
		}
	}

	// Find the first non-comment, non-empty line - this should be the workflow name
	var firstContentLine string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			firstContentLine = trimmed
			break
		}
	}

	// Check that the first content line starts the actual workflow with name:
	if !strings.HasPrefix(firstContentLine, "name:") {
		t.Errorf("First non-comment line should start with 'name:', but got: %s", firstContentLine)
	}
}

func TestValidateWorkflowSchema(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetSkipValidation(false) // Enable validation for testing

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid minimal workflow",
			yaml: `name: "Test Workflow"
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd`,
			wantErr: false,
		},
		{
			name: "invalid workflow - missing jobs",
			yaml: `name: "Test Workflow"
on: push`,
			wantErr: true,
			errMsg:  "missing property 'jobs'",
		},
		{
			name: "invalid workflow - invalid job structure",
			yaml: `name: "Test Workflow"
on: push
jobs:
  test:
    invalid-property: value`,
			wantErr: true,
			errMsg:  "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := compiler.validateGitHubActionsSchema(tt.yaml)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateGitHubActionsSchema() expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateGitHubActionsSchema() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateGitHubActionsSchema() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestBasicYAMLValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid YAML",
			yaml: `name: "Test Workflow"
on: push
jobs:
  test:
    runs-on: ubuntu-latest`,
			wantErr: false,
		},
		{
			name: "invalid YAML syntax",
			yaml: `name: "Test Workflow"
on: push
jobs:
  test: [invalid yaml structure`,
			wantErr: true,
			errMsg:  "sequence end token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic YAML validation (this is now always performed)
			var yamlTest any
			err := yaml.Unmarshal([]byte(tt.yaml), &yamlTest)

			if tt.wantErr {
				if err == nil {
					t.Errorf("YAML validation expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("YAML validation error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("YAML validation unexpected error = %v", err)
				}
			}
		})
	}
}
func TestValidationCanBeSkipped(t *testing.T) {
	compiler := NewCompiler()

	// Test via CompileWorkflow - should succeed because validation is skipped by default
	tmpDir := testutil.TempDir(t, "validation-skip-test")

	testContent := `---
name: Test Workflow
on: push
---
# Test workflow`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler.customOutput = tmpDir

	// This should succeed because validation is skipped by default
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Errorf("CompileWorkflow() should succeed when validation is skipped, but got error: %v", err)
	}
}

func TestFrontmatterEmbeddedInLockFile(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-embed-test")

	testContent := `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
timeout-minutes: 15
---

# Test Frontmatter Embedding

This workflow tests that frontmatter is NOT embedded in the lock file (removed per issue).
`

	testFile := filepath.Join(tmpDir, "test-frontmatter.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test-frontmatter.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(content)

	// Verify that the "Original Frontmatter:" comment is NOT present
	if strings.Contains(lockContent, "# Original Frontmatter:") {
		t.Error("Did not expect '# Original Frontmatter:' comment in lock file")
	}

	// Verify that the "Original Prompt:" comment is NOT present
	if strings.Contains(lockContent, "# Original Prompt:") {
		t.Error("Did not expect '# Original Prompt:' comment in lock file")
	}
}

// TestCompileWorkflowWithInvalidYAML tests that workflows with invalid YAML syntax
// produce properly formatted error messages with file:line:column information
func TestDescriptionFieldRendering(t *testing.T) {
	tmpDir := testutil.TempDir(t, "description-test")

	compiler := NewCompiler()

	tests := []struct {
		name                string
		frontmatter         string
		expectedDescription string
		description         string
	}{
		{
			name: "single_line_description",
			frontmatter: `---
description: "This is a simple workflow description"
on:
  push:
    branches: [main]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
tools:
  github:
    allowed: [list_commits]
---`,
			expectedDescription: "# This is a simple workflow description",
			description:         "Should render single-line description as comment",
		},
		{
			name: "multiline_description",
			frontmatter: `---
description: |
  This is a multi-line workflow description.
  It explains what the workflow does in detail.
  Each line should be rendered as a separate comment.
on:
  push:
    branches: [main]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
tools:
  github:
    allowed: [list_commits]
---`,
			expectedDescription: "# This is a multi-line workflow description.\n# It explains what the workflow does in detail.\n# Each line should be rendered as a separate comment.",
			description:         "Should render multi-line description with each line as comment",
		},
		{
			name: "no_description",
			frontmatter: `---
on:
  push:
    branches: [main]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
tools:
  github:
    allowed: [list_commits]
---`,
			expectedDescription: "",
			description:         "Should not render any description comments when no description is provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Workflow

This is a test workflow to verify description field rendering.
`

			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Unexpected error compiling workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read generated lock file: %v", err)
			}

			lockContent := string(content)

			if tt.expectedDescription == "" {
				// Verify no description comments are present
				// The standard header should end and workflow content should start immediately
				lines := strings.Split(lockContent, "\n")
				inHeader := true
				for i, line := range lines {
					if inHeader && strings.HasPrefix(line, "# For more information:") {
						// This is the last line of the standard header
						// Next non-empty line should be workflow content (name: ...)
						for j := i + 1; j < len(lines); j++ {
							if strings.TrimSpace(lines[j]) != "" {
								if strings.HasPrefix(lines[j], "#") && !strings.HasPrefix(lines[j], "# ") {
									// Found a comment that's not a space-prefixed description comment
									break
								}
								if strings.HasPrefix(lines[j], "# ") {
									t.Errorf("Found unexpected description comment when none expected: %s", lines[j])
								}
								break
							}
						}
						break
					}
				}
			} else {
				// Verify description comments are present
				if !strings.Contains(lockContent, tt.expectedDescription) {
					t.Errorf("Expected description comments not found in generated YAML:\nExpected: %s\nGenerated content:\n%s", tt.expectedDescription, lockContent)
				}

				// Verify description comes after standard header and before workflow content
				headerEndPattern := "# For more information: https://github.com/github/gh-aw/blob/main/.github/aw/github-agentic-workflows.md"
				workflowStartPattern := `name: "`

				headerPos := strings.Index(lockContent, headerEndPattern)
				descPos := strings.Index(lockContent, tt.expectedDescription)
				workflowPos := strings.Index(lockContent, workflowStartPattern)

				if headerPos == -1 {
					t.Error("Standard header not found in generated YAML")
				}
				if descPos == -1 {
					t.Error("Description comments not found in generated YAML")
				}
				if workflowPos == -1 {
					t.Error("Workflow content not found in generated YAML")
				}

				if headerPos >= descPos {
					t.Error("Description should come after standard header")
				}
				if descPos >= workflowPos {
					t.Error("Description should come before workflow content")
				}
			}

			// Clean up generated lock file
			os.Remove(lockFile)
		})
	}
}

func TestGenerateJobName(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name         string
		workflowName string
		expected     string
	}{
		{
			name:         "simple name",
			workflowName: "Test Workflow",
			expected:     "test-workflow",
		},
		{
			name:         "name with special characters",
			workflowName: "The Linter Maniac",
			expected:     "the-linter-maniac",
		},
		{
			name:         "name with colon",
			workflowName: "Playground: Everything Echo Test",
			expected:     "playground-everything-echo-test",
		},
		{
			name:         "name with parentheses",
			workflowName: "Daily Plan (Automatic)",
			expected:     "daily-plan-automatic",
		},
		{
			name:         "name with slashes",
			workflowName: "CI/CD Pipeline",
			expected:     "ci-cd-pipeline",
		},
		{
			name:         "name with quotes",
			workflowName: "Test \"Production\" System",
			expected:     "test-production-system",
		},
		{
			name:         "name with multiple spaces",
			workflowName: "Multiple   Spaces   Test",
			expected:     "multiple-spaces-test",
		},
		{
			name:         "single word",
			workflowName: "Build",
			expected:     "build",
		},
		{
			name:         "empty string",
			workflowName: "",
			expected:     "workflow-",
		},
		{
			name:         "starts with number",
			workflowName: "2024 Release",
			expected:     "workflow-2024-release",
		},
		{
			name:         "name with @ symbol",
			workflowName: "@mergefest - Merge Parent Branch Changes",
			expected:     "mergefest-merge-parent-branch-changes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.generateJobName(tt.workflowName)
			if result != tt.expected {
				t.Errorf("generateJobName(%q) = %q, expected %q", tt.workflowName, result, tt.expected)
			}
		})
	}
}

// TestOnSectionWithQuotes tests that the "on" keyword IS quoted in the generated YAML
// to prevent YAML parsers from interpreting it as a boolean value
