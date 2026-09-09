//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAuthPluginSHA is a stand-in commit SHA used to pin private plugin references
// in these integration tests, mirroring the constant used in plugins_test.go.
const testAuthPluginSHA = "1f181b37d3fe5862ab590648f25a292e345b5de6"

// TestPrivatePluginAuthAcrossEngines is a full-compile integration test verifying that a
// per-plugin github-token (or github-app) credential is injected into that plugin's
// checkout step for every agentic engine that supports Agent Plugins: Copilot, Claude,
// Codex, and a custom behavior-defined engine (mirroring the shared cursor/kiro engines).
// Because plugin auth is applied to the checkout step ahead of any engine-specific install
// command, the same wiring must work identically across all engines.
func TestPrivatePluginAuthAcrossEngines(t *testing.T) {
	tests := []struct {
		name       string
		engineYAML string
	}{
		{name: "copilot", engineYAML: "copilot"},
		{name: "claude", engineYAML: "claude"},
		{name: "codex", engineYAML: "codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" engine installs private plugin with github-token", func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "private-plugin-"+tt.name)
			workflowPath := filepath.Join(tmpDir, "workflow.md")
			content := `---
on: workflow_dispatch
engine: ` + tt.engineYAML + `
plugins:
  - plugin: octo-org/private-plugin@` + testAuthPluginSHA + `
    github-token: ${{ secrets.PRIVATE_PLUGIN_TOKEN }}
---

Run the workflow.
`
			require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

			compiler := NewCompiler(WithVersion("dev"))
			require.NoError(t, compiler.CompileWorkflow(workflowPath))

			lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			require.NoError(t, err)
			lockText := string(lockContent)

			assert.Regexp(t, `(?s)name: Checkout agent plugin octo-org/private-plugin.*?token: \$\{\{ secrets\.PRIVATE_PLUGIN_TOKEN \}\}`, lockText)
		})

		t.Run(tt.name+" engine installs private plugin with github-app", func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "private-plugin-app-"+tt.name)
			workflowPath := filepath.Join(tmpDir, "workflow.md")
			content := `---
on: workflow_dispatch
engine: ` + tt.engineYAML + `
plugins:
  - plugin: octo-org/private-plugin@` + testAuthPluginSHA + `
    github-app:
      client-id: ${{ vars.PLUGIN_APP_CLIENT_ID }}
      private-key: ${{ secrets.PLUGIN_APP_PRIVATE_KEY }}
---

Run the workflow.
`
			require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

			compiler := NewCompiler(WithVersion("dev"))
			require.NoError(t, compiler.CompileWorkflow(workflowPath))

			lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			require.NoError(t, err)
			lockText := string(lockContent)

			assert.Contains(t, lockText, "id: plugin-app-token-0")
			assert.Regexp(t, `(?s)name: Checkout agent plugin octo-org/private-plugin.*?token: \$\{\{ steps\.plugin-app-token-0\.outputs\.token \}\}`, lockText)
		})
	}

	t.Run("custom behavior-defined engine installs private plugin with github-token", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "private-plugin-custom-engine")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		sharedDir := filepath.Join(workflowsDir, "shared")
		require.NoError(t, os.MkdirAll(sharedDir, 0o755))

		sharedContent := `---
engine:
  id: custom-plugin-engine
  display-name: Custom Plugin Engine
  behaviors:
    plugins:
      directory: .custom/plugins
    execution:
      command-name: custom-cli
      step-name: Execute Custom Plugin Engine
---

# Shared custom plugin engine definition
`
		require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "custom-plugin-engine.md"), []byte(sharedContent), 0o644))

		workflowPath := filepath.Join(workflowsDir, "workflow.md")
		content := `---
on: workflow_dispatch
engine:
  id: custom-plugin-engine
imports:
  - shared/custom-plugin-engine.md
plugins:
  - plugin: octo-org/private-plugin@` + testAuthPluginSHA + `
    github-token: ${{ secrets.PRIVATE_PLUGIN_TOKEN }}
---

Run the workflow.
`
		require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))

		compiler := NewCompiler(WithVersion("dev"))
		require.NoError(t, compiler.CompileWorkflow(workflowPath))

		lockContent, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
		require.NoError(t, err)
		lockText := string(lockContent)

		assert.Regexp(t, `(?s)name: Checkout agent plugin octo-org/private-plugin.*?token: \$\{\{ secrets\.PRIVATE_PLUGIN_TOKEN \}\}`, lockText)
		assert.Contains(t, lockText, "name: Stage agent plugin octo-org/private-plugin")
	})
}
