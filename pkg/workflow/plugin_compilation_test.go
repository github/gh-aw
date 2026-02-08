//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginCompilationArrayFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-array-test")

	tests := []struct {
		name                string
		workflow            string
		expectedPlugins     []string
		expectedTokenString string
	}{
		{
			name: "Single plugin with Copilot",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
plugins:
  - github/test-plugin
---

Test single plugin installation
`,
			expectedPlugins:     []string{"copilot plugin install github/test-plugin"},
			expectedTokenString: "secrets.GH_AW_PLUGINS_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN",
		},
		{
			name: "Multiple plugins with Copilot",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
plugins:
  - github/plugin1
  - acme/plugin2
  - org/plugin3
---

Test multiple plugins
`,
			expectedPlugins: []string{
				"copilot plugin install github/plugin1",
				"copilot plugin install acme/plugin2",
				"copilot plugin install org/plugin3",
			},
			expectedTokenString: "secrets.GH_AW_PLUGINS_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN",
		},
		{
			name: "Plugins with Copilot org namespace",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
plugins:
  - openai/codex-plugin
---

Test plugin with org namespace
`,
			expectedPlugins:     []string{"copilot plugin install openai/codex-plugin"},
			expectedTokenString: "secrets.GH_AW_PLUGINS_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, "test-plugin-array.md")
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

			// Verify cascading token is used
			assert.Contains(t, lockContent, tt.expectedTokenString,
				"Lock file should contain cascading token resolution")

			// Verify GITHUB_TOKEN environment variable is set
			assert.Contains(t, lockContent, "GITHUB_TOKEN:",
				"Lock file should set GITHUB_TOKEN environment variable")
		})
	}
}

func TestPluginCompilationObjectFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-object-test")

	tests := []struct {
		name             string
		workflow         string
		expectedPlugins  []string
		expectedToken    string
		shouldNotContain string
	}{
		{
			name: "Object format with custom token",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
plugins:
  repos:
    - github/test-plugin
    - acme/custom-plugin
  github-token: ${{ secrets.CUSTOM_PLUGIN_TOKEN }}
---

Test object format with custom token
`,
			expectedPlugins: []string{
				"copilot plugin install github/test-plugin",
				"copilot plugin install acme/custom-plugin",
			},
			expectedToken:    "GITHUB_TOKEN: ${{ secrets.CUSTOM_PLUGIN_TOKEN }}",
			shouldNotContain: "GH_AW_PLUGINS_TOKEN",
		},
		{
			name: "Object format without custom token",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
plugins:
  repos:
    - github/plugin1
---

Test object format without custom token
`,
			expectedPlugins: []string{
				"copilot plugin install github/plugin1",
			},
			expectedToken:    "secrets.GH_AW_PLUGINS_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN",
			shouldNotContain: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, "test-plugin-object.md")
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

			// Verify expected token format is used
			assert.Contains(t, lockContent, tt.expectedToken,
				"Lock file should contain expected token: %s", tt.expectedToken)

			// Verify cascading token is NOT used when custom token is provided
			if tt.shouldNotContain != "" {
				assert.NotContains(t, lockContent, tt.shouldNotContain,
					"Lock file should not contain: %s", tt.shouldNotContain)
			}
		})
	}
}

func TestPluginCompilationWithTopLevelGitHubToken(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-toplevel-token-test")

	workflow := `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
github-token: ${{ secrets.MY_GITHUB_TOKEN }}
plugins:
  - github/test-plugin
---

Test with top-level github-token
`

	testFile := filepath.Join(tmpDir, "test-toplevel-token.md")
	err := os.WriteFile(testFile, []byte(workflow), 0644)
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

	// Verify plugin install command is present
	assert.Contains(t, lockContent, "copilot plugin install github/test-plugin",
		"Lock file should contain plugin install command")

	// Verify top-level github-token is used (not cascading)
	assert.Contains(t, lockContent, "GITHUB_TOKEN: ${{ secrets.MY_GITHUB_TOKEN }}",
		"Lock file should use top-level github-token")

	// Verify cascading token is NOT used
	assert.NotContains(t, lockContent, "GH_AW_PLUGINS_TOKEN",
		"Lock file should not use cascading token when top-level token is provided")
}

func TestPluginCompilationTokenPrecedence(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-token-precedence-test")

	tests := []struct {
		name          string
		workflow      string
		expectedToken string
		description   string
	}{
		{
			name: "Object github-token overrides top-level",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
github-token: ${{ secrets.TOPLEVEL_TOKEN }}
plugins:
  repos:
    - github/plugin1
  github-token: ${{ secrets.PLUGINS_SPECIFIC_TOKEN }}
---

Test token precedence
`,
			expectedToken: "GITHUB_TOKEN: ${{ secrets.PLUGINS_SPECIFIC_TOKEN }}",
			description:   "plugins.github-token should override top-level github-token",
		},
		{
			name: "Top-level token used when no plugin token",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
github-token: ${{ secrets.TOPLEVEL_TOKEN }}
plugins:
  - github/plugin1
---

Test top-level token
`,
			expectedToken: "GITHUB_TOKEN: ${{ secrets.TOPLEVEL_TOKEN }}",
			description:   "top-level github-token should be used when no plugins.github-token",
		},
		{
			name: "Cascading fallback when no tokens specified",
			workflow: `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
plugins:
  - github/plugin1
---

Test cascading fallback
`,
			expectedToken: "secrets.GH_AW_PLUGINS_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN",
			description:   "cascading token should be used when no custom tokens provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, "test-precedence.md")
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

			// Verify expected token is used
			assert.Contains(t, lockContent, tt.expectedToken,
				tt.description)
		})
	}
}

func TestPluginCompilationAllEngines(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-all-engines-test")

	// Note: Only copilot engine supports plugins, test different plugin formats
	engines := []struct {
		engineID    string
		expectedCmd string
	}{
		{"copilot", "copilot plugin install github/test-plugin"},
	}

	for _, engine := range engines {
		t.Run(engine.engineID, func(t *testing.T) {
			workflow := fmt.Sprintf(`---
engine: %s
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
plugins:
  - github/test-plugin
---

Test %s with plugins
`, engine.engineID, engine.engineID)

			testFile := filepath.Join(tmpDir, fmt.Sprintf("test-%s.md", engine.engineID))
			err := os.WriteFile(testFile, []byte(workflow), 0644)
			require.NoError(t, err, "Failed to write test file")

			// Compile workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(testFile)
			require.NoError(t, err, "Compilation should succeed for %s", engine.engineID)

			// Read generated lock file
			lockFile := strings.Replace(testFile, ".md", ".lock.yml", 1)
			content, err := os.ReadFile(lockFile)
			require.NoError(t, err, "Failed to read lock file")

			lockContent := string(content)

			// Verify engine-specific install command
			assert.Contains(t, lockContent, engine.expectedCmd,
				"Lock file should contain %s install command", engine.engineID)

			// Verify step name is quoted
			assert.Contains(t, lockContent, "'Install plugin: github/test-plugin'",
				"Step name should be quoted")

			// Verify GITHUB_TOKEN is set
			assert.Contains(t, lockContent, "GITHUB_TOKEN:",
				"GITHUB_TOKEN environment variable should be set")
		})
	}
}

func TestPluginCompilationNoPlugins(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-no-plugins-test")

	workflow := `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
---

Test without plugins
`

	testFile := filepath.Join(tmpDir, "test-no-plugins.md")
	err := os.WriteFile(testFile, []byte(workflow), 0644)
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

	// Verify no plugin installation steps are present
	assert.NotContains(t, lockContent, "plugin install",
		"Lock file should not contain plugin installation when no plugins specified")
}

func TestPluginCompilationInvalidRepoFormat(t *testing.T) {
	tmpDir := testutil.TempDir(t, "plugin-invalid-test")

	// Test invalid repo format (missing slash)
	workflow := `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
plugins:
  - invalid-repo-format
---

Test invalid repo
`

	testFile := filepath.Join(tmpDir, "test-invalid.md")
	err := os.WriteFile(testFile, []byte(workflow), 0644)
	require.NoError(t, err, "Failed to write test file")

	// Compile workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)

	// Should fail due to schema validation
	assert.Error(t, err, "Compilation should fail with invalid repo format")
	if err != nil {
		assert.Contains(t, err.Error(), "pattern",
			"Error should mention pattern validation failure")
	}
}
