//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestProcessIncludes(t *testing.T) {
	// Create temporary test files
	tempDir, err := os.MkdirTemp("", "test_includes")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test file with markdown content
	testFile := filepath.Join(tempDir, "test.md")
	testContent := `---
tools:
  bash:
    allowed: ["ls", "cat"]
---

# Test Content
This is a test file content.
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create test file with extra newlines for trimming test
	testFileWithNewlines := filepath.Join(tempDir, "test-newlines.md")
	testContentWithNewlines := `

# Content with Extra Newlines
Some content here.


`
	if err := os.WriteFile(testFileWithNewlines, []byte(testContentWithNewlines), 0644); err != nil {
		t.Fatalf("Failed to write test file with newlines: %v", err)
	}

	tests := []struct {
		name         string
		content      string
		baseDir      string
		extractTools bool
		expected     string
		wantErr      bool
	}{
		{
			name:         "no includes",
			content:      "# Title\nRegular content",
			baseDir:      tempDir,
			extractTools: false,
			expected:     "# Title\nRegular content\n",
		},
		{
			name:         "simple include",
			content:      "@include test.md\n# After include",
			baseDir:      tempDir,
			extractTools: false,
			expected:     "# Test Content\nThis is a test file content.\n# After include\n",
		},
		{
			name:         "extract tools",
			content:      "@include test.md",
			baseDir:      tempDir,
			extractTools: true,
			expected:     `{"bash":{"allowed":["ls","cat"]}}` + "\n",
		},
		{
			name:         "file not found",
			content:      "@include nonexistent.md",
			baseDir:      tempDir,
			extractTools: false,
			wantErr:      true, // Now expects error instead of embedding comment
		},
		{
			name:         "include file with extra newlines",
			content:      "@include test-newlines.md\n# After include",
			baseDir:      tempDir,
			extractTools: false,
			expected:     "# Content with Extra Newlines\nSome content here.\n# After include\n",
		},
		{
			name:         "simple import (alias for include)",
			content:      "@import test.md\n# After import",
			baseDir:      tempDir,
			extractTools: false,
			expected:     "# Test Content\nThis is a test file content.\n# After import\n",
		},
		{
			name:         "extract tools with import",
			content:      "@import test.md",
			baseDir:      tempDir,
			extractTools: true,
			expected:     `{"bash":{"allowed":["ls","cat"]}}` + "\n",
		},
		{
			name:         "import file not found",
			content:      "@import nonexistent.md",
			baseDir:      tempDir,
			extractTools: false,
			wantErr:      true,
		},
		{
			name:         "optional import missing file",
			content:      "@import? missing.md\n",
			baseDir:      tempDir,
			extractTools: false,
			expected:     "",
		},
	}

	// Create test file with invalid frontmatter for testing validation
	invalidFile := filepath.Join(tempDir, "invalid.md")
	invalidContent := `---
title: Invalid File
on: push
tools:
  bash:
    allowed: ["ls"]
---

# Invalid Content
This file has invalid frontmatter for an included file.
`
	if err := os.WriteFile(invalidFile, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid test file: %v", err)
	}

	// Add test case for invalid frontmatter in included file (should now pass with warnings for non-workflow files)
	tests = append(tests, struct {
		name         string
		content      string
		baseDir      string
		extractTools bool
		expected     string
		wantErr      bool
	}{
		name:         "invalid frontmatter in included file",
		content:      "@include invalid.md",
		baseDir:      tempDir,
		extractTools: false,
		expected:     "# Invalid Content\nThis file has invalid frontmatter for an included file.\n",
		wantErr:      false,
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProcessIncludes(tt.content, tt.baseDir, tt.extractTools)

			if tt.wantErr && err == nil {
				t.Errorf("ProcessIncludes() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ProcessIncludes() error = %v", err)
				return
			}

			// Special handling for the invalid frontmatter test case - it should now pass with warnings
			if tt.name == "invalid frontmatter in included file" {
				// Check that the content was successfully included
				if !strings.Contains(result, "# Invalid Content") {
					t.Errorf("ProcessIncludes() = %q, expected to contain '# Invalid Content'", result)
				}
				return
			}

			if result != tt.expected {
				t.Errorf("ProcessIncludes() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestProcessIncludesConditionalValidation(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "test_conditional_validation")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create .github/workflows directory structure
	workflowsDir := filepath.Join(tempDir, constants.GetWorkflowDir())
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows dir: %v", err)
	}

	// Create docs directory for non-workflow files
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs dir: %v", err)
	}

	// Test file 1: Valid workflow file (should pass strict validation)
	validWorkflowFile := filepath.Join(workflowsDir, "valid.md")
	validWorkflowContent := `---
