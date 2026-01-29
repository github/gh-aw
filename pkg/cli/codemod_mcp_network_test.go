//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMCPNetworkMigrationCodemod(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	// Verify codemod metadata
	assert.Equal(t, "mcp-network-to-top-level-migration", codemod.ID)
	assert.Equal(t, "Migrate MCP network config to top-level", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.6.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestMCPNetworkCodemod_NoMCPServers(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestMCPNetworkCodemod_MCPServerWithoutNetwork(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  my-server:
    command: node server.js
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"command": "node server.js",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestMCPNetworkCodemod_SingleServerWithNetwork(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  my-server:
    container: my-image
    network:
      allowed:
        - example.com
        - api.example.com
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
				"network": map[string]any{
					"allowed": []any{"example.com", "api.example.com"},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "    network:", "Should remove per-server network")
	assert.Contains(t, result, "network:", "Should add top-level network")
	assert.Contains(t, result, "  allowed:", "Should add allowed field")
	assert.Contains(t, result, "    - example.com", "Should include domain")
	assert.Contains(t, result, "    - api.example.com", "Should include domain")
	assert.Contains(t, result, "container: my-image", "Should preserve container field")
}

func TestMCPNetworkCodemod_MultipleServersWithSameNetwork(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  server1:
    container: image1
    network:
      allowed:
        - example.com
  server2:
    container: image2
    network:
      allowed:
        - example.com
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"server1": map[string]any{
				"container": "image1",
				"network": map[string]any{
					"allowed": []any{"example.com"},
				},
			},
			"server2": map[string]any{
				"container": "image2",
				"network": map[string]any{
					"allowed": []any{"example.com"},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "      allowed:", "Should remove per-server network")
	assert.Contains(t, result, "network:", "Should add top-level network")
	assert.Contains(t, result, "  allowed:", "Should add allowed field")
	// Should only have one instance of example.com (deduplication)
	occurrences := 0
	for _, line := range splitLines(result) {
		if line == "    - example.com" {
			occurrences++
		}
	}
	assert.Equal(t, 1, occurrences, "Should deduplicate domains")
}

func TestMCPNetworkCodemod_MultipleServersWithDifferentNetworks(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  server1:
    container: image1
    network:
      allowed:
        - example.com
  server2:
    container: image2
    network:
      allowed:
        - api.github.com
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"server1": map[string]any{
				"container": "image1",
				"network": map[string]any{
					"allowed": []any{"example.com"},
				},
			},
			"server2": map[string]any{
				"container": "image2",
				"network": map[string]any{
					"allowed": []any{"api.github.com"},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "network:", "Should add top-level network")
	assert.Contains(t, result, "    - example.com", "Should merge domain from server1")
	assert.Contains(t, result, "    - api.github.com", "Should merge domain from server2")
}

func TestMCPNetworkCodemod_MergeWithExistingTopLevelNetwork(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
network:
  allowed:
    - existing.com
mcp-servers:
  my-server:
    container: my-image
    network:
      allowed:
        - new.com
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"allowed": []any{"existing.com"},
		},
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
				"network": map[string]any{
					"allowed": []any{"new.com"},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "    - existing.com", "Should keep existing domain")
	assert.Contains(t, result, "    - new.com", "Should add new domain")
	assert.NotContains(t, result, "      allowed:", "Should remove per-server network")
}

func TestMCPNetworkCodemod_PreservesOtherMCPFields(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  my-server:
    type: stdio
    container: my-image
    env:
      API_KEY: secret
    network:
      allowed:
        - example.com
    allowed:
      - tool1
      - tool2
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"type":      "stdio",
				"container": "my-image",
				"env": map[string]any{
					"API_KEY": "secret",
				},
				"network": map[string]any{
					"allowed": []any{"example.com"},
				},
				"allowed": []any{"tool1", "tool2"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "type: stdio", "Should preserve type")
	assert.Contains(t, result, "container: my-image", "Should preserve container")
	assert.Contains(t, result, "env:", "Should preserve env")
	assert.Contains(t, result, "API_KEY: secret", "Should preserve env value")
	assert.Contains(t, result, "    allowed:", "Should preserve tool allowed list")
	assert.Contains(t, result, "      - tool1", "Should preserve tool")
	assert.NotContains(t, result, "    network:", "Should remove network block")
	assert.Contains(t, result, "network:", "Should add top-level network")
	assert.Contains(t, result, "    - example.com", "Should add domain to top-level")
}

func TestMCPNetworkCodemod_MixedServersWithAndWithoutNetwork(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  server1:
    container: image1
    network:
      allowed:
        - example.com
  server2:
    command: node server.js
  server3:
    container: image3
    network:
      allowed:
        - api.com
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"server1": map[string]any{
				"container": "image1",
				"network": map[string]any{
					"allowed": []any{"example.com"},
				},
			},
			"server2": map[string]any{
				"command": "node server.js",
			},
			"server3": map[string]any{
				"container": "image3",
				"network": map[string]any{
					"allowed": []any{"api.com"},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "command: node server.js", "Should preserve server2")
	assert.Contains(t, result, "network:", "Should add top-level network")
	assert.Contains(t, result, "    - example.com", "Should include domain from server1")
	assert.Contains(t, result, "    - api.com", "Should include domain from server3")
}

func TestMCPNetworkCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  my-server:
    container: my-image
    network:
      allowed:
        - example.com
---

# Test Workflow

This is a test workflow with:
- Multiple lines
- Markdown formatting

` + "```yaml" + `
key: value
` + "```"

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
				"network": map[string]any{
					"allowed": []any{"example.com"},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "- Multiple lines")
	assert.Contains(t, result, "```yaml")
}

func TestMCPNetworkCodemod_PreservesComments(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
# MCP server configuration
mcp-servers:
  my-server:
    container: my-image
    # Network configuration (deprecated)
    network:
      allowed:
        - example.com
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
				"network": map[string]any{
					"allowed": []any{"example.com"},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# MCP server configuration")
	// Comments at the same level as the removed field are preserved (they're siblings, not children)
	assert.Contains(t, result, "# Network configuration (deprecated)")
}

func TestMCPNetworkCodemod_EmptyNetworkAllowed(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  my-server:
    container: my-image
    network:
      allowed: []
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
				"network": map[string]any{
					"allowed": []any{},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied, "Should not apply when network.allowed is empty")
	_ = result // Unused but needed for test structure
}

func TestMCPNetworkCodemod_NetworkWithoutAllowed(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
mcp-servers:
  my-server:
    container: my-image
    network:
      timeout: 30
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
				"network": map[string]any{
					"timeout": 30,
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied, "Should not apply when network doesn't have allowed field")
	_ = result // Unused but needed for test structure
}

func TestMCPNetworkCodemod_DeduplicatesAcrossServersAndTopLevel(t *testing.T) {
	codemod := getMCPNetworkMigrationCodemod()

	content := `---
on: workflow_dispatch
network:
  allowed:
    - example.com
    - api.com
mcp-servers:
  server1:
    container: image1
    network:
      allowed:
        - example.com
        - new1.com
  server2:
    container: image2
    network:
      allowed:
        - api.com
        - new2.com
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"network": map[string]any{
			"allowed": []any{"example.com", "api.com"},
		},
		"mcp-servers": map[string]any{
			"server1": map[string]any{
				"container": "image1",
				"network": map[string]any{
					"allowed": []any{"example.com", "new1.com"},
				},
			},
			"server2": map[string]any{
				"container": "image2",
				"network": map[string]any{
					"allowed": []any{"api.com", "new2.com"},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)

	// Count occurrences of each domain
	lines := splitLines(result)
	domainCounts := make(map[string]int)
	for _, line := range lines {
		if line == "    - example.com" {
			domainCounts["example.com"]++
		}
		if line == "    - api.com" {
			domainCounts["api.com"]++
		}
		if line == "    - new1.com" {
			domainCounts["new1.com"]++
		}
		if line == "    - new2.com" {
			domainCounts["new2.com"]++
		}
	}

	assert.Equal(t, 1, domainCounts["example.com"], "Should have exactly one example.com")
	assert.Equal(t, 1, domainCounts["api.com"], "Should have exactly one api.com")
	assert.Equal(t, 1, domainCounts["new1.com"], "Should have exactly one new1.com")
	assert.Equal(t, 1, domainCounts["new2.com"], "Should have exactly one new2.com")
}

// Helper function to split content into lines
func splitLines(content string) []string {
	lines := []string{}
	current := ""
	for _, char := range content {
		if char == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(char)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
