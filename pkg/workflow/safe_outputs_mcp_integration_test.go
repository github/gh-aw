//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestSafeOutputsMCPServerIntegration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "safe-outputs-integration-test")

	// Create a test markdown file with safe-outputs configuration
	testContent := `---
on: push
name: Test Safe Outputs MCP
engine: claude
safe-outputs:
  create-issue:
    max: 3
  missing-tool: {}
---

Test safe outputs workflow with MCP server integration.
`

	testFile := filepath.Join(tmpDir, "test-safe-outputs.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := filepath.Join(tmpDir, "test-safe-outputs.lock.yml")
	yamlContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}
	yamlStr := string(yamlContent)

	// Note: mcp-server.cjs is now copied by actions/setup from safe-outputs-mcp-server.cjs
	// So we don't check for cat command anymore, we just check the MCP config references it

	// Check that safe-outputs configuration file is written
	if !strings.Contains(yamlStr, "cat > /opt/gh-aw/safeoutputs/config.json") {
		t.Error("Expected safe-outputs configuration to be written to config.json file")
	}

	// Check that safeoutputs is included in MCP configuration
	if !strings.Contains(yamlStr, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs in MCP server configuration")
	}

	// Check that the MCP server is configured with HTTP transport (per MCP Gateway spec)
	if !strings.Contains(yamlStr, `"type": "http"`) {
		t.Error("Expected safeoutputs MCP server to be configured with HTTP transport")
	}

	// Check that safe outputs config is written to file, not as environment variable
	if strings.Contains(yamlStr, "GH_AW_SAFE_OUTPUTS_CONFIG:") {
		t.Error("GH_AW_SAFE_OUTPUTS_CONFIG should NOT be in environment variables - config is now in file")
	}

	// Check that config file is created
	if !strings.Contains(yamlStr, "cat > /opt/gh-aw/safeoutputs/config.json") {
		t.Error("Expected config file to be created")
	}

	t.Log("Safe outputs MCP server integration test passed")
}

func TestSafeOutputsMCPServerDisabled(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "safe-outputs-disabled-test")

	// Create a test markdown file without safe-outputs configuration
	testContent := `---
on: push
name: Test Without Safe Outputs
engine: claude
---

Test workflow without safe outputs.
`

	testFile := filepath.Join(tmpDir, "test-no-safe-outputs.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := filepath.Join(tmpDir, "test-no-safe-outputs.lock.yml")
	yamlContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}
	yamlStr := string(yamlContent)

	// Check that safe-outputs MCP server file is NOT written (it's copied by setup.sh instead)
	// The check is now redundant since we removed the cat command entirely

	// Check that safe-outputs configuration file is NOT written
	if strings.Contains(yamlStr, "cat > /opt/gh-aw/safeoutputs/config.json") {
		t.Error("Expected safe-outputs configuration to NOT be written when safe-outputs are disabled")
	}

	// Check that safeoutputs is NOT included in MCP configuration
	if strings.Contains(yamlStr, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs to NOT be in MCP server configuration when disabled")
	}

	t.Log("Safe outputs MCP server disabled test passed")
}

func TestSafeOutputsMCPServerCodex(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "safe-outputs-codex-test")

	// Create a test markdown file with safe-outputs configuration for Codex
	testContent := `---
on: push
name: Test Safe Outputs MCP with Codex
engine: codex
safe-outputs:
  create-issue: {}
  missing-tool: {}
---

Test safe outputs workflow with Codex engine.
`

	testFile := filepath.Join(tmpDir, "test-safe-outputs-codex.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read the generated .lock.yml file
	lockFile := filepath.Join(tmpDir, "test-safe-outputs-codex.lock.yml")
	yamlContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}
	yamlStr := string(yamlContent)

	// Note: mcp-server.cjs is now copied by actions/setup from safe-outputs-mcp-server.cjs
	// So we don't check for cat command anymore

	// Check that safe-outputs configuration file is written
	if !strings.Contains(yamlStr, "cat > /opt/gh-aw/safeoutputs/config.json") {
		t.Error("Expected safe-outputs configuration to be written to config.json file")
	}

	// Check that safeoutputs is included in TOML configuration for Codex
	if !strings.Contains(yamlStr, "[mcp_servers.safeoutputs]") {
		t.Error("Expected safeoutputs in Codex MCP server TOML configuration")
	}

	// Check that the MCP server is configured with HTTP transport (per MCP Gateway spec)
	if !strings.Contains(yamlStr, `type = "http"`) {
		t.Error("Expected safeoutputs MCP server to be configured with HTTP transport in TOML")
	}

	t.Log("Safe outputs MCP server Codex integration test passed")
}