tools:
  github:
    allowed: [issue_read]
---

# Valid Workflow
This is a valid workflow file.`
	if err := os.WriteFile(validWorkflowFile, []byte(validWorkflowContent), 0644); err != nil {
		t.Fatalf("Failed to write valid workflow file: %v", err)
	}

	// Test file 2: Invalid workflow file (should fail strict validation)
	invalidWorkflowFile := filepath.Join(workflowsDir, "invalid.md")
	invalidWorkflowContent := `---
title: Invalid Field
on: push
tools:
  github:
    allowed: [issue_read]
---

# Invalid Workflow
This has invalid frontmatter fields.`
	if err := os.WriteFile(invalidWorkflowFile, []byte(invalidWorkflowContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid workflow file: %v", err)
	}

	// Test file 2.5: Invalid non-workflow file (should pass with warnings)
	invalidNonWorkflowFile := filepath.Join(docsDir, "invalid-external.md")
	invalidNonWorkflowContent := `---
title: Invalid Field
on: push
tools:
  github:
    allowed: [issue_read]
---

# Invalid External File
This has invalid frontmatter fields but it's outside workflows dir.`
	if err := os.WriteFile(invalidNonWorkflowFile, []byte(invalidNonWorkflowContent), 0644); err != nil {
		t.Fatalf("Failed to write invalid non-workflow file: %v", err)
	}

	// Test file 3: Agent instructions file (should pass with warnings)
	agentFile := filepath.Join(docsDir, "agent-instructions.md")
	agentContent := `---
description: Agent instructions
applyTo: "**/*.py"
temperature: 0.7
tools:
  github:
    allowed: [issue_read]
---

# Agent Instructions
These are instructions for AI agents.`
	if err := os.WriteFile(agentFile, []byte(agentContent), 0644); err != nil {
		t.Fatalf("Failed to write agent file: %v", err)
	}

	// Test file 4: Plain markdown file (no frontmatter)
	plainFile := filepath.Join(docsDir, "plain.md")
	plainContent := `# Plain Markdown
This is just plain markdown content with no frontmatter.`
	if err := os.WriteFile(plainFile, []byte(plainContent), 0644); err != nil {
		t.Fatalf("Failed to write plain file: %v", err)
	}

	tests := []struct {
		name         string
		content      string
		baseDir      string
		extractTools bool
		wantErr      bool
		checkContent string
	}{
		{
			name:         "valid workflow file inclusion",
			content:      "@include .github/workflows/valid.md",
			baseDir:      tempDir,
			extractTools: false,
			wantErr:      false,
			checkContent: "# Valid Workflow",
		},
		{
			name:         "invalid workflow file inclusion should fail",
			content:      "@include .github/workflows/invalid.md",
			baseDir:      tempDir,
			extractTools: false,
			wantErr:      true, // Now expects error instead of embedding comment
		},
		{
			name:         "invalid non-workflow file inclusion should succeed with warnings",
			content:      "@include docs/invalid-external.md",
			baseDir:      tempDir,
			extractTools: false,
			wantErr:      false,
			checkContent: "# Invalid External File",
		},
		{
			name:         "agent instructions file inclusion should succeed",
			content:      "@include docs/agent-instructions.md",
			baseDir:      tempDir,
			extractTools: false,
			wantErr:      false,
			checkContent: "# Agent Instructions",
		},
		{
			name:         "plain markdown file inclusion should succeed",
			content:      "@include docs/plain.md",
			baseDir:      tempDir,
			extractTools: false,
			wantErr:      false,
			checkContent: "# Plain Markdown",
		},
		{
			name:         "extract tools from valid workflow file",
			content:      "@include .github/workflows/valid.md",
			baseDir:      tempDir,
			extractTools: true,
			wantErr:      false,
			checkContent: `{"github":{"allowed":["issue_read"]}}`,
		},
		{
			name:         "extract tools from agent file",
			content:      "@include docs/agent-instructions.md",
			baseDir:      tempDir,
			extractTools: true,
			wantErr:      false,
			checkContent: `{"github":{"allowed":["issue_read"]}}`,
		},
		{
			name:         "extract tools from plain file (no tools)",
			content:      "@include docs/plain.md",
			baseDir:      tempDir,
			extractTools: true,
			wantErr:      false,
			checkContent: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProcessIncludes(tt.content, tt.baseDir, tt.extractTools)

			if tt.wantErr && err == nil {
				t.Errorf("ProcessIncludes() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ProcessIncludes() error = %v", err)
				return
			}

			if !tt.wantErr && tt.checkContent != "" {
				if !strings.Contains(result, tt.checkContent) {
					t.Errorf("ProcessIncludes() result = %q, expected to contain %q", result, tt.checkContent)
				}
			}
		})
	}
}

