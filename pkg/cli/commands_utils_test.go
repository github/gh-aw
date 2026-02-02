//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestExtractWorkflowNameFromFile(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "test-*")

	tests := []struct {
		name        string
		content     string
		filename    string
		expected    string
		expectError bool
	}{
		{
			name: "file with H1 header",
			content: `---
title: Test Workflow
---

# Daily Test Coverage Improvement

This is a test workflow.`,
			filename:    "test-workflow.md",
			expected:    "Daily Test Coverage Improvement",
			expectError: false,
		},
		{
			name: "file with H1 header with extra spaces",
			content: `# Weekly Research   

This is a research workflow.`,
			filename:    "weekly-research.md",
			expected:    "Weekly Research",
			expectError: false,
		},
		{
			name: "file without H1 header - generates from filename",
			content: `This is content without H1 header.

## Some H2 header

Content here.`,
			filename:    "daily-dependency-updates.md",
			expected:    "Daily Dependency Updates",
			expectError: false,
		},
		{
			name:        "file with complex filename",
			content:     `No headers here.`,
			filename:    "complex-workflow-name-test.md",
			expected:    "Complex Workflow Name Test",
			expectError: false,
		},
		{
			name:        "file with single word filename",
			content:     `No headers.`,
			filename:    "workflow.md",
			expected:    "Workflow",
			expectError: false,
		},
		{
			name:        "empty file - generates from filename",
			content:     "",
			filename:    "empty-workflow.md",
			expected:    "Empty Workflow",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			filePath := filepath.Join(tmpDir, tt.filename)
			err := os.WriteFile(filePath, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Test the function
			result, err := extractWorkflowNameFromFile(filePath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %q, got %q", tt.expected, result)
				}
			}
		})
	}
}

