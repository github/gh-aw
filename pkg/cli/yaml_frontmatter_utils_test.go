//go:build !integration

package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontmatterLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		content       string
		expectedLines []string
		expectedMd    string
		shouldErr     bool
	}{
		{
			name: "valid frontmatter",
			content: `---
on: workflow_dispatch
permissions:
  contents: read
---

# Test Workflow

Content here.`,
			expectedLines: []string{
				"on: workflow_dispatch",
				"permissions:",
				"  contents: read",
			},
			expectedMd: "# Test Workflow\n\nContent here.",
			shouldErr:  false,
		},
		{
			name:          "no frontmatter",
			content:       "Just markdown content",
			expectedLines: []string{},
			expectedMd:    "Just markdown content",
			shouldErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, md, err := parseFrontmatterLines(tt.content)

			if tt.shouldErr {
				assert.Error(t, err, "Expected error parsing invalid content")
				return
			}

			require.NoError(t, err, "Should parse valid frontmatter")
			assert.Equal(t, tt.expectedLines, lines, "Frontmatter lines should match")
			assert.Equal(t, tt.expectedMd, md, "Markdown content should match")
		})
	}
}

func TestGetIndentation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		expected string
	}{
		{
			name:     "no indentation",
			line:     "key: value",
			expected: "",
		},
		{
			name:     "two space indentation",
			line:     "  key: value",
			expected: "  ",
		},
		{
			name:     "four space indentation",
			line:     "    key: value",
			expected: "    ",
		},
		{
			name:     "tab indentation",
			line:     "\tkey: value",
			expected: "\t",
		},
		{
			name:     "mixed indentation",
			line:     "  \t  key: value",
			expected: "  \t  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIndentation(tt.line)
			assert.Equal(t, tt.expected, result, "Indentation should match")
		})
	}
}

func TestIsTopLevelKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "top level key",
			line:     "on: workflow_dispatch",
			expected: true,
		},
		{
			name:     "indented key",
			line:     "  contents: read",
			expected: false,
		},
		{
			name:     "comment",
			line:     "# This is a comment",
			expected: false,
		},
		{
			name:     "empty line",
			line:     "",
			expected: false,
		},
		{
			name:     "no colon",
			line:     "just text",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTopLevelKey(tt.line)
			assert.Equal(t, tt.expected, result, "Top-level key detection should match")
		})
	}
}

func TestIsNestedUnder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		currentLine  string
		parentIndent string
		expected     bool
	}{
		{
			name:         "nested under parent",
			currentLine:  "    key: value",
			parentIndent: "  ",
			expected:     true,
		},
		{
			name:         "same level as parent",
			currentLine:  "  key: value",
			parentIndent: "  ",
			expected:     false,
		},
		{
			name:         "less indentation than parent",
			currentLine:  "key: value",
			parentIndent: "  ",
			expected:     false,
		},
		{
			name:         "deeply nested",
			currentLine:  "      key: value",
			parentIndent: "  ",
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNestedUnder(tt.currentLine, tt.parentIndent)
			assert.Equal(t, tt.expected, result, "Nested detection should match")
		})
	}
}

func TestHasExitedBlock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		line        string
		blockIndent string
		expected    bool
	}{
		{
			name:        "exited with top-level key",
			line:        "permissions:",
			blockIndent: "  ",
			expected:    true,
		},
		{
			name:        "still in block - more indentation",
			line:        "    nested: value",
			blockIndent: "  ",
			expected:    false,
		},
		{
			name:        "exited with comment at same level",
			line:        "  # Comment",
			blockIndent: "  ",
			expected:    true,
		},
		{
			name:        "still in block - nested comment",
			line:        "    # Nested comment",
			blockIndent: "  ",
			expected:    false,
		},
		{
			name:        "empty line",
			line:        "",
			blockIndent: "  ",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasExitedBlock(tt.line, tt.blockIndent)
			assert.Equal(t, tt.expected, result, "Block exit detection should match")
		})
	}
}

func TestFindAndReplaceInLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		line        string
		oldKey      string
		newKey      string
		expected    string
		shouldMatch bool
	}{
		{
			name:        "replace simple key",
			line:        "timeout_minutes: 30",
			oldKey:      "timeout_minutes",
			newKey:      "timeout-minutes",
			expected:    "timeout-minutes: 30",
			shouldMatch: true,
		},
		{
			name:        "preserve indentation",
			line:        "  timeout_minutes: 30",
			oldKey:      "timeout_minutes",
			newKey:      "timeout-minutes",
			expected:    "  timeout-minutes: 30",
			shouldMatch: true,
		},
		{
			name:        "preserve inline comment",
			line:        "timeout_minutes: 30  # 30 minutes",
			oldKey:      "timeout_minutes",
			newKey:      "timeout-minutes",
			expected:    "timeout-minutes: 30  # 30 minutes",
			shouldMatch: true,
		},
		{
			name:        "no match - different key",
			line:        "other_key: value",
			oldKey:      "timeout_minutes",
			newKey:      "timeout-minutes",
			expected:    "other_key: value",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, matched := findAndReplaceInLine(tt.line, tt.oldKey, tt.newKey)
			assert.Equal(t, tt.shouldMatch, matched, "Match status should be correct")
			assert.Equal(t, tt.expected, result, "Replaced line should match")
		})
	}
}

func TestRemoveFieldFromBlock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		lines         []string
		fieldName     string
		parentBlock   string
		expectedLines []string
		shouldModify  bool
	}{
		{
			name: "remove simple field",
			lines: []string{
				"network:",
				"  allowed:",
				"    - defaults",
				"  firewall: null",
				"permissions:",
				"  contents: read",
			},
			fieldName:   "firewall",
			parentBlock: "network",
			expectedLines: []string{
				"network:",
				"  allowed:",
				"    - defaults",
				"permissions:",
				"  contents: read",
			},
			shouldModify: true,
		},
		{
			name: "remove field with nested properties",
			lines: []string{
				"network:",
				"  allowed:",
				"    - defaults",
				"  firewall:",
				"    log-level: debug",
				"    version: v1.0.0",
				"permissions:",
				"  contents: read",
			},
			fieldName:   "firewall",
			parentBlock: "network",
			expectedLines: []string{
				"network:",
				"  allowed:",
				"    - defaults",
				"permissions:",
				"  contents: read",
			},
			shouldModify: true,
		},
		{
			name: "remove field with comments",
			lines: []string{
				"network:",
				"  allowed:",
				"    - defaults",
				"  firewall:",
				"    # Firewall config",
				"    log-level: debug",
				"permissions:",
				"  contents: read",
			},
			fieldName:   "firewall",
			parentBlock: "network",
			expectedLines: []string{
				"network:",
				"  allowed:",
				"    - defaults",
				"permissions:",
				"  contents: read",
			},
			shouldModify: true,
		},
		{
			name: "field not present",
			lines: []string{
				"network:",
				"  allowed:",
				"    - defaults",
				"permissions:",
				"  contents: read",
			},
			fieldName:   "firewall",
			parentBlock: "network",
			expectedLines: []string{
				"network:",
				"  allowed:",
				"    - defaults",
				"permissions:",
				"  contents: read",
			},
			shouldModify: false,
		},
		{
			name: "removes empty parent block when last child is removed",
			lines: []string{
				"features:",
				"  mcp-cli: true",
				"permissions:",
				"  contents: read",
			},
			fieldName:   "mcp-cli",
			parentBlock: "features",
			expectedLines: []string{
				"permissions:",
				"  contents: read",
			},
			shouldModify: true,
		},
		{
			name: "keeps parent block when other children remain",
			lines: []string{
				"features:",
				"  mcp-cli: true",
				"  other-flag: true",
				"permissions:",
				"  contents: read",
			},
			fieldName:   "mcp-cli",
			parentBlock: "features",
			expectedLines: []string{
				"features:",
				"  other-flag: true",
				"permissions:",
				"  contents: read",
			},
			shouldModify: true,
		},
		{
			name: "removes empty parent block when it is the last block",
			lines: []string{
				"engine: copilot",
				"features:",
				"  mcp-cli: true",
			},
			fieldName:   "mcp-cli",
			parentBlock: "features",
			expectedLines: []string{
				"engine: copilot",
			},
			shouldModify: true,
		},
		{
			name: "keeps parent block header when only a comment remains under it",
			lines: []string{
				"features:",
				"  # user-authored comment",
				"  mcp-cli: true",
				"permissions:",
				"  contents: read",
			},
			fieldName:   "mcp-cli",
			parentBlock: "features",
			expectedLines: []string{
				"features:",
				"  # user-authored comment",
				"permissions:",
				"  contents: read",
			},
			shouldModify: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, modified := removeFieldFromBlock(tt.lines, tt.fieldName, tt.parentBlock)
			assert.Equal(t, tt.shouldModify, modified, "Modification status should match")

			if tt.shouldModify || !modified {
				// Compare line by line for better error messages
				assert.Len(t, result, len(tt.expectedLines), "Number of lines should match")
				for i := range tt.expectedLines {
					if i < len(result) {
						assert.Equal(t, tt.expectedLines[i], result[i], "Line %d should match", i)
					}
				}
			}
		})
	}
}

