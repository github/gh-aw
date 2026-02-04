//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestNewInitCommand(t *testing.T) {
	t.Parallel()

	cmd := NewInitCommand()

	if cmd == nil {
		t.Fatal("NewInitCommand() returned nil")
	}

	if cmd.Use != "init" {
		t.Errorf("Expected Use to be 'init', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("Expected Short description to be set")
	}

	if cmd.Long == "" {
		t.Error("Expected Long description to be set")
	}

	// Verify flags
	noMcpFlag := cmd.Flags().Lookup("no-mcp")
	if noMcpFlag == nil {
		t.Error("Expected 'no-mcp' flag to be defined")
		return
	}

	// Verify hidden --mcp flag still exists for backward compatibility
	mcpFlag := cmd.Flags().Lookup("mcp")
	if mcpFlag == nil {
		t.Error("Expected 'mcp' flag to be defined (for backward compatibility)")
		return
	}

	// Verify --mcp flag is hidden
	if !mcpFlag.Hidden {
		t.Error("Expected 'mcp' flag to be hidden")
	}

	if noMcpFlag.DefValue != "false" {
		t.Errorf("Expected no-mcp flag default to be 'false', got %q", noMcpFlag.DefValue)
	}

	if mcpFlag.DefValue != "false" {
		t.Errorf("Expected mcp flag default to be 'false', got %q", mcpFlag.DefValue)
	}

	codespaceFlag := cmd.Flags().Lookup("codespaces")
	if codespaceFlag == nil {
		t.Error("Expected 'codespaces' flag to be defined")
		return
	}

	// String flags with NoOptDefVal have "" as default value
	if codespaceFlag.DefValue != "" {
		t.Errorf("Expected codespaces flag default to be '', got %q", codespaceFlag.DefValue)
	}

	// Verify NoOptDefVal is set to a space (allows --codespaces without value)
	if codespaceFlag.NoOptDefVal != " " {
		t.Errorf("Expected codespaces flag NoOptDefVal to be ' ' (space), got %q", codespaceFlag.NoOptDefVal)
	}

	// Check push flag
	pushFlag := cmd.Flags().Lookup("push")
	if pushFlag == nil {
		t.Error("Expected 'push' flag to be defined")
		return
	}

	// Check create-pull-request flags
	createPRFlag := cmd.Flags().Lookup("create-pull-request")
	if createPRFlag == nil {
		t.Error("Expected 'create-pull-request' flag to be defined")
		return
	}

	prFlag := cmd.Flags().Lookup("pr")
	if prFlag == nil {
		t.Error("Expected 'pr' flag to be defined (alias)")
		return
	}

	// Verify --pr flag is hidden
	if !prFlag.Hidden {
		t.Error("Expected 'pr' flag to be hidden")
	}
}

func TestInitCommandHelp(t *testing.T) {
	t.Parallel()

	cmd := NewInitCommand()

	// Test that help can be generated without error
	helpText := cmd.Long
	if !strings.Contains(helpText, "Initialize") {
		t.Error("Expected help text to contain 'Initialize'")
	}

	if !strings.Contains(helpText, ".gitattributes") {
		t.Error("Expected help text to mention .gitattributes")
	}

	if !strings.Contains(helpText, "Copilot") {
		t.Error("Expected help text to mention Copilot")
	}

	if !strings.Contains(helpText, "Interactive Mode") {
		t.Error("Expected help text to mention Interactive Mode")
	}
}

func TestInitCommandInteractiveModeDetection(t *testing.T) {
	t.Parallel()

	// Test that interactive mode is triggered when no flags are set
	// We can't test the actual interactive prompts in unit tests, but we can
	// verify that the command structure supports the detection logic

	cmd := NewInitCommand()

	// Verify that all the flags exist that are checked for interactive mode detection
	requiredFlags := []string{"mcp", "no-mcp", "tokens", "engine", "codespaces", "completions", "push", "create-pull-request", "pr"}
	for _, flagName := range requiredFlags {
		flag := cmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("Expected flag %q to exist for interactive mode detection", flagName)
		}
	}
}

func TestInitRepositoryInteractivePreconditions(t *testing.T) {
	// Test that InitRepositoryInteractive properly checks preconditions

	// Test 1: Fails in CI environment
	t.Setenv("CI", "true")
	err := InitRepositoryInteractive(false, nil)
	if err == nil {
		t.Error("Expected InitRepositoryInteractive to fail in CI environment")
	}
	if !strings.Contains(err.Error(), "automated tests or CI") {
		t.Errorf("Expected error about CI environment, got: %v", err)
	}

	// Test 2: Fails in test mode
	t.Setenv("CI", "")
	t.Setenv("GO_TEST_MODE", "true")
	err = InitRepositoryInteractive(false, nil)
	if err == nil {
		t.Error("Expected InitRepositoryInteractive to fail in test mode")
	}
	if !strings.Contains(err.Error(), "automated tests or CI") {
		t.Errorf("Expected error about test mode, got: %v", err)
	}
}

