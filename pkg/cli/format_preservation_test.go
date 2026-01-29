//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatPreservation(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		source      string
		shouldErr   bool
		mustContain []string
		description string
	}{
		{
			name: "preserves comments and indentation",
			content: `---
on:
    workflow_dispatch:

    schedule:
        # Daily run at 9 AM UTC on weekdays  
        - cron: "0 9 * * 1-5"

    # Auto-stop workflow after 2 hours
    stop-after: +2h

timeout_minutes: 45

permissions: read-all

network: defaults

engine: claude

tools:
    # Enable web search functionality
    web-search: null
    
    # Memory caching for better performance  
    cache-memory: true

---

# Test Formatting Preservation

This workflow is designed to test whether the formatting is preserved.`,
			source:    "test/repo/workflow.md@v1.0.0",
			shouldErr: false,
			mustContain: []string{
				"# Daily run at 9 AM UTC on weekdays",
				"\n\n",
				"stop-after: +2h",
				"    workflow_dispatch:",
				"# Enable web search functionality",
				"# Memory caching for better performance",
			},
			description: "should preserve YAML comments, blank lines, and indentation",
		},
		{
			name: "preserves inline comments in tools section",
			content: `---
tools:
    # Enable web search functionality
    web-search: null
    
    # Memory caching for better performance  
    cache-memory: true
---

# Test`,
			source:    "test/repo/workflow.md@v1.0.0",
			shouldErr: false,
			mustContain: []string{
				"# Enable web search functionality",
				"# Memory caching for better performance",
			},
			description: "should preserve inline comments in tools section",
		},
		{
			name: "preserves complex nested indentation",
			content: `---
on:
    schedule:
        - cron: "0 9 * * 1-5"
          # Comment on nested item
          enabled: true
---

# Test`,
			source:    "test/repo/workflow.md@v1.0.0",
			shouldErr: false,
			mustContain: []string{
				"        - cron:",
				"          # Comment on nested item",
				"          enabled: true",
			},
			description: "should preserve complex nested indentation patterns",
		},
		{
			name: "handles empty frontmatter",
			content: `---
---

# Test`,
			source:    "test/repo/workflow.md@v1.0.0",
			shouldErr: false,
			mustContain: []string{
				"---",
				"# Test",
				"source: test/repo/workflow.md@v1.0.0",
			},
			description: "should handle empty frontmatter without errors",
		},
		{
			name: "handles missing source",
			content: `---
engine: claude
---

# Test`,
			source:    "",
			shouldErr: false,
			mustContain: []string{
				"source: ",
			},
			description: "should handle empty source string",
		},
		{
			name: "handles malformed YAML frontmatter",
			content: `---
invalid: yaml: content: extra: colons
---
# Test`,
			source:      "test/repo/workflow.md@v1.0.0",
			shouldErr:   true,
			mustContain: nil,
			description: "should error when frontmatter contains malformed YAML",
		},
		{
			name: "handles missing closing delimiter",
			content: `---
engine: claude
# Test`,
			source:      "test/repo/workflow.md@v1.0.0",
			shouldErr:   true,
			mustContain: nil,
			description: "should error when closing delimiter is missing",
		},
		{
			name: "preserves unicode in comments",
			content: `---
# 中文注释
engine: claude
---
# Test`,
			source:    "test/repo/workflow.md@v1.0.0",
			shouldErr: false,
			mustContain: []string{
				"# 中文注释",
				"source: test/repo/workflow.md@v1.0.0",
			},
			description: "should preserve unicode characters in comments",
		},
		{
			name: "preserves 2-space indentation",
			content: `---
on:
  workflow_dispatch:
  schedule:
    - cron: "0 9 * * 1-5"
---
# Test`,
			source:    "test/repo/workflow.md@v1.0.0",
			shouldErr: false,
			mustContain: []string{
				"  workflow_dispatch:",
				"  schedule:",
				"    - cron:",
			},
			description: "should preserve 2-space indentation patterns",
		},
		{
			name: "rejects tabs in indentation",
			content: `---
on:
	workflow_dispatch:
	schedule:
		- cron: "0 9 * * 1-5"
---
# Test`,
			source:      "test/repo/workflow.md@v1.0.0",
			shouldErr:   true,
			mustContain: nil,
			description: "should error when YAML contains tabs (YAML spec requires spaces)",
		},
		{
			name: "preserves trailing spaces in comments",
			content: `---
# Comment with trailing spaces  
engine: claude
---
# Test`,
			source:    "test/repo/workflow.md@v1.0.0",
			shouldErr: false,
			mustContain: []string{
				"# Comment with trailing spaces",
			},
			description: "should preserve comments even with trailing spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := addSourceToWorkflow(tt.content, tt.source)

			if tt.shouldErr {
				require.Error(t, err, tt.description)
				return
			}

			require.NoError(t, err, tt.description)

			for _, expected := range tt.mustContain {
				assert.Contains(t, result, expected,
					"%s - expected substring not found: %q", tt.description, expected)
			}
		})
	}
}

func TestFormatPreservationSubtests(t *testing.T) {
	content := `---
on:
    workflow_dispatch:

    schedule:
        # Daily run at 9 AM UTC on weekdays  
        - cron: "0 9 * * 1-5"

    # Auto-stop workflow after 2 hours
    stop-after: +2h

timeout_minutes: 45

permissions: read-all

network: defaults

engine: claude

tools:
    # Enable web search functionality
    web-search: null
    
    # Memory caching for better performance  
    cache-memory: true

---

# Test Formatting Preservation

This workflow is designed to test whether the formatting is preserved.`

	result, err := addSourceToWorkflow(content, "test/repo/workflow.md@v1.0.0")
	require.NoError(t, err, "setup should succeed")

	t.Run("comments preserved", func(t *testing.T) {
		assert.Contains(t, result, "# Daily run at 9 AM UTC on weekdays",
			"YAML comments should be preserved in the output - comment block may have been stripped during processing")
		assert.Contains(t, result, "# Auto-stop workflow after 2 hours",
			"inline comments should be preserved in the output")
		assert.Contains(t, result, "# Enable web search functionality",
			"tool section comments should be preserved in the output")
		assert.Contains(t, result, "# Memory caching for better performance",
			"nested tool comments should be preserved in the output")
	})

	t.Run("whitespace preserved", func(t *testing.T) {
		assert.Contains(t, result, "\n\n",
			"blank lines should be preserved in the output - check YAML parser configuration")
	})

	t.Run("indentation preserved", func(t *testing.T) {
		assert.Contains(t, result, "    workflow_dispatch:",
			"4-space indentation should be preserved - check YAML parser configuration")
		assert.Contains(t, result, "        # Daily run at 9 AM UTC on weekdays",
			"8-space indentation for nested comments should be preserved")
	})

	t.Run("source field added", func(t *testing.T) {
		assert.Contains(t, result, "source: test/repo/workflow.md@v1.0.0",
			"source field should be added to frontmatter")
	})

	t.Run("content structure preserved", func(t *testing.T) {
		assert.Contains(t, result, "# Test Formatting Preservation",
			"markdown heading should be preserved in the output")
		assert.Contains(t, result, "This workflow is designed to test whether the formatting is preserved.",
			"markdown body content should be preserved in the output")
	})
}