func TestExtractWorkflowNameFromFile_NonExistentFile(t *testing.T) {
	_, err := extractWorkflowNameFromFile("/nonexistent/file.md")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestUpdateWorkflowTitle(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		number   int
		expected string
	}{
		{
			name: "content with H1 header",
			content: `---
title: Test
---

# Daily Test Coverage

This is a workflow.`,
			number: 2,
			expected: `---
title: Test
---

# Daily Test Coverage 2

This is a workflow.`,
		},
		{
			name: "content with H1 header with extra spaces",
			content: `   # Weekly Research   

Content here.`,
			number: 3,
			expected: `# # Weekly Research 3

Content here.`,
		},
		{
			name: "content without H1 header",
			content: `## H2 Header

Content without H1.`,
			number: 1,
			expected: `## H2 Header

Content without H1.`,
		},
		{
			name:     "empty content",
			content:  "",
			number:   1,
			expected: "",
		},
		{
			name: "multiple H1 headers - only first is modified",
			content: `# First Header

Some content.

# Second Header

More content.`,
			number: 5,
			expected: `# First Header 5

Some content.

# Second Header

More content.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := updateWorkflowTitle(tt.content, tt.number)
			if result != tt.expected {
				t.Errorf("Expected:\n%s\n\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

func TestIsGitRepo(t *testing.T) {
	// Test in current directory (should be a git repo based on project setup)
	result := isGitRepo()

	// Since we're running in a git repository, this should return true
	if !result {
		t.Error("Expected isGitRepo() to return true in git repository")
	}
}

// TestFindGitRoot is already tested in gitroot_test.go, skipping duplicate

func TestExtractWorkflowNameFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple workflow file",
			path:     ".github/workflows/daily-test.lock.yml",
			expected: "daily-test",
		},
		{
			name:     "workflow file without lock suffix",
			path:     ".github/workflows/weekly-research.yml",
			expected: "weekly-research",
		},
		{
			name:     "nested path",
			path:     "/home/user/project/.github/workflows/complex-workflow-name.lock.yml",
			expected: "complex-workflow-name",
		},
		{
			name:     "file without extension",
			path:     ".github/workflows/workflow",
			expected: "workflow",
		},
		{
			name:     "single file name",
			path:     "test.yml",
			expected: "test",
		},
		{
			name:     "file with multiple dots",
			path:     "test.lock.yml",
			expected: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractWorkflowNameFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFindIncludesInContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "no includes",
			content:  "This is regular content without includes.",
			expected: []string{},
		},
		{
			name: "single include",
			content: `This is content with include:
@include shared/tools.md
More content here.`,
			expected: []string{"shared/tools.md"},
		},
		{
			name: "multiple includes",
			content: `Content with multiple includes:
@include shared/tools.md
Some content between.
@include shared/config.md
More content.
@include another/file.md`,
			expected: []string{"shared/tools.md", "shared/config.md", "another/file.md"},
		},
		{
			name: "includes with different whitespace",
			content: `Content:
@include shared/tools.md
@include  shared/config.md  
@include	shared/tabs.md`,
			expected: []string{"shared/tools.md", "shared/config.md", "shared/tabs.md"},
		},
		{
			name: "includes with section references",
			content: `Content:
@include shared/tools.md#Tools
@include shared/config.md#Configuration`,
			expected: []string{"shared/tools.md", "shared/config.md"},
		},
		{
			name:     "empty content",
			content:  "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := findIncludesInContent(tt.content, "", false)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d includes, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, expected := range tt.expected {
				if i >= len(result) || result[i] != expected {
					t.Errorf("Expected include %d to be %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

// Benchmark tests
func BenchmarkExtractWorkflowNameFromFile(b *testing.B) {
	// Create temporary test file
	tmpDir := b.TempDir()
	content := `---
title: Test Workflow
---

# Daily Test Coverage Improvement

This is a test workflow with some content.`

	filePath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		b.Fatalf("Failed to create test file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = extractWorkflowNameFromFile(filePath)
	}
}

func BenchmarkUpdateWorkflowTitle(b *testing.B) {
	content := `---
title: Test
---

# Daily Test Coverage

This is a workflow with some content that needs title updating.`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = updateWorkflowTitle(content, i+1)
	}
}

func BenchmarkFindIncludesInContent(b *testing.B) {
	content := `This is content with includes:
@include shared/tools.md
Some content between includes.
@include shared/config.md
More content here.
@include another/file.md
Final content.`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = findIncludesInContent(content, "", false)
	}
}

func TestCopyMarkdownFiles(t *testing.T) {
	tests := []struct {
		name           string
		sourceFiles    map[string]string // path -> content
		expectedTarget map[string]string // relative path -> content
		verbose        bool
		expectError    bool
	}{
		{
			name: "copy single markdown file",
			sourceFiles: map[string]string{
				"workflow.md": `# Test Workflow
This is a test workflow.`,
			},
			expectedTarget: map[string]string{
				"workflow.md": `# Test Workflow
This is a test workflow.`,
			},
			verbose:     false,
			expectError: false,
		},
		{
			name: "copy multiple markdown files",
			sourceFiles: map[string]string{
				"daily.md": `# Daily Workflow
Daily tasks`,
				"weekly.md": `# Weekly Workflow
Weekly tasks`,
			},
			expectedTarget: map[string]string{
				"daily.md": `# Daily Workflow
Daily tasks`,
				"weekly.md": `# Weekly Workflow
Weekly tasks`,
			},
			verbose:     false,
			expectError: false,
		},
		{
			name: "copy markdown files in subdirectories",
			sourceFiles: map[string]string{
				"workflows/daily.md": `# Daily
Content`,
				"workflows/weekly.md": `# Weekly
Content`,
				"shared/utils.md": `# Utils
Shared content`,
			},
			expectedTarget: map[string]string{
				"workflows/daily.md": `# Daily
Content`,
				"workflows/weekly.md": `# Weekly
Content`,
				"shared/utils.md": `# Utils
Shared content`,
			},
			verbose:     true,
			expectError: false,
		},
		{
			name: "skip non-markdown files",
			sourceFiles: map[string]string{
				"workflow.md": `# Test Workflow`,
				"config.yaml": `name: test`,
				"readme.txt":  `This is a readme`,
				"script.sh":   `#!/bin/bash\necho "hello"`,
			},
			expectedTarget: map[string]string{
				"workflow.md": `# Test Workflow`,
			},
			verbose:     false,
			expectError: false,
		},
		{
			name: "handle empty source directory",
			sourceFiles: map[string]string{
				"not-markdown.txt": `This won't be copied`,
			},
			expectedTarget: map[string]string{},
			verbose:        false,
			expectError:    false,
		},
		{
			name: "copy nested markdown files with complex structure",
			sourceFiles: map[string]string{
				"level1/workflow1.md":               `# Level 1 Workflow 1`,
				"level1/level2/workflow2.md":        `# Level 2 Workflow 2`,
				"level1/level2/level3/workflow3.md": `# Level 3 Workflow 3`,
				"other.txt":                         `Not copied`,
			},
			expectedTarget: map[string]string{
				"level1/workflow1.md":               `# Level 1 Workflow 1`,
				"level1/level2/workflow2.md":        `# Level 2 Workflow 2`,
				"level1/level2/level3/workflow3.md": `# Level 3 Workflow 3`,
			},
			verbose:     false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary source and target directories
			sourceDir := testutil.TempDir(t, "test-*")
			targetDir := testutil.TempDir(t, "test-*")

			// Create source files
			for path, content := range tt.sourceFiles {
				fullPath := filepath.Join(sourceDir, path)
				// Create directory if needed
				dir := filepath.Dir(fullPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("Failed to create source directory %s: %v", dir, err)
				}
				// Write file
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to create source file %s: %v", fullPath, err)
				}
			}

			// Test the function
			err := copyMarkdownFiles(sourceDir, targetDir, tt.verbose)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}
			}

			// Verify expected files were copied
			for expectedPath, expectedContent := range tt.expectedTarget {
				fullTargetPath := filepath.Join(targetDir, expectedPath)

				// Check if file exists
				if _, err := os.Stat(fullTargetPath); os.IsNotExist(err) {
					t.Errorf("Expected file %s was not copied", expectedPath)
					continue
				}

				// Check file content
				content, err := os.ReadFile(fullTargetPath)
				if err != nil {
					t.Errorf("Failed to read copied file %s: %v", expectedPath, err)
					continue
				}

				if string(content) != expectedContent {
					t.Errorf("File %s content mismatch:\nExpected: %q\nGot: %q",
						expectedPath, expectedContent, string(content))
				}
			}

			// Verify no unexpected files were copied (check that only .md files exist)
			err = filepath.Walk(targetDir, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}

				if !info.IsDir() {
					relPath, err := filepath.Rel(targetDir, path)
					if err != nil {
						return err
					}

					// All files in target should be .md files
					if !strings.HasSuffix(relPath, ".md") {
						t.Errorf("Unexpected non-markdown file copied: %s", relPath)
					}
				}
				return nil
			})

			if err != nil {
				t.Errorf("Error walking target directory: %v", err)
			}
		})
	}
}