func TestInitRepositoryBasic(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo (required for some init operations)
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Test basic init with MCP enabled by default (mcp=true, noMcp=false behavior)
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) failed: %v", err)
	}

	// Verify .gitattributes was created/updated
	gitAttributesPath := ".gitattributes"
	if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
		t.Error("Expected .gitattributes to be created")
	}

	// Read and verify .gitattributes content
	content, err := os.ReadFile(gitAttributesPath)
	if err != nil {
		t.Fatalf("Failed to read .gitattributes: %v", err)
	}

	expectedEntry := ".github/workflows/*.lock.yml linguist-generated=true merge=ours"
	if !strings.Contains(string(content), expectedEntry) {
		t.Errorf("Expected .gitattributes to contain %q", expectedEntry)
	}

	// Verify MCP files were created by default
	mcpConfigPath := filepath.Join(".vscode", "mcp.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Error("Expected .vscode/mcp.json to be created by default")
	}

	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	if _, err := os.Stat(setupStepsPath); os.IsNotExist(err) {
		t.Error("Expected .github/workflows/copilot-setup-steps.yml to be created by default")
	}
}

func TestInitRepositoryWithMCP(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Test init with MCP explicitly enabled (same as default)
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) with MCP failed: %v", err)
	}

	// Verify .vscode/mcp.json was created
	mcpConfigPath := filepath.Join(".vscode", "mcp.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Error("Expected .vscode/mcp.json to be created")
	}

	// Verify copilot-setup-steps.yml was created
	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	if _, err := os.Stat(setupStepsPath); os.IsNotExist(err) {
		t.Error("Expected .github/workflows/copilot-setup-steps.yml to be created")
	}
}

func TestInitRepositoryWithNoMCP(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Test init with --no-mcp flag (mcp=false)
	err = InitRepository(false, false, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) with --no-mcp failed: %v", err)
	}

	// Verify .vscode/mcp.json was NOT created
	mcpConfigPath := filepath.Join(".vscode", "mcp.json")
	if _, err := os.Stat(mcpConfigPath); err == nil {
		t.Error("Expected .vscode/mcp.json to NOT be created with --no-mcp flag")
	}

	// Verify copilot-setup-steps.yml was NOT created
	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	if _, err := os.Stat(setupStepsPath); err == nil {
		t.Error("Expected .github/workflows/copilot-setup-steps.yml to NOT be created with --no-mcp flag")
	}

	// Verify basic files were still created
	if _, err := os.Stat(".gitattributes"); os.IsNotExist(err) {
		t.Error("Expected .gitattributes to be created even with --no-mcp flag")
	}
}

func TestInitRepositoryWithMCPBackwardCompatibility(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Test init with deprecated --mcp flag for backward compatibility (mcp=true)
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) with deprecated --mcp flag failed: %v", err)
	}

	// Verify .vscode/mcp.json was created
	mcpConfigPath := filepath.Join(".vscode", "mcp.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Error("Expected .vscode/mcp.json to be created with --mcp flag (backward compatibility)")
	}

	// Verify copilot-setup-steps.yml was created
	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	if _, err := os.Stat(setupStepsPath); os.IsNotExist(err) {
		t.Error("Expected .github/workflows/copilot-setup-steps.yml to be created with --mcp flag (backward compatibility)")
	}
}

func TestInitRepositoryVerbose(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Test verbose mode with MCP enabled by default (should not error, just produce more output)
	err = InitRepository(true, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) in verbose mode failed: %v", err)
	}

	// Verify basic files were still created
	if _, err := os.Stat(".gitattributes"); os.IsNotExist(err) {
		t.Error("Expected .gitattributes to be created even in verbose mode")
	}
}

func TestInitRepositoryNotInGitRepo(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Don't initialize git repo - should fail for some operations
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)

	// The function should handle this gracefully or return an error
	// Based on the implementation, ensureGitAttributes requires git
	if err == nil {
		t.Log("InitRepository(, false, false, false, nil) succeeded despite not being in a git repo")
	}
}

func TestInitRepositoryIdempotent(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Run init twice with MCP enabled by default
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("First InitRepository(, false, false, false, nil) failed: %v", err)
	}

	// Second run should be idempotent
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("Second InitRepository(, false, false, false, nil) failed: %v", err)
	}

	// Verify .gitattributes still correct
	content, err := os.ReadFile(".gitattributes")
	if err != nil {
		t.Fatalf("Failed to read .gitattributes: %v", err)
	}

	expectedEntry := ".github/workflows/*.lock.yml linguist-generated=true merge=ours"

	// Count occurrences - should only appear once
	count := strings.Count(string(content), expectedEntry)
	if count != 1 {
		t.Errorf("Expected .gitattributes entry to appear exactly once, got %d occurrences", count)
	}
}

func TestInitRepositoryWithMCPIdempotent(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Run init with MCP twice
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("First InitRepository(, false, false, false, nil) with MCP failed: %v", err)
	}

	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("Second InitRepository(, false, false, false, nil) with MCP failed: %v", err)
	}

	// Verify files still exist and are correct
	mcpConfigPath := filepath.Join(".vscode", "mcp.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Error("Expected .vscode/mcp.json to still exist after second run")
	}

	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	if _, err := os.Stat(setupStepsPath); os.IsNotExist(err) {
		t.Error("Expected copilot-setup-steps.yml to still exist after second run")
	}
}

