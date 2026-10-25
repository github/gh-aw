//go:build !integration

package parser

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractMarkdownSectionReportsScannerError(t *testing.T) {
	content := "# Tools\nPartial section\n" + strings.Repeat("x", 2*bufio.MaxScanTokenSize)
	result, err := ExtractMarkdownSection(content, "Tools")
	require.ErrorContains(t, err, "failed to scan markdown section")
	require.Empty(t, result)
}

func TestExtractFrontmatterFromContent(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantYAML     map[string]any
		wantMarkdown string
		wantErr      bool
	}{
		{
			name: "valid frontmatter and markdown",
			content: `---
title: Test Workflow
on: push
---

# Test Workflow

This is a test workflow.`,
			wantYAML: map[string]any{
				"title": "Test Workflow",
				"on":    "push",
			},
			wantMarkdown: "# Test Workflow\n\nThis is a test workflow.",
		},
		{
			name: "no frontmatter",
			content: `# Test Workflow

This is a test workflow without frontmatter.`,
			wantYAML:     map[string]any{},
			wantMarkdown: "# Test Workflow\n\nThis is a test workflow without frontmatter.",
		},
		{
			name: "empty frontmatter",
			content: `---
---

# Test Workflow

This is a test workflow with empty frontmatter.`,
			wantYAML:     map[string]any{},
			wantMarkdown: "# Test Workflow\n\nThis is a test workflow with empty frontmatter.",
		},
		{
			name:    "unclosed frontmatter",
			content: "---\ntitle: Test\nno closing delimiter",
			wantErr: true,
		},
		{
			name:    "no-break whitespace in values",
			content: "---\ntitle:\u00A0Test\u00A0Workflow\nengine:\u00A0copilot\n---\n\n# Content",
			wantYAML: map[string]any{
				"title":  "Test Workflow",
				"engine": "copilot",
			},
			wantMarkdown: "# Content",
		},
		{
			name:    "frontmatter delimiters with surrounding whitespace",
			content: " \t--- \t\r\non: push\r\n \t--- \t\r\n\r\n# Test Workflow\r\n",
			wantYAML: map[string]any{
				"on": "push",
			},
			wantMarkdown: "# Test Workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractFrontmatterFromContent(tt.content)

			if tt.wantErr {
				require.Error(t, err, "ExtractFrontmatterFromContent() should return an error")
				return
			}

			require.NoError(t, err, "ExtractFrontmatterFromContent() should not return an error")
			assert.Equal(t, tt.wantYAML, result.Frontmatter, "ExtractFrontmatterFromContent() frontmatter should match expected")
			assert.Equal(t, tt.wantMarkdown, result.Markdown, "ExtractFrontmatterFromContent() markdown should match expected")
		})
	}
}

func TestExtractMarkdownSection(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		sectionName string
		expected    string
		wantErr     bool
	}{
		{
			name: "basic H1 section",
			content: `# Introduction

This is the introduction.

# Setup

This is the setup section.

# Configuration

This is the configuration.`,
			sectionName: "Setup",
			expected: `# Setup

This is the setup section.`,
		},
		{
			name: "H2 section",
			content: `# Main Title

## Subsection 1

Content for subsection 1.

## Subsection 2

Content for subsection 2.`,
			sectionName: "Subsection 1",
			expected: `## Subsection 1

Content for subsection 1.`,
		},
		{
			name: "nested sections",
			content: `# Main

## Sub1

Content 1

### Sub1.1

Nested content

## Sub2

Content 2`,
			sectionName: "Sub1",
			expected: `## Sub1

Content 1

### Sub1.1

Nested content`,
		},
		{
			name:        "section not found",
			content:     "# Title\n\nContent",
			sectionName: "NonExistent",
			wantErr:     true,
		},
		{
			name:        "section name with regexp metacharacters",
			content:     "## Setup (v2.0+)\n\nContent",
			sectionName: "Setup (v2.0+)",
			expected:    "## Setup (v2.0+)\n\nContent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractMarkdownSection(tt.content, tt.sectionName)

			if tt.wantErr {
				require.Error(t, err, "ExtractMarkdownSection() should return an error for missing section")
				return
			}

			require.NoError(t, err, "ExtractMarkdownSection() should not return an error")
			assert.Equal(t, tt.expected, result, "ExtractMarkdownSection() should return the expected section content")
		})
	}
}

func TestGenerateDefaultWorkflowName(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected string
	}{
		{
			name:     "simple filename",
			filePath: "test-workflow.md",
			expected: "Test Workflow",
		},
		{
			name:     "multiple hyphens",
			filePath: "my-test-workflow-file.md",
			expected: "My Test Workflow File",
		},
		{
			name:     "full path",
			filePath: "/path/to/my-workflow.md",
			expected: "My Workflow",
		},
		{
			name:     "no extension",
			filePath: "workflow",
			expected: "Workflow",
		},
		{
			name:     "single word",
			filePath: "test.md",
			expected: "Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateDefaultWorkflowName(tt.filePath)
			assert.Equal(t, tt.expected, result, "generateDefaultWorkflowName() should derive name from file path")
		})
	}
}

func TestExtractMarkdownContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
		wantErr  bool
	}{
		{
			name: "with frontmatter",
			content: `---
title: Test
---

# Markdown

This is markdown.`,
			expected: "# Markdown\n\nThis is markdown.",
		},
		{
			name:     "no frontmatter",
			content:  "# Just Markdown\n\nContent here.",
			expected: "# Just Markdown\n\nContent here.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractMarkdownContent(tt.content)

			if tt.wantErr {
				require.Error(t, err, "ExtractMarkdownContent() should return an error")
				return
			}

			require.NoError(t, err, "ExtractMarkdownContent() should not return an error")
			assert.Equal(t, tt.expected, result, "ExtractMarkdownContent() should return only the markdown body")
		})
	}
}

func TestExtractFrontmatterFromContent_FrontmatterLinesAndStart(t *testing.T) {
	tests := []struct {
		name                 string
		content              string
		wantFrontmatterLines []string
		wantFrontmatterStart int
	}{
		{
			name: "no trailing blank frontmatter line without blank before closing delimiter",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
---
# Body
`,
			wantFrontmatterLines: []string{
				"on: workflow_dispatch",
				"permissions:",
				"  contents: read",
			},
			wantFrontmatterStart: 2,
		},
		{
			name: "preserve intentional blank line before closing delimiter",
			content: `---
on: workflow_dispatch
permissions:
  contents: read

---
# Body
`,
			wantFrontmatterLines: []string{
				"on: workflow_dispatch",
				"permissions:",
				"  contents: read",
				"",
			},
			wantFrontmatterStart: 2,
		},
		{
			name: "no frontmatter keeps empty frontmatter metadata",
			content: `# Body without frontmatter
`,
			wantFrontmatterLines: []string{},
			wantFrontmatterStart: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractFrontmatterFromContent(tt.content)
			require.NoError(t, err, "ExtractFrontmatterFromContent() should not return an error")

			assert.Equal(t, tt.wantFrontmatterLines, result.FrontmatterLines, "ExtractFrontmatterFromContent() FrontmatterLines should match expected")
			assert.Equal(t, tt.wantFrontmatterStart, result.FrontmatterStart, "ExtractFrontmatterFromContent() FrontmatterStart should match expected")
		})
	}
}

func TestExtractWorkflowNameFromMarkdownBody(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		virtualPath string
		expected    string
	}{
		{
			name:        "H1 header present",
			body:        "# My Workflow\n\nSome description.",
			virtualPath: "fallback.md",
			expected:    "My Workflow",
		},
		{
			name:        "H1 header with leading/trailing whitespace",
			body:        "#   Trimmed Name   \n\nContent.",
			virtualPath: "fallback.md",
			expected:    "Trimmed Name",
		},
		{
			name:        "no H1 header falls back to filename",
			body:        "## Not an H1\n\nContent.",
			virtualPath: "my-workflow.md",
			expected:    "My Workflow",
		},
		{
			name:        "empty body falls back to filename",
			body:        "",
			virtualPath: "deploy-service.md",
			expected:    "Deploy Service",
		},
		{
			name:        "H1 header after other content",
			body:        "Some intro text.\n\n# Actual Title\n\nMore content.",
			virtualPath: "fallback.md",
			expected:    "Actual Title",
		},
		{
			name:        "full virtual path uses base filename for fallback",
			body:        "No header here.",
			virtualPath: "/workflows/release-deploy.md",
			expected:    "Release Deploy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractWorkflowNameFromMarkdownBody(tt.body, tt.virtualPath)
			require.NoError(t, err, "ExtractWorkflowNameFromMarkdownBody() should not return an error")
			assert.Equal(t, tt.expected, result, "ExtractWorkflowNameFromMarkdownBody() should return the expected workflow name")
		})
	}
}

func TestExtractWorkflowNameFromContent(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		virtualPath string
		expected    string
		wantErr     bool
	}{
		{
			name: "H1 header in markdown body",
			content: `---
on: push
---

# My Workflow

Some description.`,
			virtualPath: "fallback.md",
			expected:    "My Workflow",
		},
		{
			name: "H1 header without frontmatter",
			content: `# Standalone Workflow

Content here.`,
			virtualPath: "fallback.md",
			expected:    "Standalone Workflow",
		},
		{
			name: "no H1 header falls back to filename",
			content: `---
on: push
---

## Not an H1
`,
			virtualPath: "my-workflow.md",
			expected:    "My Workflow",
		},
		{
			name:        "empty content falls back to filename",
			content:     "",
			virtualPath: "deploy-service.md",
			expected:    "Deploy Service",
		},
		{
			name:    "unclosed frontmatter returns error",
			content: "---\ntitle: Test\nno closing delimiter",
			wantErr: true,
		},
		{
			name: "full virtual path uses base filename for fallback",
			content: `---
on: push
---

No H1 here.`,
			virtualPath: "/workflows/release-deploy.md",
			expected:    "Release Deploy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractWorkflowNameFromContent(tt.content, tt.virtualPath)

			if tt.wantErr {
				require.Error(t, err, "ExtractWorkflowNameFromContent() should return an error")
				return
			}

			require.NoError(t, err, "ExtractWorkflowNameFromContent() should not return an error")
			assert.Equal(t, tt.expected, result, "ExtractWorkflowNameFromContent() should return the expected workflow name")
		})
	}
}