func TestRemoveFieldFromBlock_PreservesComments(t *testing.T) {
	t.Parallel()
	lines := []string{
		"network:",
		"  # Network configuration",
		"  allowed:",
		"    - defaults",
		"  firewall:",
		"    # This comment should be removed",
		"    log-level: debug",
		"  # This comment should be preserved",
		"permissions:",
		"  contents: read",
	}

	result, modified := removeFieldFromBlock(lines, "firewall", "network")

	require.True(t, modified, "Should modify the lines")

	// Check that the firewall block was removed
	for _, line := range result {
		assert.NotContains(t, line, "firewall:", "Firewall line should be removed")
		assert.NotContains(t, line, "log-level:", "Nested firewall properties should be removed")
		assert.NotContains(t, line, "This comment should be removed", "Nested comments should be removed")
	}

	// Check that other content is preserved
	assert.Contains(t, strings.Join(result, "\n"), "# Network configuration", "Block comment should be preserved")
	assert.Contains(t, strings.Join(result, "\n"), "# This comment should be preserved", "Comment after firewall should be preserved")
	assert.Contains(t, strings.Join(result, "\n"), "allowed:", "Other network fields should be preserved")
	assert.Contains(t, strings.Join(result, "\n"), "permissions:", "Other top-level fields should be preserved")
}

func TestApplyFrontmatterLineTransform(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		content     string
		transform   func([]string) ([]string, bool)
		wantApplied bool
		wantContent string
		wantErr     bool
	}{
		{
			name: "transform applied",
			content: `---
on: workflow_dispatch
timeout_minutes: 30
---

# Test`,
			transform: func(lines []string) ([]string, bool) {
				result := make([]string, len(lines))
				modified := false
				for i, line := range lines {
					if strings.Contains(line, "timeout_minutes") {
						result[i] = strings.ReplaceAll(line, "timeout_minutes", "timeout-minutes")
						modified = true
					} else {
						result[i] = line
					}
				}
				return result, modified
			},
			wantApplied: true,
			wantContent: "timeout-minutes: 30",
		},
		{
			name: "no change returns original content",
			content: `---
on: workflow_dispatch
---

# Test`,
			transform: func(lines []string) ([]string, bool) {
				return lines, false
			},
			wantApplied: false,
		},
		{
			name:    "content with no frontmatter is handled gracefully",
			content: "not valid frontmatter at all",
			transform: func(lines []string) ([]string, bool) {
				// transform returns false because there are no lines to modify
				return lines, false
			},
			wantApplied: false,
		},
		{
			name: "markdown body preserved",
			content: `---
on: workflow_dispatch
---

# My Workflow

Some description here.`,
			transform: func(lines []string) ([]string, bool) {
				result := make([]string, len(lines))
				copy(result, lines)
				result[0] = "on: push"
				return result, true
			},
			wantApplied: true,
			wantContent: "# My Workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, applied, err := applyFrontmatterLineTransform(tt.content, tt.transform)

			if tt.wantErr {
				assert.Error(t, err, "Expected error")
				return
			}

			require.NoError(t, err, "Should not return an error")
			assert.Equal(t, tt.wantApplied, applied, "Applied status should match")

			if tt.wantApplied {
				assert.Contains(t, result, tt.wantContent, "Transformed content should be present")
			} else {
				assert.Equal(t, tt.content, result, "Original content should be returned when not applied")
			}
		})
	}
}
