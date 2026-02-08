//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginMCPCompilation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-mcp-test")

	tests := []struct {
		name             string
		workflow         string
		expectedPlugins  []string
		expectedEnvVars  map[string]string
		shouldNotContain []string
	}{
		{
			name: "Plugin with MCP env configuration",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
  contents: read
plugins:
  - github/simple-plugin
  - id: github/mcp-plugin
    mcp:
      env:
        API_KEY: ${{ secrets.API_KEY }}
        API_URL: https://api.example.com
---

Test plugin with MCP configuration
`,
			expectedPlugins: []string{
				"copilot plugin install github/simple-plugin",
				"copilot plugin install github/mcp-plugin",
			},
			expectedEnvVars: map[string]string{
				"API_KEY": "${{ secrets.API_KEY }}",
				"API_URL": "https://api.example.com",
			},
			shouldNotContain: []string{},
		},
		{
			name: "Multiple plugins with different MCP configs",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
  contents: read
plugins:
  - id: github/plugin1
    mcp:
      env:
        PLUGIN1_KEY: ${{ secrets.PLUGIN1_KEY }}
  - id: github/plugin2
    mcp:
      env:
        PLUGIN2_KEY: ${{ secrets.PLUGIN2_KEY }}
        PLUGIN2_URL: https://plugin2.example.com
---

Test multiple plugins with MCP configs
`,
			expectedPlugins: []string{
				"copilot plugin install github/plugin1",
				"copilot plugin install github/plugin2",
			},
			expectedEnvVars: map[string]string{
				"PLUGIN1_KEY": "${{ secrets.PLUGIN1_KEY }}",
				"PLUGIN2_KEY": "${{ secrets.PLUGIN2_KEY }}",
				"PLUGIN2_URL": "https://plugin2.example.com",
			},
			shouldNotContain: []string{},
		},
		{
			name: "Mixed simple and MCP-enabled plugins",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
  contents: read
plugins:
  repos:
    - github/simple1
    - id: github/mcp-enabled
      mcp:
        env:
          MCP_SECRET: ${{ secrets.MCP_SECRET }}
    - github/simple2
  github-token: ${{ secrets.CUSTOM_TOKEN }}
---

Test mixed plugin configuration
`,
			expectedPlugins: []string{
				"copilot plugin install github/simple1",
				"copilot plugin install github/mcp-enabled",
				"copilot plugin install github/simple2",
			},
			expectedEnvVars: map[string]string{
				"MCP_SECRET": "${{ secrets.MCP_SECRET }}",
			},
			shouldNotContain: []string{
				"GH_AW_PLUGINS_TOKEN", // Should use custom token instead
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, "test-plugin-mcp.md")
			err := os.WriteFile(testFile, []byte(tt.workflow), 0644)
			require.NoError(t, err, "Failed to write test file")

			// Compile workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(testFile)
			require.NoError(t, err, "Compilation should succeed")

			// Read generated lock file
			lockFile := strings.Replace(testFile, ".md", ".lock.yml", 1)
			content, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Failed to read lock file")

			lockContent := string(content)

			// Verify all expected plugin install commands are present
			for _, expectedPlugin := range tt.expectedPlugins {
				assert.Contains(t, lockContent, expectedPlugin,
					"Lock file should contain plugin install command: %s", expectedPlugin)
			}

			// Verify MCP gateway step exists
			assert.Contains(t, lockContent, "Start MCP gateway",
				"Lock file should contain MCP gateway step")

			// Extract the MCP gateway step section
			startIdx := strings.Index(lockContent, "Start MCP gateway")
			require.Greater(t, startIdx, 0, "MCP gateway step should exist")

			// Find the end of the MCP gateway step (next step or end of job)
			endIdx := strings.Index(lockContent[startIdx:], "- name:")
			if endIdx == -1 {
				endIdx = len(lockContent)
			} else {
				endIdx = startIdx + endIdx
			}
			mcpGatewaySection := lockContent[startIdx:endIdx]

			// Verify all expected environment variables are in the MCP gateway step
			for envVar, expectedValue := range tt.expectedEnvVars {
				expectedLine := envVar + ": " + expectedValue
				assert.Contains(t, mcpGatewaySection, expectedLine,
					"MCP gateway step should contain environment variable: %s", expectedLine)
			}

			// Verify items that should NOT be present
			for _, shouldNotContainStr := range tt.shouldNotContain {
				assert.NotContains(t, lockContent, shouldNotContainStr,
					"Lock file should not contain: %s", shouldNotContainStr)
			}
		})
	}
}

func TestPluginMCPBackwardCompatibility(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-backward-compat-test")

	// Test that existing plugin formats still work
	workflow := `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
  contents: read
plugins:
  - github/plugin1
  - github/plugin2
---

Test backward compatibility
`

	testFile := filepath.Join(tmpDir, "test-backward-compat.md")
	err := os.WriteFile(testFile, []byte(workflow), 0644)
	require.NoError(t, err, "Failed to write test file")

	// Compile workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Compilation should succeed for backward compatible format")

	// Read generated lock file
	lockFile := strings.Replace(testFile, ".md", ".lock.yml", 1)
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")

	lockContent := string(content)

	// Verify plugins are installed
	assert.Contains(t, lockContent, "copilot plugin install github/plugin1",
		"Should install plugin1")
	assert.Contains(t, lockContent, "copilot plugin install github/plugin2",
		"Should install plugin2")
}