func TestExpandIncludes(t *testing.T) {
	// Create temporary test files
	tempDir, err := os.MkdirTemp("", "test_expand")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create go.mod to make it project root for component resolution
	goModFile := filepath.Join(tempDir, "go.mod")
	if err := os.WriteFile(goModFile, []byte("module test"), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	// Create test file
	testFile := filepath.Join(tempDir, "test.md")
	testContent := `---
tools:
  bash:
    allowed: ["ls"]
---

# Test Content
This is test content.
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	tests := []struct {
		name         string
		content      string
		baseDir      string
		extractTools bool
		wantContains string
		wantErr      bool
	}{
		{
			name:         "expand markdown content",
			content:      "# Start\n@include test.md\n# End",
			baseDir:      tempDir,
			extractTools: false,
			wantContains: "# Test Content\nThis is test content.",
		},
		{
			name:         "expand tools",
			content:      "@include test.md",
			baseDir:      tempDir,
			extractTools: true,
			wantContains: `"bash"`,
		},
		{
			name:         "expand markdown content with import",
			content:      "# Start\n@import test.md\n# End",
			baseDir:      tempDir,
			extractTools: false,
			wantContains: "# Test Content\nThis is test content.",
		},
		{
			name:         "expand tools with import",
			content:      "@import test.md",
			baseDir:      tempDir,
			extractTools: true,
			wantContains: `"bash"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExpandIncludes(tt.content, tt.baseDir, tt.extractTools)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ExpandIncludes() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ExpandIncludes() error = %v", err)
				return
			}

			if !strings.Contains(result, tt.wantContains) {
				t.Errorf("ExpandIncludes() = %q, want to contain %q", result, tt.wantContains)
			}
		})
	}
}

// Test ExtractWorkflowNameFromMarkdown function
func TestProcessIncludesOptional(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "test_optional_includes")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create an existing include file
	existingFile := filepath.Join(tempDir, "existing.md")
	existingContent := "# Existing Include\nThis file exists."
	if err := os.WriteFile(existingFile, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing file: %v", err)
	}

	tests := []struct {
		name           string
		content        string
		extractTools   bool
		expectedOutput string
		expectError    bool
	}{
		{
			name:           "regular include existing file",
			content:        "@include existing.md\n",
			extractTools:   false,
			expectedOutput: existingContent,
		},
		{
			name:         "regular include missing file",
			content:      "@include missing.md\n",
			extractTools: false,
			expectError:  true, // Now expects error instead of embedding comment
		},
		{
			name:           "optional include existing file",
			content:        "@include? existing.md\n",
			extractTools:   false,
			expectedOutput: existingContent,
		},
		{
			name:           "optional include missing file",
			content:        "@include? missing.md\n",
			extractTools:   false,
			expectedOutput: "", // No content added, friendly message goes to stdout
		},
		{
			name:           "optional include missing file extract tools",
			content:        "@include? missing.md\n",
			extractTools:   true,
			expectedOutput: "",
		},
		{
			name:         "regular include missing file extract tools",
			content:      "@include missing.md\n",
			extractTools: true,
			expectError:  true, // Now expects error instead of returning {}
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProcessIncludes(tt.content, tempDir, tt.extractTools)

			if tt.expectError {
				if err == nil {
					t.Errorf("ProcessIncludes expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ProcessIncludes unexpected error: %v", err)
				return
			}

			if !strings.Contains(result, tt.expectedOutput) {
				t.Errorf("ProcessIncludes output = %q, expected to contain %q", result, tt.expectedOutput)
			}
		})
	}
}

