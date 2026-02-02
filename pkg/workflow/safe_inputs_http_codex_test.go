//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestCodexSafeInputsHTTPTransport verifies that Codex engine uses HTTP transport for safe-inputs
// (not stdio transport) to be consistent with Copilot and Claude engines
func TestCodexSafeInputsHTTPTransport(t *testing.T) {
	// Create a temporary workflow file
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "test-workflow.md")

	workflowContent := `---
on: workflow_dispatch
engine: codex
safe-inputs:
  test-tool:
    description: Test tool
    script: |
      return { result: "test" };
---

Test safe-inputs HTTP transport for Codex
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
		"Generate Safe Inputs MCP Server Config",
		"Start Safe Inputs MCP HTTP Server",
		"Start MCP gateway",
	}

	for _, stepName := range expectedSteps {
		if !strings.Contains(yamlStr, stepName) {
			t.Errorf("Expected step not found in workflow: %q", stepName)
		}
	}

	// Verify HTTP transport in TOML config (not stdio)
	if !strings.Contains(yamlStr, "[mcp_servers.safeinputs]") {
		t.Error("Safe-inputs MCP server config section not found")
	}

	// Should have explicit type field
	codexConfigSection := extractCodexConfigSection(yamlStr)
	if !strings.Contains(codexConfigSection, `type = "http"`) {
		t.Error("Expected type field set to 'http' in TOML format")
	}

	// Should use HTTP transport (url + headers) with host.docker.internal
	if !strings.Contains(yamlStr, `url = "http://host.docker.internal:$GH_AW_SAFE_INPUTS_PORT"`) {
		t.Error("Expected HTTP URL config with host.docker.internal not found in TOML format")
	}

	if !strings.Contains(yamlStr, `headers = { Authorization = "$GH_AW_SAFE_INPUTS_API_KEY" }`) {
		t.Error("Expected HTTP headers config not found in TOML format")
	}

	// Should NOT use stdio transport (command + args to node)
	if strings.Contains(codexConfigSection, `command = "node"`) {
		t.Error("Codex config should not use stdio transport (command = 'node'), should use HTTP")
	}

	if strings.Contains(codexConfigSection, `args = [`) && strings.Contains(codexConfigSection, `/opt/gh-aw/safe-inputs/mcp-server.cjs`) {
		t.Error("Codex config should not use stdio transport with mcp-server.cjs args, should use HTTP")
	}

	// Verify environment variables are NOT in the MCP config (env_vars not supported for HTTP transport)
	// They should be in the job's env section instead
	if strings.Contains(codexConfigSection, "env_vars") {
		t.Error("HTTP MCP servers should not have env_vars in config (not supported for HTTP transport)")
	}

	t.Logf("✓ Codex engine correctly uses HTTP transport for safe-inputs")
}

// extractCodexConfigSection extracts the Codex MCP config section from the workflow YAML
func extractCodexConfigSection(yamlContent string) string {
	// Find the start of the safeinputs config
	start := strings.Index(yamlContent, "[mcp_servers.safeinputs]")
	if start == -1 {
		return ""
	}

	// Find the end (next section or EOF)
	end := strings.Index(yamlContent[start:], "EOF")
	if end == -1 {
		return yamlContent[start:]
	}

	return yamlContent[start : start+end]
}

// TestCodexSafeInputsWithSecretsHTTPTransport verifies that environment variables
// from safe-inputs tools are properly passed through with HTTP transport
func TestCodexSafeInputsWithSecretsHTTPTransport(t *testing.T) {
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "test-workflow.md")

	workflowContent := `---
on: workflow_dispatch
engine: codex
safe-inputs:
  api-call:
    description: Call an API
    env:
      API_KEY: ${{ secrets.API_KEY }}
      GH_TOKEN: ${{ github.token }}
    script: |
      return { result: "test" };
---

Test safe-inputs with secrets
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
	codexConfigSection := extractCodexConfigSection(yamlStr)

	// Verify tool-specific env vars are NOT in the MCP config (env_vars not supported for HTTP)
	// They should be passed via the job's env section instead
	if strings.Contains(codexConfigSection, "env_vars") {
		t.Error("HTTP MCP servers should not have env_vars in config (not supported for HTTP transport)")
	}

	// Verify env vars are set in Start MCP gateway step (this is the correct location for HTTP transport)
	if !strings.Contains(yamlStr, "API_KEY: ${{ secrets.API_KEY }}") {
		t.Error("Expected API_KEY secret in Start MCP gateway env section")
	}

	if !strings.Contains(yamlStr, "GH_TOKEN: ${{ github.token }}") {
		t.Error("Expected GH_TOKEN in Start MCP gateway env section")
	}

	t.Logf("✓ Codex engine correctly passes secrets through HTTP transport (via job env, not MCP config)")
}
