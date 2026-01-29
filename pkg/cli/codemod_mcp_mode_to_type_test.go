//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPModeToTypeCodemod(t *testing.T) {
	codemod := getMCPModeToTypeCodemod()

	t.Run("renames mode to type in custom MCP servers", func(t *testing.T) {
		content := `---
engine: copilot
tools:
  github: null
mcp-servers:
  my-custom-server:
    mode: stdio
    command: npx
    args: ["-y", "@my/server"]
---

# Test Workflow
`

		frontmatter := map[string]any{
			"engine": "copilot",
			"tools": map[string]any{
				"github": nil,
			},
			"mcp-servers": map[string]any{
				"my-custom-server": map[string]any{
					"mode":    "stdio",
					"command": "npx",
					"args":    []any{"-y", "@my/server"},
				},
			},
		}

		result, modified, err := codemod.Apply(content, frontmatter)
		require.NoError(t, err, "Should not error when applying codemod")
		assert.True(t, modified, "Should modify content")
		assert.Contains(t, result, "type: stdio", "Should rename mode to type")
		assert.NotContains(t, result, "mode: stdio", "Should not contain old mode field")
	})

	t.Run("does not modify workflows without mcp-servers", func(t *testing.T) {
		content := `---
engine: copilot
tools:
  github:
    mode: remote
---

# Test Workflow
`

		frontmatter := map[string]any{
			"engine": "copilot",
			"tools": map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
		}

		result, modified, err := codemod.Apply(content, frontmatter)
		require.NoError(t, err, "Should not error")
		assert.False(t, modified, "Should not modify content without mcp-servers")
		assert.Equal(t, content, result, "Content should remain unchanged")
	})

	t.Run("does not modify GitHub tool mode field", func(t *testing.T) {
		content := `---
engine: copilot
tools:
  github:
    mode: remote
mcp-servers:
  my-server:
    mode: stdio
    command: node
---

# Test Workflow
`

		frontmatter := map[string]any{
			"engine": "copilot",
			"tools": map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			"mcp-servers": map[string]any{
				"my-server": map[string]any{
					"mode":    "stdio",
					"command": "node",
				},
			},
		}

		result, modified, err := codemod.Apply(content, frontmatter)
		require.NoError(t, err, "Should not error")
		assert.True(t, modified, "Should modify mcp-servers")
		assert.Contains(t, result, "mode: remote", "Should keep GitHub tool mode field")
		assert.Contains(t, result, "type: stdio", "Should rename mode in mcp-servers to type")
		assert.NotContains(t, result, "my-server:\n    mode: stdio", "Should not contain mode in mcp-servers")
	})

	t.Run("handles multiple MCP servers with mode", func(t *testing.T) {
		content := `---
engine: copilot
mcp-servers:
  server1:
    mode: stdio
    command: npm
  server2:
    mode: http
    url: http://localhost:8080
---

# Test Workflow
`

		frontmatter := map[string]any{
			"engine": "copilot",
			"mcp-servers": map[string]any{
				"server1": map[string]any{
					"mode":    "stdio",
					"command": "npm",
				},
				"server2": map[string]any{
					"mode": "http",
					"url":  "http://localhost:8080",
				},
			},
		}

		result, modified, err := codemod.Apply(content, frontmatter)
		require.NoError(t, err, "Should not error")
		assert.True(t, modified, "Should modify content")
		assert.Contains(t, result, "type: stdio", "Should rename first server mode")
		assert.Contains(t, result, "type: http", "Should rename second server mode")
		assert.NotContains(t, result, "mode: stdio", "Should not contain mode: stdio")
		assert.NotContains(t, result, "mode: http", "Should not contain mode: http")
	})

	t.Run("does not modify when no mode field exists", func(t *testing.T) {
		content := `---
engine: copilot
mcp-servers:
  my-server:
    type: stdio
    command: node
---

# Test Workflow
`

		frontmatter := map[string]any{
			"engine": "copilot",
			"mcp-servers": map[string]any{
				"my-server": map[string]any{
					"type":    "stdio",
					"command": "node",
				},
			},
		}

		result, modified, err := codemod.Apply(content, frontmatter)
		require.NoError(t, err, "Should not error")
		assert.False(t, modified, "Should not modify content when no mode field")
		assert.Equal(t, content, result, "Content should remain unchanged")
	})

	t.Run("preserves comments and formatting", func(t *testing.T) {
		content := `---
engine: copilot
mcp-servers:
  my-server:
    # MCP connection mode
    mode: stdio  # Use stdio transport
    command: node
---

# Test Workflow
`

		frontmatter := map[string]any{
			"engine": "copilot",
			"mcp-servers": map[string]any{
				"my-server": map[string]any{
					"mode":    "stdio",
					"command": "node",
				},
			},
		}

		result, modified, err := codemod.Apply(content, frontmatter)
		require.NoError(t, err, "Should not error")
		assert.True(t, modified, "Should modify content")
		assert.Contains(t, result, "# MCP connection mode", "Should preserve comment")
		assert.Contains(t, result, "type: stdio  # Use stdio transport", "Should preserve inline comment and formatting")
		assert.NotContains(t, result, "mode: stdio", "Should not contain old field")
	})
}
