//go:build !integration

package cli

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashAnonymousRemovalCodemod(t *testing.T) {
	codemod := getBashAnonymousRemovalCodemod()

	tests := []struct {
		name        string
		input       string
		expectApply bool
		expectError bool
	}{
		{
			name: "replaces anonymous bash with bash: true",
			input: `---
name: Test Workflow
tools:
  bash:
  github:
---
# Test workflow`,
			expectApply: true,
		},
		{
			name: "does not modify bash: true",
			input: `---
name: Test Workflow
tools:
  bash: true
  github:
---
# Test workflow`,
			expectApply: false,
		},
		{
			name: "does not modify bash: false",
			input: `---
name: Test Workflow
tools:
  bash: false
  github:
---
# Test workflow`,
			expectApply: false,
		},
		{
			name: "does not modify bash with array",
			input: `---
name: Test Workflow
tools:
  bash: ["echo", "ls"]
  github:
---
# Test workflow`,
			expectApply: false,
		},
		{
			name: "does not modify when bash is not present",
			input: `---
name: Test Workflow
tools:
  github:
---
# Test workflow`,
			expectApply: false,
		},
		{
			name: "does not modify when tools is not present",
			input: `---
name: Test Workflow
---
# Test workflow`,
			expectApply: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse frontmatter to get the map
			result, err := parser.ExtractFrontmatterFromContent(tt.input)
			require.NoError(t, err, "Failed to parse test input frontmatter")

			// Apply the codemod
			output, applied, err := codemod.Apply(tt.input, result.Frontmatter)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectApply, applied, "Applied status mismatch")

			if tt.expectApply {
				// Verify the output contains the replacement
				assert.Contains(t, output, "bash: true", "Output should contain 'bash: true'")
				assert.NotContains(t, output, "bash:\n", "Output should not contain anonymous bash:")
				assert.NotContains(t, output, "bash: \n", "Output should not contain bash with space")

				// Verify the markdown body is preserved
				assert.Contains(t, output, "# Test workflow", "Markdown body should be preserved")
			} else {
				// If not applied, output should be unchanged
				assert.Equal(t, tt.input, output, "Output should be unchanged when not applied")
			}
		})
	}
}

func TestBashAnonymousCodemodWithComments(t *testing.T) {
	codemod := getBashAnonymousRemovalCodemod()

	input := `---
name: Test Workflow
tools:
  # Enable bash
  bash:
  github:
---
# Test workflow`

	result, err := parser.ExtractFrontmatterFromContent(input)
	require.NoError(t, err)

	output, applied, err := codemod.Apply(input, result.Frontmatter)
	require.NoError(t, err)
	assert.True(t, applied, "Should apply when bash: is present")
	assert.Contains(t, output, "bash: true", "Should replace with bash: true")
	assert.Contains(t, output, "# Enable bash", "Should preserve comments")
}

func TestBashAnonymousCodemodPreservesIndentation(t *testing.T) {
	codemod := getBashAnonymousRemovalCodemod()

	input := `---
name: Test Workflow
tools:
  bash:
  github:
    mode: remote
---
# Test workflow`

	result, err := parser.ExtractFrontmatterFromContent(input)
	require.NoError(t, err)

	output, applied, err := codemod.Apply(input, result.Frontmatter)
	require.NoError(t, err)
	assert.True(t, applied, "Should apply")

	// Check indentation is preserved
	lines := strings.Split(output, "\n")
	var foundBash bool
	for _, line := range lines {
		if strings.Contains(line, "bash: true") {
			foundBash = true
			// Should have 2-space indentation
			assert.True(t, strings.HasPrefix(line, "  bash: true"), "Should have proper indentation")
		}
	}
	assert.True(t, foundBash, "Should find bash: true in output")
}

func TestReplaceBashAnonymousWithTrue(t *testing.T) {
	tests := []struct {
		name        string
		lines       []string
		expectLines []string
		modified    bool
	}{
		{
			name: "replaces bash: in tools block",
			lines: []string{
				"name: Test",
				"tools:",
				"  bash:",
				"  github:",
			},
			expectLines: []string{
				"name: Test",
				"tools:",
				"  bash: true",
				"  github:",
			},
			modified: true,
		},
		{
			name: "does not modify outside tools block",
			lines: []string{
				"name: Test",
				"bash:",
				"tools:",
				"  github:",
			},
			expectLines: []string{
				"name: Test",
				"bash:",
				"tools:",
				"  github:",
			},
			modified: false,
		},
		{
			name: "does not modify bash with value",
			lines: []string{
				"name: Test",
				"tools:",
				"  bash: true",
				"  github:",
			},
			expectLines: []string{
				"name: Test",
				"tools:",
				"  bash: true",
				"  github:",
			},
			modified: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, modified := replaceBashAnonymousWithTrue(tt.lines)
			assert.Equal(t, tt.modified, modified, "Modified status mismatch")
			assert.Equal(t, tt.expectLines, result, "Output lines mismatch")
		})
	}
}