func TestProcessIncludesWithCycleDetection(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "test_cycle_detection")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create file A that includes file B
	fileA := filepath.Join(tempDir, "fileA.md")
	if err := os.WriteFile(fileA, []byte("# File A\n@include fileB.md\n"), 0644); err != nil {
		t.Fatalf("Failed to write fileA: %v", err)
	}

	// Create file B that includes file A (creating a cycle)
	fileB := filepath.Join(tempDir, "fileB.md")
	if err := os.WriteFile(fileB, []byte("# File B\n@include fileA.md\n"), 0644); err != nil {
		t.Fatalf("Failed to write fileB: %v", err)
	}

	// Process includes from file A - should not hang due to cycle detection
	content := "# Main\n@include fileA.md\n"
	result, err := ProcessIncludes(content, tempDir, false)

	if err != nil {
		t.Errorf("ProcessIncludes with cycle should not error: %v", err)
	}

	// Result should contain content from fileA and fileB, but cycle should be prevented
	if !strings.Contains(result, "File A") {
		t.Errorf("ProcessIncludes result should contain File A content")
	}
	if !strings.Contains(result, "File B") {
		t.Errorf("ProcessIncludes result should contain File B content")
	}
}

func TestProcessIncludedFileWithNameAndDescription(t *testing.T) {
	tempDir := t.TempDir()
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Create a test file with name and description fields
	testFile := filepath.Join(docsDir, "shared-config.md")
	testContent := `---
name: Shared Configuration
description: Common tools and configuration for workflows
tools:
  github:
    allowed: [issue_read]
---

# Shared Configuration

This is a shared configuration file.`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Process the included file - should not generate warnings for name and description
	result, err := processIncludedFileWithVisited(testFile, "", false, make(map[string]bool))
	if err != nil {
		t.Fatalf("processIncludedFileWithVisited() error = %v", err)
	}

	if !strings.Contains(result, "# Shared Configuration") {
		t.Errorf("Expected markdown content not found in result")
	}

	// The test should pass without warnings being printed to stderr
	// We can't easily capture stderr in this test, but the absence of an error
	// indicates that the file was processed successfully
}

// TestProcessIncludedFileWithOnlyNameAndDescription verifies that files with only
// name and description fields (and no other fields) are processed without warnings
func TestProcessIncludedFileWithOnlyNameAndDescription(t *testing.T) {
	tempDir := t.TempDir()
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Create a test file with only name and description fields
	testFile := filepath.Join(docsDir, "minimal-config.md")
	testContent := `---
name: Minimal Configuration
description: A minimal configuration with just metadata
---

# Minimal Configuration

This file only has name and description in frontmatter.`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Process the included file - should not generate warnings
	result, err := processIncludedFileWithVisited(testFile, "", false, make(map[string]bool))
	if err != nil {
		t.Fatalf("processIncludedFileWithVisited() error = %v", err)
	}

	if !strings.Contains(result, "# Minimal Configuration") {
		t.Errorf("Expected markdown content not found in result")
	}
}

