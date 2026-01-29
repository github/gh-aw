//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/testutil"
	"github.com/githubnext/gh-aw/pkg/workflow"
)

func TestInitRepository_WithMCP(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Create go.mod for the copilot-setup-steps.yml to reference
	goModContent := []byte("module github.com/test/repo\n\ngo 1.23\n")
	if err := os.WriteFile("go.mod", goModContent, 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Call the function with MCP flag (no campaign agent)
	err = InitRepository(false, true, false, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) with MCP returned error: %v", err)
	}

	// Verify standard files were created
	gitAttributesPath := filepath.Join(tempDir, ".gitattributes")
	if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
		t.Errorf("Expected .gitattributes file to exist")
	}

	// Verify copilot-setup-steps.yml was created
	setupStepsPath := filepath.Join(tempDir, ".github", "workflows", "copilot-setup-steps.yml")
	if _, err := os.Stat(setupStepsPath); os.IsNotExist(err) {
		t.Errorf("Expected copilot-setup-steps.yml to exist")
	} else {
		// Verify content contains key elements
		content, err := os.ReadFile(setupStepsPath)
		if err != nil {
			t.Fatalf("Failed to read copilot-setup-steps.yml: %v", err)
		}
		contentStr := string(content)

		if !strings.Contains(contentStr, "name: \"Copilot Setup Steps\"") {
			t.Errorf("Expected copilot-setup-steps.yml to contain workflow name")
		}
		if !strings.Contains(contentStr, "copilot-setup-steps:") {
			t.Errorf("Expected copilot-setup-steps.yml to contain job name")
		}
		if !strings.Contains(contentStr, "install-gh-aw.sh") {
			t.Errorf("Expected copilot-setup-steps.yml to contain gh-aw installation steps with bash script")
		}
	}

	// Verify .vscode/mcp.json was created
	mcpConfigPath := filepath.Join(tempDir, ".vscode", "mcp.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Errorf("Expected .vscode/mcp.json to exist")
	} else {
		// Verify content is valid JSON with gh-aw server
		content, err := os.ReadFile(mcpConfigPath)
		if err != nil {
			t.Fatalf("Failed to read .vscode/mcp.json: %v", err)
		}

		var config MCPConfig
		if err := json.Unmarshal(content, &config); err != nil {
			t.Fatalf("Failed to parse .vscode/mcp.json: %v", err)
		}

		if _, exists := config.Servers["github-agentic-workflows"]; !exists {
			t.Errorf("Expected .vscode/mcp.json to contain github-agentic-workflows server")
		}

		server := config.Servers["github-agentic-workflows"]
		if server.Command != "gh" {
			t.Errorf("Expected command to be 'gh', got %s", server.Command)
		}
		if len(server.Args) != 2 || server.Args[0] != "aw" || server.Args[1] != "mcp-server" {
			t.Errorf("Expected args to be ['aw', 'mcp-server'], got %v", server.Args)
		}
	}
}

func TestInitRepository_MCP_Idempotent(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Create go.mod
	goModContent := []byte("module github.com/test/repo\n\ngo 1.23\n")
	if err := os.WriteFile("go.mod", goModContent, 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Call the function first time with MCP
	err = InitRepository(false, true, false, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) with MCP returned error on first call: %v", err)
	}

	// Call the function second time with MCP
	err = InitRepository(false, true, false, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) with MCP returned error on second call: %v", err)
	}

	// Verify files still exist
	setupStepsPath := filepath.Join(tempDir, ".github", "workflows", "copilot-setup-steps.yml")
	if _, err := os.Stat(setupStepsPath); os.IsNotExist(err) {
		t.Errorf("Expected copilot-setup-steps.yml to exist after second call")
	}

	mcpConfigPath := filepath.Join(tempDir, ".vscode", "mcp.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Errorf("Expected .vscode/mcp.json to exist after second call")
	}
}