func TestInitRepositoryCreatesDirectories(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Run init with MCP
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) failed: %v", err)
	}

	// Verify directory structure
	vscodeDir := ".vscode"
	info, err := os.Stat(vscodeDir)
	if os.IsNotExist(err) {
		t.Error("Expected .vscode directory to be created")
	} else if !info.IsDir() {
		t.Error("Expected .vscode to be a directory")
	}

	workflowsDir := filepath.Join(".github", "workflows")
	info, err = os.Stat(workflowsDir)
	if os.IsNotExist(err) {
		t.Error("Expected .github/workflows directory to be created")
	} else if !info.IsDir() {
		t.Error("Expected .github/workflows to be a directory")
	}
}

func TestInitCommandFlagValidation(t *testing.T) {
	t.Parallel()

	cmd := NewInitCommand()

	// Test that no-mcp flag is a boolean
	noMcpFlag := cmd.Flags().Lookup("no-mcp")
	if noMcpFlag == nil {
		t.Fatal("Expected 'no-mcp' flag to exist")
	}

	if noMcpFlag.Value.Type() != "bool" {
		t.Errorf("Expected no-mcp flag to be bool, got %s", noMcpFlag.Value.Type())
	}

	// Test verbose flag exists (inherited from parent command likely)
	// Note: verbose flag might be added by parent command, not in init command itself
}

func TestInitRepositoryErrorHandling(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Test init without git repo (with MCP enabled by default)
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)

	// Should handle error gracefully or return error
	// The actual behavior depends on implementation
	if err != nil {
		// Error is acceptable if git is required
		if !strings.Contains(err.Error(), "git") {
			t.Logf("Received error (acceptable): %v", err)
		}
	}
}

func TestInitRepositoryWithExistingFiles(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create existing .gitattributes with different content
	existingContent := "*.md linguist-documentation=true\n"
	if err := os.WriteFile(".gitattributes", []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create existing .gitattributes: %v", err)
	}

	// Run init with MCP enabled by default
	err = InitRepository(false, true, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) failed: %v", err)
	}

	// Verify existing content is preserved and new entry is added
	content, err := os.ReadFile(".gitattributes")
	if err != nil {
		t.Fatalf("Failed to read .gitattributes: %v", err)
	}

	contentStr := string(content)

	// Should contain both old and new entries
	if !strings.Contains(contentStr, "*.md linguist-documentation=true") {
		t.Error("Expected existing content to be preserved")
	}

	expectedEntry := ".github/workflows/*.lock.yml linguist-generated=true merge=ours"
	if !strings.Contains(contentStr, expectedEntry) {
		t.Error("Expected new entry to be added")
	}
}

func TestInitRepositoryWithCodespace(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Test init with --codespaces flag (with MCP enabled by default and additional repos)
	additionalRepos := []string{"org/repo1", "owner/repo2"}
	err = InitRepository(false, true, false, "", additionalRepos, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) with codespaces failed: %v", err)
	}

	// Verify .devcontainer/devcontainer.json was created at default location
	devcontainerPath := filepath.Join(".devcontainer", "devcontainer.json")
	if _, err := os.Stat(devcontainerPath); os.IsNotExist(err) {
		t.Error("Expected .devcontainer/devcontainer.json to be created")
	}

	// Verify additional repos were added
	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read devcontainer.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "org/repo1") {
		t.Error("Expected org/repo1 to be in devcontainer.json")
	}
	if !strings.Contains(content, "owner/repo2") {
		t.Error("Expected owner/repo2 to be in devcontainer.json")
	}

	// Verify basic files are still created
	gitAttributesPath := ".gitattributes"
	if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
		t.Error("Expected .gitattributes to be created")
	}
}

func TestInitCommandWithCodespacesNoArgs(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Save and restore original directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize a git repository
	err = exec.Command("git", "init").Run()
	if err != nil {
		t.Skip("Git not available")
	}

	// Create a mock git remote to test owner extraction
	err = exec.Command("git", "remote", "add", "origin", "https://github.com/testorg/testrepo.git").Run()
	if err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Test init with --codespaces flag (no additional repos, MCP enabled by default)
	err = InitRepository(false, true, false, "", []string{}, true, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) with codespaces (no args) failed: %v", err)
	}

	// Verify .devcontainer/devcontainer.json was created at default location
	devcontainerPath := filepath.Join(".devcontainer", "devcontainer.json")
	if _, err := os.Stat(devcontainerPath); os.IsNotExist(err) {
		t.Error("Expected .devcontainer/devcontainer.json to be created")
	}

	// Verify only current repo is configured
	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read devcontainer.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "testorg/testrepo") {
		t.Error("Expected testorg/testrepo to be in devcontainer.json")
	}

	// Verify basic files are still created
	gitAttributesPath := ".gitattributes"
	if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
		t.Error("Expected .gitattributes to be created")
	}
}
