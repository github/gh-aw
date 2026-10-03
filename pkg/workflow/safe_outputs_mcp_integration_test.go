//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
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

	// Check that safe-outputs configuration file generation step is present
	if !strings.Contains(yamlStr, "Generate Safe Outputs Config") {
		t.Error("Expected safe-outputs config generation step")
	}
	if !strings.Contains(yamlStr, `\"path\":\"safeoutputs/config.json\"`) {
		t.Error("Expected safe-outputs config.json path in generated file config")
	}
	if !strings.Contains(yamlStr, `\"content_env\":\"GH_AW_SAFE_OUTPUTS_CONFIG\"`) {
		t.Error("Expected safe-outputs config content env mapping")
	}

	// Check that safeoutputs is included in MCP configuration
	if !strings.Contains(yamlStr, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs in MCP server configuration")
	}

	// Check that the MCP server is configured as a containerized stdio MCP server
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)
	if !strings.Contains(yamlStr, `"container": "`+pinnedGhAwNodeImage+`"`) {
		t.Error("Expected safeoutputs MCP server to run in the gh-aw node container")
	}
	if !strings.Contains(yamlStr, `"mounts": ["\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw", "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "/tmp/gh-aw:/tmp/gh-aw:rw"]`) {
		t.Error("Expected safeoutputs MCP server mounts for workspace, runtime files, and logs")
	}
	if !strings.Contains(yamlStr, `"entrypoint": "sh"`) {
		t.Error("Expected safeoutputs MCP server to override container entrypoint to sh")
	}
	if !strings.Contains(yamlStr, `"entrypointArgs": ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`) {
		t.Error("Expected safeoutputs MCP server entrypointArgs to run the stdio MCP server script")
	}

	// Check that safe outputs config is passed via step env and written to file by create_files.cjs
	if !strings.Contains(yamlStr, "GH_AW_SAFE_OUTPUTS_CONFIG:") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_CONFIG env var in Generate Safe Outputs Config step")
	}

	if strings.Contains(yamlStr, "Start Safe Outputs MCP HTTP Server") {
		t.Error("Expected safeoutputs MCP server to be listed in MCP servers, not launched in a dedicated step")
	}

	// Check that config file creation mapping is configured
	if !strings.Contains(yamlStr, `\"path\":\"safeoutputs/config.json\"`) {
		t.Error("Expected config file mapping to be created")
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

	// When no safe-outputs are user-configured, the compiler automatically injects a default
	// create-issue safe output (applyDefaultCreateIssue). This auto-inject ensures agents can
	// always report their results, so the safeoutputs MCP server IS expected to be present.
	// Verify the auto-inject behaviour is active by checking for the auto-create-issue prompt.
	if !strings.Contains(yamlStr, "safe_outputs_auto_create_issue.md") {
		t.Error("Expected auto-injected create-issue prompt to be present when no safe-outputs are configured")
	}

	// The safeoutputs MCP server and config file are written because auto-inject enabled create-issue.
	if !strings.Contains(yamlStr, "Generate Safe Outputs Config") {
		t.Error("Expected safe-outputs configuration generation due to auto-injected create-issue")
	}
	if !strings.Contains(yamlStr, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs to be in MCP server configuration due to auto-injected create-issue")
	}

	// The auto-injected config should reference the create_issue tool, confirming the MCP
	// server is configured with the correct auto-injected output definition.
	if !strings.Contains(yamlStr, `"create_issue"`) {
		t.Error("Expected create_issue tool to be in safeoutputs config due to auto-injected create-issue")
	}

	// Explicitly configured safe-outputs like upload_artifact or custom jobs should NOT appear.
	if strings.Contains(yamlStr, "upload_artifact") {
		t.Error("Expected upload_artifact to NOT be configured when safe-outputs are not user-configured")
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

	// Check that safe-outputs configuration file generation step is present
	if !strings.Contains(yamlStr, "Generate Safe Outputs Config") {
		t.Error("Expected safe-outputs config generation step")
	}

	if !strings.Contains(yamlStr, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs in Codex gateway MCP server configuration")
	}

	// Codex consumes native TOML generated from the shared gateway JSON at runtime.
	if !strings.Contains(yamlStr, "convert_gateway_config_codex.cjs") {
		t.Error("Expected Codex gateway-to-native configuration converter")
	}
	if !strings.Contains(yamlStr, `cp "${RUNNER_TEMP}/gh-aw/mcp-config/config.toml" "${CODEX_HOME}/config.toml"`) {
		t.Error("Expected generated Codex config to be copied to CODEX_HOME")
	}

	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)
	if !strings.Contains(yamlStr, `"container": "`+pinnedGhAwNodeImage+`"`) {
		t.Error("Expected safeoutputs MCP server to run in the gh-aw node container")
	}
	if !strings.Contains(yamlStr, `"mounts": ["\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw", "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "/tmp/gh-aw:/tmp/gh-aw:rw"]`) {
		t.Error("Expected safeoutputs gateway MCP configuration to mount workspace, runtime files, and logs")
	}
	if !strings.Contains(yamlStr, `"entrypoint": "sh"`) {
		t.Error("Expected safeoutputs MCP server to override container entrypoint to sh")
	}
	if !strings.Contains(yamlStr, `"entrypointArgs": ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`) {
		t.Error("Expected safeoutputs MCP server entrypointArgs to run the stdio MCP server script")
	}

	t.Log("Safe outputs MCP server Codex integration test passed")
}