func TestCopyMarkdownFiles_ErrorScenarios(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (sourceDir, targetDir string, cleanup func())
		expectError bool
		errorText   string
	}{
		{
			name: "nonexistent source directory",
			setup: func() (string, string, func()) {
				targetDir := testutil.TempDir(t, "test-*")
				return "/nonexistent/source", targetDir, func() {}
			},
			expectError: true,
			errorText:   "no such file or directory",
		},
		{
			name: "permission denied on target directory",
			setup: func() (string, string, func()) {
				sourceDir := testutil.TempDir(t, "test-*")
				targetDir := testutil.TempDir(t, "test-*")

				// Create a source file
				sourceFile := filepath.Join(sourceDir, "test.md")
				os.WriteFile(sourceFile, []byte("# Test"), 0644)

				// Make target directory read-only
				os.Chmod(targetDir, 0444)

				cleanup := func() {
					os.Chmod(targetDir, 0755) // Restore permissions for cleanup
				}

				return sourceDir, targetDir, cleanup
			},
			expectError: true,
			errorText:   "permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip permission tests when running as root (e.g., in Docker containers)
			// Root can write to read-only directories, bypassing Unix permission checks
			if tt.name == "permission denied on target directory" && os.Geteuid() == 0 {
				t.Skip("Skipping permission test when running as root")
			}

			sourceDir, targetDir, cleanup := tt.setup()
			defer cleanup()

			err := copyMarkdownFiles(sourceDir, targetDir, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorText != "" && !sliceutil.ContainsIgnoreCase(err.Error(), tt.errorText) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorText, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestIsRunnable(t *testing.T) {
	tests := []struct {
		name         string
		mdContent    string
		lockContent  string
		expected     bool
		expectError  bool
		errorMessage string
	}{
		{
			name: "workflow with schedule trigger",
			mdContent: `---
on:
  schedule:
    - cron: "0 9 * * *"
---
# Test Workflow
This workflow runs on schedule.`,
			lockContent: `name: "Test Workflow"
on:
  schedule:
    - cron: "0 9 * * *"
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    true,
			expectError: false,
		},
		{
			name: "workflow with workflow_dispatch trigger",
			mdContent: `---
on:
  workflow_dispatch:
---
# Manual Workflow
This workflow can be triggered manually.`,
			lockContent: `name: "Manual Workflow"
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    true,
			expectError: false,
		},
		{
			name: "workflow with both schedule and workflow_dispatch",
			mdContent: `---
on:
  schedule:
    - cron: "0 9 * * 1"  
  workflow_dispatch:
  push:
    branches: [main]
---
# Mixed Triggers Workflow`,
			lockContent: `name: "Mixed Triggers Workflow"
on:
  schedule:
    - cron: "0 9 * * 1"
  workflow_dispatch:
  push:
    branches: [main]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    true,
			expectError: false,
		},
		{
			name: "workflow with only push trigger (not runnable)",
			mdContent: `---
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
---
# CI Workflow
This is not runnable via schedule or manual dispatch.`,
			lockContent: `name: "CI Workflow"
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    false,
			expectError: false,
		},
		{
			name: "workflow with no 'on' section (defaults to runnable)",
			mdContent: `---
name: Default Workflow
---
# Default Workflow
No on section means it defaults to runnable.`,
			lockContent: `name: "Default Workflow"
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    false,
			expectError: false,
		},
		{
			name: "workflow with cron trigger (alternative schedule format)",
			mdContent: `---
on:
  cron: "0 */6 * * *"
---
# Cron Workflow
Uses cron format directly.`,
			lockContent: `name: "Cron Workflow"
on:
  schedule:
    - cron: "0 */6 * * *"
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    true,
			expectError: false,
		},
		{
			name: "case insensitive schedule detection",
			mdContent: `---
on:
  SCHEDULE:
    - cron: "0 12 * * 0"
---
# Case Test Workflow`,
			lockContent: `name: "Case Test Workflow"
on:
  schedule:
    - cron: "0 12 * * 0"
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    true,
			expectError: false,
		},
		{
			name: "case insensitive workflow_dispatch detection",
			mdContent: `---
on:
  WORKFLOW_DISPATCH:
---
# Case Test Manual Workflow`,
			lockContent: `name: "Case Test Manual Workflow"
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    true,
			expectError: false,
		},
		{
			name: "complex on section with schedule buried in text",
			mdContent: `---
on:
  push:
    branches: [main]
  schedule:
    - cron: "0 0 * * 0"  # Weekly
  issues:
    types: [opened]
---
# Complex Workflow`,
			lockContent: `name: "Complex Workflow"
on:
  push:
    branches: [main]
  schedule:
    - cron: "0 0 * * 0"
  workflow_dispatch:
  issues:
    types: [opened]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    true,
			expectError: false,
		},
		{
			name: "empty on section (not runnable)",
			mdContent: `---
on: {}
---
# Empty On Section`,
			lockContent: `name: "Empty On Section"
on: {}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    false,
			expectError: false,
		},
		{
			name: "malformed frontmatter",
			mdContent: `---
invalid yaml structure {
on:
  schedule
---
# Malformed YAML`,
			lockContent:  `invalid yaml`,
			expected:     false,
			expectError:  true,
			errorMessage: "failed to parse lock file YAML",
		},
		{
			name: "no frontmatter at all (defaults to runnable)",
			mdContent: `# Simple Markdown
This file has no frontmatter.
Just plain markdown content.`,
			lockContent: `name: "Simple Markdown"
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expected:    true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary test files
			tmpDir := testutil.TempDir(t, "test-*")
			mdPath := filepath.Join(tmpDir, "test-workflow.md")
			lockPath := filepath.Join(tmpDir, "test-workflow.lock.yml")

			err := os.WriteFile(mdPath, []byte(tt.mdContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create markdown file: %v", err)
			}

			err = os.WriteFile(lockPath, []byte(tt.lockContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create lock file: %v", err)
			}

			// Test the function
			result, err := IsRunnable(mdPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorMessage != "" && !strings.Contains(err.Error(), tt.errorMessage) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorMessage, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %v, got %v", tt.expected, result)
				}
			}
		})
	}
}

func TestIsRunnable_FileErrors(t *testing.T) {
	tests := []struct {
		name      string
		filePath  string
		expectErr bool
	}{
		{
			name:      "nonexistent file",
			filePath:  "/nonexistent/path/workflow.md",
			expectErr: true,
		},
		{
			name:      "empty file path",
			filePath:  "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := IsRunnable(tt.filePath)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				// Result should be false when there's an error
				if result {
					t.Errorf("Expected false result on error, got true")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}
