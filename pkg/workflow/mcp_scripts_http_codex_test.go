//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestCodexMCPScriptsHTTPTransport verifies that Codex engine uses HTTP transport for mcp-scripts
// (not stdio transport) to be consistent with Copilot and Claude engines
func TestCodexMCPScriptsHTTPTransport(t *testing.T) {
	// Create a temporary workflow file
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "test-workflow.md")

	workflowContent := `---
on: workflow_dispatch
engine: codex
mcp-scripts:
  test-tool:
    description: Test tool
    script: |
      return { result: "test" };
---

Test mcp-scripts HTTP transport for Codex
`

	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(lockContent)

	// Verify that the HTTP server configuration steps are generated
	expectedSteps := []string{
		"Generate MCP Scripts Server Config",
		"Start MCP Scripts HTTP Server",
		"Start MCP Gateway",
	}

	for _, stepName := range expectedSteps {
		if !strings.Contains(yamlStr, stepName) {
			t.Errorf("Expected step not found in workflow: %q", stepName)
		}
	}

	// Verify HTTP transport in TOML config (not stdio)
	if !strings.Contains(yamlStr, `"mcpscripts":`) {
		t.Error("MCP Scripts server config section not found")
	}

	// The gateway receives upstream transport details; Codex only receives its
	// authenticated gateway connection after conversion.
	nativeConfig := decodeCodexBootstrap(t, yamlStr)
	nativeServers := nativeConfig["mcp_servers"].(map[string]any)
	if _, ok := nativeServers["mcpscripts"]; !ok {
		t.Fatal("Expected native Codex client settings for mcpscripts")
	}
	codexConfigSection := extractCodexConfigSection(t, yamlStr)
	if !strings.Contains(yamlStr, `"type": "http"`) {
		t.Error("Expected HTTP transport in the gateway configuration")
	}

	// Should use HTTP transport (url + headers) with host.docker.internal
	if !strings.Contains(yamlStr, `"url": "http://host.docker.internal:$GH_AW_MCP_SCRIPTS_PORT"`) {
		t.Error("Expected upstream MCP Scripts URL in the gateway configuration")
	}

	if !strings.Contains(yamlStr, `"Authorization": "\${GH_AW_MCP_SCRIPTS_API_KEY}"`) {
		t.Error("Expected upstream authentication header in the gateway configuration")
	}

	// Should NOT use stdio transport (command + args to node)
	if strings.Contains(codexConfigSection, `command = "node"`) {
		t.Error("Codex config should not use stdio transport (command = 'node'), should use HTTP")
	}

	if strings.Contains(codexConfigSection, `args = [`) && strings.Contains(codexConfigSection, `${RUNNER_TEMP}/gh-aw/mcp-scripts/mcp-server.cjs`) {
		t.Error("Codex config should not use stdio transport with mcp-server.cjs args, should use HTTP")
	}

	// Verify environment variables are NOT in the MCP config (env_vars not supported for HTTP transport)
	// They should be in the job's env section instead
	if strings.Contains(codexConfigSection, "env_vars") {
		t.Error("HTTP MCP servers should not have env_vars in config (not supported for HTTP transport)")
	}

	t.Logf("✓ Codex engine correctly uses HTTP transport for mcp-scripts")
}

// extractCodexConfigSection extracts the Codex MCP config section from the workflow YAML
func extractCodexConfigSection(t *testing.T, yamlContent string) string {
	t.Helper()
	config := decodeCodexBootstrap(t, yamlContent)
	servers := config["mcp_servers"].(map[string]any)
	data, err := json.Marshal(servers["mcpscripts"])
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestCodexMCPScriptsWithSecretsHTTPTransport verifies that environment variables
// from mcp-scripts tools are properly passed through with HTTP transport
func TestCodexMCPScriptsWithSecretsHTTPTransport(t *testing.T) {
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "test-workflow.md")

	workflowContent := `---
on: workflow_dispatch
engine: codex
mcp-scripts:
  api-call:
    description: Call an API
    env:
      API_KEY: ${{ secrets.API_KEY }}
      GH_TOKEN: ${{ github.token }}
    script: |
      return { result: "test" };
---

Test mcp-scripts with secrets
`

	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(lockContent)
	codexConfigSection := extractCodexConfigSection(t, yamlStr)

	// Verify tool-specific env vars are NOT in the MCP config (env_vars not supported for HTTP)
	// They should be passed via the job's env section instead
	if strings.Contains(codexConfigSection, "env_vars") {
		t.Error("HTTP MCP servers should not have env_vars in config (not supported for HTTP transport)")
	}

	// Verify env vars are set in Start MCP Gateway step (this is the correct location for HTTP transport)
	if !strings.Contains(yamlStr, "API_KEY: ${{ secrets.API_KEY }}") {
		t.Error("Expected API_KEY secret in Start MCP Gateway env section")
	}

	if !strings.Contains(yamlStr, "GH_TOKEN: ${{ github.token }}") {
		t.Error("Expected GH_TOKEN in Start MCP Gateway env section")
	}

	t.Logf("✓ Codex engine correctly passes secrets through HTTP transport (via job env, not MCP config)")
}