func TestEnsureMCPConfig_UpdatesExisting(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Create .vscode directory
	if err := os.MkdirAll(".vscode", 0755); err != nil {
		t.Fatalf("Failed to create .vscode directory: %v", err)
	}

	// Create initial mcp.json with a different server
	initialConfig := MCPConfig{
		Servers: map[string]VSCodeMCPServer{
			"other-server": {
				Command: "other-command",
				Args:    []string{"arg1"},
			},
		},
	}
	initialData, _ := json.MarshalIndent(initialConfig, "", "  ")
	mcpConfigPath := filepath.Join(tempDir, ".vscode", "mcp.json")
	if err := os.WriteFile(mcpConfigPath, initialData, 0644); err != nil {
		t.Fatalf("Failed to write initial mcp.json: %v", err)
	}

	// Call ensureMCPConfig
	if err := ensureMCPConfig(false); err != nil {
		t.Fatalf("ensureMCPConfig() returned error: %v", err)
	}

	// Verify the config now contains both servers
	content, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}

	var config MCPConfig
	if err := json.Unmarshal(content, &config); err != nil {
		t.Fatalf("Failed to parse mcp.json: %v", err)
	}

	// Check both servers exist
	if _, exists := config.Servers["other-server"]; !exists {
		t.Errorf("Expected existing 'other-server' to be preserved")
	}

	if _, exists := config.Servers["github-agentic-workflows"]; !exists {
		t.Errorf("Expected 'github-agentic-workflows' server to be added")
	}
}

func TestEnsureCopilotSetupSteps_InjectsStep(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create custom copilot-setup-steps.yml without extension install step
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	customContent := `name: "Copilot Setup Steps"

jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: Some step
        run: echo 'some'

      - name: Build code
        run: make build
`
	if err := os.WriteFile(setupStepsPath, []byte(customContent), 0644); err != nil {
		t.Fatalf("Failed to write custom setup steps: %v", err)
	}

	// Call ensureCopilotSetupSteps
	if err := ensureCopilotSetupSteps(false, workflow.ActionModeDev, "dev"); err != nil {
		t.Fatalf("ensureCopilotSetupSteps() returned error: %v", err)
	}

	// Verify the extension install step was injected
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read setup steps file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "Install gh-aw extension") {
		t.Errorf("Expected extension install step to be injected")
	}
	if !strings.Contains(contentStr, "install-gh-aw.sh") {
		t.Errorf("Expected extension install command to be present with bash script")
	}
	if !strings.Contains(contentStr, "curl -fsSL") {
		t.Errorf("Expected curl command to be present")
	}

	// Verify it was injected at the beginning (before other steps)
	extensionIndex := strings.Index(contentStr, "Install gh-aw extension")
	someStepIndex := strings.Index(contentStr, "Some step")
	buildIndex := strings.Index(contentStr, "Build code")

	if extensionIndex == -1 || someStepIndex == -1 || buildIndex == -1 {
		t.Fatalf("Could not find expected steps in file")
	}

	if extensionIndex >= someStepIndex || someStepIndex >= buildIndex {
		t.Errorf("Extension install step not in correct position (should be at beginning, before other steps)")
	}
}

func TestEnsureCopilotSetupSteps_SkipsWhenStepExists(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create copilot-setup-steps.yml that already has the extension install step
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	customContent := `name: "Copilot Setup Steps"

jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v5

      - name: Install gh-aw extension
        run: |
          curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/refs/heads/main/install-gh-aw.sh | bash

      - name: Build code
        run: make build
`
	if err := os.WriteFile(setupStepsPath, []byte(customContent), 0644); err != nil {
		t.Fatalf("Failed to write custom setup steps: %v", err)
	}

	// Call ensureCopilotSetupSteps
	if err := ensureCopilotSetupSteps(false, workflow.ActionModeDev, "dev"); err != nil {
		t.Fatalf("ensureCopilotSetupSteps() returned error: %v", err)
	}

	// Verify the file was not modified (content should be the same)
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read setup steps file: %v", err)
	}

	if string(content) != customContent {
		t.Errorf("Expected file to remain unchanged when extension step already exists")
	}
}