// TestProcessIncludedFileWithInferField verifies that the "infer" field
// (used in custom agent format) is accepted without warnings
func TestProcessIncludedFileWithInferField(t *testing.T) {
	tempDir := t.TempDir()
	agentsDir := filepath.Join(tempDir, ".github", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create agents directory: %v", err)
	}

	// Create a test file with the "infer" field (custom agent format)
	testFile := filepath.Join(agentsDir, "test-agent.agent.md")
	testContent := `---
name: Test Agent
description: A test custom agent
infer: false
---

# Test Agent

This is a custom agent file with the infer field.`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Process the included file - should not generate warnings
	result, err := processIncludedFileWithVisited(testFile, "", false, make(map[string]bool))
	if err != nil {
		t.Fatalf("processIncludedFileWithVisited() error = %v", err)
	}

	if !strings.Contains(result, "# Test Agent") {
		t.Errorf("Expected markdown content not found in result")
	}
}

// TestProcessIncludedFileWithAgentToolsArray verifies that custom agent files
// with tools as an array (GitHub Copilot format) are processed without validation errors
func TestProcessIncludedFileWithAgentToolsArray(t *testing.T) {
	tempDir := t.TempDir()
	agentsDir := filepath.Join(tempDir, ".github", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create agents directory: %v", err)
	}

	// Create a test file with tools as an array (custom agent format)
	testFile := filepath.Join(agentsDir, "feature-flag-remover.agent.md")
	testContent := `---
description: "Removes feature flags from codebase"
tools:
  [
    "edit",
    "search",
    "execute/getTerminalOutput",
    "execute/runInTerminal",
    "read/terminalLastCommand",
    "read/terminalSelection",
    "execute/createAndRunTask",
    "execute/getTaskOutput",
    "execute/runTask",
    "read/problems",
    "search/changes",
    "agent",
    "runTasks",
    "problems",
    "changes",
    "runSubagent",
  ]
---

# Feature Flag Remover Agent

This agent removes feature flags from the codebase.`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Process the included file - should not generate validation errors
	// because custom agent files use a different tools format (array vs object)
	result, err := processIncludedFileWithVisited(testFile, "", false, make(map[string]bool))
	if err != nil {
		t.Fatalf("processIncludedFileWithVisited() error = %v, want nil", err)
	}

	if !strings.Contains(result, "# Feature Flag Remover Agent") {
		t.Errorf("Expected markdown content not found in result")
	}

	// Also test that tools extraction skips agent files and returns empty object
	toolsResult, err := processIncludedFileWithVisited(testFile, "", true, make(map[string]bool))
	if err != nil {
		t.Fatalf("processIncludedFileWithVisited(extractTools=true) error = %v, want nil", err)
	}

	if toolsResult != "{}" {
		t.Errorf("processIncludedFileWithVisited(extractTools=true) = %q, want {}", toolsResult)
	}
}

// TestProcessIncludedFileWithEngineCommand verifies that included files
// with engine.command property are processed without validation errors
func TestProcessIncludedFileWithEngineCommand(t *testing.T) {
	tempDir := t.TempDir()
	docsDir := filepath.Join(tempDir, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs directory: %v", err)
	}

	// Create a test file with engine.command property
	testFile := filepath.Join(docsDir, "engine-config.md")
	testContent := `---
engine:
  id: copilot
  command: /custom/path/to/copilot
  version: "1.0.0"
tools:
  github:
    allowed: [issue_read]
---

# Engine Configuration

This is a shared engine configuration with custom command.`

	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Process the included file - should not generate validation errors
	result, err := processIncludedFileWithVisited(testFile, "", false, make(map[string]bool))
	if err != nil {
		t.Fatalf("processIncludedFileWithVisited() error = %v, want nil", err)
	}

	if !strings.Contains(result, "# Engine Configuration") {
		t.Errorf("Expected markdown content not found in result")
	}

	// Also test that tools extraction works correctly
	toolsResult, err := processIncludedFileWithVisited(testFile, "", true, make(map[string]bool))
	if err != nil {
		t.Fatalf("processIncludedFileWithVisited(extractTools=true) error = %v, want nil", err)
	}

	if !strings.Contains(toolsResult, `"github"`) {
		t.Errorf("processIncludedFileWithVisited(extractTools=true) should contain github tools, got: %q", toolsResult)
	}
}
