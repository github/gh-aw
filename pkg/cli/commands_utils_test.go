//go:build !integration

package cli

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestExtractWorkflowNameFromFile(t *testing.T) {
	t.Parallel()
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
			name: "file with whitespace-padded frontmatter delimiter and indented H1",
			content: `  ---
title: Test Workflow
---   

   # Deliberately Indented Workflow Name   
`,
			filename:    "indented-workflow.md",
			expected:    "Deliberately Indented Workflow Name",
			expectError: false,
		},
		{
			name: "file without H1 header - uses H2 header",
			content: `This is content without H1 header.

## Some H2 header

Content here.`,
			filename:    "daily-dependency-updates.md",
			expected:    "Some H2 header",
			expectError: false,
		},
		{
			name: "file without H1/H2 header - uses H3 header",
			content: `This is content without H1 and H2 headers.

### Some H3 header

Content here.`,
			filename:    "daily-dependency-updates.md",
			expected:    "Some H3 header",
			expectError: false,
		},
		{
			name: "file without H1/H2/H3 header - generates from filename",
			content: `This is content without supported headers.

#### Some H4 header

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
	t.Parallel()
	_, err := extractWorkflowNameFromFile("/nonexistent/file.md")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestExtractWorkflowNameFromFile_LargeFrontmatterLine(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-*")
	filePath := filepath.Join(tmpDir, "large-frontmatter.md")
	content := "---\nblob: " + strings.Repeat("x", bufio.MaxScanTokenSize+1) + "\n---\n\n# Large Frontmatter Workflow\n"

	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	result, err := extractWorkflowNameFromFile(filePath)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result != "Large Frontmatter Workflow" {
		t.Fatalf("Expected %q, got %q", "Large Frontmatter Workflow", result)
	}
}

func TestFastParseTitleFromReader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		expected    string
		expectError bool
	}{
		{
			name:     "first supported heading wins",
			content:  "## First heading\n# Later heading\n",
			expected: "First heading",
		},
		{
			name:     "frontmatter headings are ignored",
			content:  "---\n# Not a title\n---\n### Actual title",
			expected: "Actual title",
		},
		{
			name:     "unicode whitespace and CRLF",
			content:  "\u2003---\u2003\r\nkey: value\r\n---\r\n\u2003# Title\u2003\r\n",
			expected: "Title",
		},
		{
			name:     "empty heading stops parsing",
			content:  "# \n# Later heading\n",
			expected: "",
		},
		{
			name:     "frontmatter only opens on the first line",
			content:  "\n---\n# Title\n",
			expected: "Title",
		},
		{
			name:        "unclosed frontmatter",
			content:     "---\n# Not a title\n",
			expectError: true,
		},
		{
			name:        "oversized line",
			content:     strings.Repeat("x", 1024*1024) + "\n# Title\n",
			expectError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, err := fastParseTitleFromReader(strings.NewReader(tt.content))
			if (err != nil) != tt.expectError {
				t.Fatalf("Expected error=%v, got %v", tt.expectError, err)
			}
			if title != tt.expected {
				t.Errorf("Expected title %q, got %q", tt.expected, title)
			}
		})
	}
}

func TestFastParseTitleFromReader_ReadError(t *testing.T) {
	t.Parallel()
	readErr := errors.New("unexpected read")
	for _, tt := range []struct {
		name     string
		content  string
		expected string
	}{
		{name: "before title"},
		{name: "after title", content: "# Title\n", expected: "Title"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := io.MultiReader(strings.NewReader(tt.content), iotest.ErrReader(readErr))
			title, err := fastParseTitleFromReader(reader)
			if tt.expected == "" && !errors.Is(err, readErr) {
				t.Fatalf("Expected read error, got %v", err)
			}
			if tt.expected != "" && err != nil {
				t.Fatalf("Parser read past the title: %v", err)
			}
			if title != tt.expected {
				t.Errorf("Expected title %q, got %q", tt.expected, title)
			}
		})
	}
}

func TestIsGitRepo(t *testing.T) {
	t.Parallel()
	// Test in current directory (should be a git repo based on project setup)
	result := isGitRepo()

	// Since we're running in a git repository, this should return true
	if !result {
		t.Error("Expected isGitRepo() to return true in git repository")
	}
}

// TestFindGitRoot is already tested in gitroot_test.go, skipping duplicate

func TestExtractWorkflowNameFromPath(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
		// @import (deprecated synonym for @include)
		{
			name:     "@import basic",
			content:  "@import shared/tools.md",
			expected: []string{"shared/tools.md"},
		},
		{
			name:     "@import optional marker",
			content:  "@import? shared/tools.md",
			expected: []string{"shared/tools.md"},
		},
		{
			name:     "@import with section reference",
			content:  "@import shared/tools.md#Section",
			expected: []string{"shared/tools.md"},
		},
		// {{#import}} deprecated syntax
		{
			name:     "{{#import}} with colon",
			content:  "{{#import: shared/tools.md}}",
			expected: []string{"shared/tools.md"},
		},
		{
			name:     "{{#import}} without colon",
			content:  "{{#import shared/tools.md}}",
			expected: []string{"shared/tools.md"},
		},
		{
			name:     "{{#import}} optional marker with colon",
			content:  "{{#import?: shared/tools.md}}",
			expected: []string{"shared/tools.md"},
		},
		{
			name:     "{{#import}} with section reference",
			content:  "{{#import: shared/tools.md#Section}}",
			expected: []string{"shared/tools.md"},
		},
		{
			name:     "{{#import}} with trailing content is not a directive",
			content:  "{{#import shared/tools.md}} trailing",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := findIncludesInContent(tt.content)
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

func TestFindIncludesInContent_EmptyContentReturnsNonNilSlice(t *testing.T) {
	t.Parallel()
	result, err := findIncludesInContent("")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil empty slice for empty content")
	}
	if len(result) != 0 {
		t.Fatalf("Expected empty slice, got %v", result)
	}
}

// Benchmark tests
// Measure parsing separately from file I/O to distinguish parser regressions
// from filesystem or runner overhead in BenchmarkExtractWorkflowNameFromFile.
func BenchmarkFastParseTitleFromReader(b *testing.B) {
	const content = "---\ntitle: Test Workflow\n---\n\n# Daily Test Coverage Improvement\n\nThis is a test workflow with some content."
	reader := strings.NewReader(content)
	b.ReportAllocs()
	for b.Loop() {
		reader.Reset(content)
		title, err := fastParseTitleFromReader(reader)
		if err != nil || title != "Daily Test Coverage Improvement" {
			b.Fatalf("Unexpected title %q or error: %v", title, err)
		}
	}
}

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

	for b.Loop() {
		title, err := extractWorkflowNameFromFile(filePath)
		if err != nil || title != "Daily Test Coverage Improvement" {
			b.Fatalf("Unexpected title %q or error: %v", title, err)
		}
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

	for b.Loop() {
		_, _ = findIncludesInContent(content)
	}
}

func TestIsRunnable(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
