//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestEnsureDevcontainerConfig(t *testing.T) {
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

	// Initialize git repo (required for getCurrentRepoName)
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Test creating devcontainer.json
	err = ensureDevcontainerConfig(false, []string{})
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() failed: %v", err)
	}

	// Verify .devcontainer/devcontainer.json was created at default location
	devcontainerPath := filepath.Join(".devcontainer", "devcontainer.json")
	if _, err := os.Stat(devcontainerPath); os.IsNotExist(err) {
		t.Fatal("Expected .devcontainer/devcontainer.json to be created")
	}

	// Read and parse the created file
	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read devcontainer.json: %v", err)
	}

	var config DevcontainerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse devcontainer.json: %v", err)
	}

	// Verify basic structure
	if config.Name == "" {
		t.Error("Expected name to be set")
	}

	if config.Image != "mcr.microsoft.com/devcontainers/universal:latest" {
		t.Errorf("Expected universal image, got %q", config.Image)
	}

	// Verify Codespaces configuration
	if config.Customizations == nil || config.Customizations.Codespaces == nil {
		t.Fatal("Expected Codespaces customizations to be set")
	}

	// Verify VSCode extensions
	if config.Customizations.VSCode == nil {
		t.Fatal("Expected VSCode customizations to be set")
	}

	extensions := config.Customizations.VSCode.Extensions
	hasGitHubCopilot := false
	hasCopilotChat := false
	for _, ext := range extensions {
		if ext == "GitHub.copilot" {
			hasGitHubCopilot = true
		}
		if ext == "GitHub.copilot-chat" {
			hasCopilotChat = true
		}
	}

	if !hasGitHubCopilot {
		t.Error("Expected GitHub.copilot extension to be included")
	}

	if !hasCopilotChat {
		t.Error("Expected GitHub.copilot-chat extension to be included")
	}

	// Verify GitHub CLI feature
	if config.Features == nil {
		t.Fatal("Expected features to be set")
	}

	if _, exists := config.Features["ghcr.io/devcontainers/features/github-cli:1"]; !exists {
		t.Error("Expected GitHub CLI feature to be included")
	}

	// Verify Copilot CLI feature
	if _, exists := config.Features["ghcr.io/devcontainers/features/copilot-cli:latest"]; !exists {
		t.Error("Expected Copilot CLI feature to be included")
	}

	// Verify postCreateCommand
	if config.PostCreateCommand == "" {
		t.Error("Expected postCreateCommand to be set")
	}

	// Test that running again doesn't fail (idempotency)
	err = ensureDevcontainerConfig(false, []string{})
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() should be idempotent, but failed: %v", err)
	}
}

func TestEnsureDevcontainerConfigWithAdditionalRepos(t *testing.T) {
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

	// Test creating devcontainer.json with additional repos
	additionalRepos := []string{"org/additional-repo1", "owner/additional-repo2"}
	err = ensureDevcontainerConfig(false, additionalRepos)
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() failed: %v", err)
	}

	// Read and parse the created file at default location
	devcontainerPath := filepath.Join(".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read devcontainer.json: %v", err)
	}

	var config DevcontainerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse devcontainer.json: %v", err)
	}

	// Verify additional repos are included
	if config.Customizations == nil || config.Customizations.Codespaces == nil {
		t.Fatal("Expected Codespaces customizations to be set")
	}

	if _, exists := config.Customizations.Codespaces.Repositories["org/additional-repo1"]; !exists {
		t.Error("Expected org/additional-repo1 to be in repositories")
	}

	if _, exists := config.Customizations.Codespaces.Repositories["owner/additional-repo2"]; !exists {
		t.Error("Expected owner/additional-repo2 to be in repositories")
	}

	// Verify read permissions for additional repos
	repo1 := config.Customizations.Codespaces.Repositories["org/additional-repo1"]
	if repo1.Permissions["contents"] != "read" {
		t.Errorf("Expected contents: read for org/additional-repo1, got %q", repo1.Permissions["contents"])
	}

	repo2 := config.Customizations.Codespaces.Repositories["owner/additional-repo2"]
	if repo2.Permissions["contents"] != "read" {
		t.Errorf("Expected contents: read for owner/additional-repo2, got %q", repo2.Permissions["contents"])
	}
}

func TestEnsureDevcontainerConfigWithCurrentRepo(t *testing.T) {
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

	// Test creating devcontainer.json
	err = ensureDevcontainerConfig(false, []string{})
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() failed: %v", err)
	}

	// Read and parse the created file at default location
	devcontainerPath := filepath.Join(".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read devcontainer.json: %v", err)
	}

	var config DevcontainerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse devcontainer.json: %v", err)
	}

	// Verify that current repo has workflows: write permission
	if config.Customizations == nil || config.Customizations.Codespaces == nil {
		t.Fatal("Expected Codespaces customizations to be set")
	}

	// Check if any repository has workflows: write (should be current repo)
	hasWorkflowsWrite := false
	for _, repo := range config.Customizations.Codespaces.Repositories {
		if repo.Permissions["workflows"] == "write" {
			hasWorkflowsWrite = true
			break
		}
	}

	if !hasWorkflowsWrite {
		t.Error("Expected at least one repository to have workflows: write permission")
	}
}

func TestEnsureDevcontainerConfigWithOwnerValidation(t *testing.T) {
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

	// Add remote with specific owner
	exec.Command("git", "remote", "add", "origin", "https://github.com/testowner/testrepo.git").Run()

	// Test that same owner succeeds
	err = ensureDevcontainerConfig(false, []string{"testowner/repo1"})
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() with same owner should succeed: %v", err)
	}

	// Verify the repo was added
	devcontainerPath := filepath.Join(".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read devcontainer.json: %v", err)
	}

	var config DevcontainerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse devcontainer.json: %v", err)
	}

	if _, exists := config.Customizations.Codespaces.Repositories["testowner/repo1"]; !exists {
		t.Error("Expected testowner/repo1 to be in repositories")
	}

	// Clean up for next test
	os.RemoveAll(filepath.Join(".devcontainer", "devcontainer.json"))

	// Test that different owner is skipped (not an error, just logged and skipped)
	err = ensureDevcontainerConfig(false, []string{"differentowner/repo2"})
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() with different owner should succeed but skip the repo: %v", err)
	}

	// Verify the file was created but without the different-owner repo
	data, err = os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read devcontainer.json: %v", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse devcontainer.json: %v", err)
	}

	// Different owner repo should not be in the config
	if _, exists := config.Customizations.Codespaces.Repositories["differentowner/repo2"]; exists {
		t.Error("Expected differentowner/repo2 to be skipped")
	}
}

func TestEnsureDevcontainerConfigUpdatesOldVersion(t *testing.T) {
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

	// Create .devcontainer directory
	devcontainerDir := ".devcontainer"
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Create a devcontainer.json with old copilot-cli version at default location
	oldConfig := DevcontainerConfig{
		Name:  "Existing Dev Environment",
		Image: "mcr.microsoft.com/devcontainers/go:latest",
		Features: DevcontainerFeatures{
			"ghcr.io/devcontainers/features/github-cli:1":  map[string]any{},
			"ghcr.io/devcontainers/features/copilot-cli:1": map[string]any{}, // Old version
		},
		PostCreateCommand: "echo 'existing setup'",
	}

	devcontainerPath := filepath.Join(devcontainerDir, "devcontainer.json")
	data, err := json.MarshalIndent(oldConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal old config: %v", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(devcontainerPath, data, 0644); err != nil {
		t.Fatalf("Failed to write old config: %v", err)
	}

	// Run ensureDevcontainerConfig - should update the version
	err = ensureDevcontainerConfig(false, []string{})
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() failed: %v", err)
	}

	// Read and verify the updated config
	updatedData, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read updated config: %v", err)
	}

	var updatedConfig DevcontainerConfig
	if err := json.Unmarshal(updatedData, &updatedConfig); err != nil {
		t.Fatalf("Failed to parse updated config: %v", err)
	}

	// Verify copilot-cli was updated to :latest
	if _, exists := updatedConfig.Features["ghcr.io/devcontainers/features/copilot-cli:latest"]; !exists {
		t.Error("Expected copilot-cli feature to be updated to :latest")
	}

	// Verify old version is gone
	if _, exists := updatedConfig.Features["ghcr.io/devcontainers/features/copilot-cli:1"]; exists {
		t.Error("Expected old copilot-cli:1 version to be removed")
	}

	// Verify existing config properties were preserved
	if updatedConfig.Name != "Existing Dev Environment" {
		t.Errorf("Expected name to be preserved, got %q", updatedConfig.Name)
	}

	if updatedConfig.Image != "mcr.microsoft.com/devcontainers/go:latest" {
		t.Errorf("Expected image to be preserved, got %q", updatedConfig.Image)
	}

	// Verify postCreateCommand was updated to include gh-aw
	if !strings.Contains(updatedConfig.PostCreateCommand, "install-gh-aw.sh") {
		t.Error("Expected postCreateCommand to include gh-aw installation")
	}
	if !strings.Contains(updatedConfig.PostCreateCommand, "echo 'existing setup'") {
		t.Error("Expected postCreateCommand to preserve existing command")
	}

	// Verify GitHub Copilot extensions were added
	hasGitHubCopilot := false
	hasCopilotChat := false
	for _, ext := range updatedConfig.Customizations.VSCode.Extensions {
		if ext == "GitHub.copilot" {
			hasGitHubCopilot = true
		}
		if ext == "GitHub.copilot-chat" {
			hasCopilotChat = true
		}
	}
	if !hasGitHubCopilot {
		t.Error("Expected GitHub.copilot extension to be added")
	}
	if !hasCopilotChat {
		t.Error("Expected GitHub.copilot-chat extension to be added")
	}
}

func TestEnsureDevcontainerConfigMergesWithExisting(t *testing.T) {
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

	// Configure git and add remote
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "remote", "add", "origin", "https://github.com/testorg/testrepo.git").Run()

	// Create .devcontainer directory
	devcontainerDir := ".devcontainer"
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Create an existing devcontainer.json with custom configuration
	existingConfig := DevcontainerConfig{
		Name:  "My Custom Dev Environment",
		Image: "mcr.microsoft.com/devcontainers/python:3.11",
		Customizations: &DevcontainerCustomizations{
			VSCode: &DevcontainerVSCode{
				Extensions: []string{
					"ms-python.python",
					"ms-python.vscode-pylance",
				},
			},
		},
		Features: DevcontainerFeatures{
			"ghcr.io/devcontainers/features/docker-in-docker:2": map[string]any{},
		},
		PostCreateCommand: "pip install -r requirements.txt",
	}

	devcontainerPath := filepath.Join(devcontainerDir, "devcontainer.json")
	data, err := json.MarshalIndent(existingConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal existing config: %v", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(devcontainerPath, data, 0644); err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	// Run ensureDevcontainerConfig - should merge with existing config
	err = ensureDevcontainerConfig(false, []string{})
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() failed: %v", err)
	}

	// Read and verify the merged config
	mergedData, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read merged config: %v", err)
	}

	var mergedConfig DevcontainerConfig
	if err := json.Unmarshal(mergedData, &mergedConfig); err != nil {
		t.Fatalf("Failed to parse merged config: %v", err)
	}

	// Verify existing properties were preserved
	if mergedConfig.Name != "My Custom Dev Environment" {
		t.Errorf("Expected name to be preserved, got %q", mergedConfig.Name)
	}

	if mergedConfig.Image != "mcr.microsoft.com/devcontainers/python:3.11" {
		t.Errorf("Expected image to be preserved, got %q", mergedConfig.Image)
	}

	// Verify existing extensions were preserved and new ones added
	extensions := mergedConfig.Customizations.VSCode.Extensions
	hasPython := false
	hasPylance := false
	hasGitHubCopilot := false
	hasCopilotChat := false

	for _, ext := range extensions {
		switch ext {
		case "ms-python.python":
			hasPython = true
		case "ms-python.vscode-pylance":
			hasPylance = true
		case "GitHub.copilot":
			hasGitHubCopilot = true
		case "GitHub.copilot-chat":
			hasCopilotChat = true
		}
	}

	if !hasPython {
		t.Error("Expected existing ms-python.python extension to be preserved")
	}
	if !hasPylance {
		t.Error("Expected existing ms-python.vscode-pylance extension to be preserved")
	}
	if !hasGitHubCopilot {
		t.Error("Expected GitHub.copilot extension to be added")
	}
	if !hasCopilotChat {
		t.Error("Expected GitHub.copilot-chat extension to be added")
	}

	// Verify existing features were preserved and new ones added
	if _, exists := mergedConfig.Features["ghcr.io/devcontainers/features/docker-in-docker:2"]; !exists {
		t.Error("Expected existing docker-in-docker feature to be preserved")
	}
	if _, exists := mergedConfig.Features["ghcr.io/devcontainers/features/github-cli:1"]; !exists {
		t.Error("Expected github-cli feature to be added")
	}
	if _, exists := mergedConfig.Features["ghcr.io/devcontainers/features/copilot-cli:latest"]; !exists {
		t.Error("Expected copilot-cli feature to be added")
	}

	// Verify postCreateCommand was updated to include gh-aw
	if !strings.Contains(mergedConfig.PostCreateCommand, "pip install -r requirements.txt") {
		t.Error("Expected postCreateCommand to preserve existing command")
	}
	if !strings.Contains(mergedConfig.PostCreateCommand, "install-gh-aw.sh") {
		t.Error("Expected postCreateCommand to include gh-aw installation")
	}

	// Verify codespaces repository permissions were added
	if mergedConfig.Customizations.Codespaces == nil {
		t.Fatal("Expected Codespaces configuration to be added")
	}
	if _, exists := mergedConfig.Customizations.Codespaces.Repositories["testorg/testrepo"]; !exists {
		t.Error("Expected testorg/testrepo to be in repositories")
	}
}

func TestEnsureDevcontainerConfigWithBuildField(t *testing.T) {
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

	// Configure git and add remote
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "remote", "add", "origin", "https://github.com/testorg/testrepo.git").Run()

	// Create .devcontainer directory
	devcontainerDir := ".devcontainer"
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Create an existing devcontainer.json with "build" field instead of "image"
	existingConfig := DevcontainerConfig{
		Name: "Custom Build Environment",
		Build: &DevcontainerBuild{
			Dockerfile: "Dockerfile",
		},
		Customizations: &DevcontainerCustomizations{
			VSCode: &DevcontainerVSCode{
				Extensions: []string{
					"golang.go",
				},
			},
		},
		Features: DevcontainerFeatures{
			"ghcr.io/devcontainers/features/docker-in-docker:2": map[string]any{},
		},
		PostCreateCommand: "make setup",
	}

	devcontainerPath := filepath.Join(devcontainerDir, "devcontainer.json")
	data, err := json.MarshalIndent(existingConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal existing config: %v", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(devcontainerPath, data, 0644); err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	// Run ensureDevcontainerConfig - should merge with existing config and preserve build field
	err = ensureDevcontainerConfig(false, []string{})
	if err != nil {
		t.Fatalf("ensureDevcontainerConfig() failed: %v", err)
	}

	// Read and verify the merged config
	mergedData, err := os.ReadFile(devcontainerPath)
	if err != nil {
		t.Fatalf("Failed to read merged config: %v", err)
	}

	var mergedConfig DevcontainerConfig
	if err := json.Unmarshal(mergedData, &mergedConfig); err != nil {
		t.Fatalf("Failed to parse merged config: %v", err)
	}

	// Verify the build field is preserved
	if mergedConfig.Build == nil {
		t.Fatal("Expected build field to be preserved")
	}

	if mergedConfig.Build.Dockerfile != "Dockerfile" {
		t.Errorf("Expected build.dockerfile to be 'Dockerfile', got %q", mergedConfig.Build.Dockerfile)
	}

	// Verify image field is not set
	if mergedConfig.Image != "" {
		t.Errorf("Expected image field to be empty when build is present, got %q", mergedConfig.Image)
	}

	// Verify existing properties were preserved
	if mergedConfig.Name != "Custom Build Environment" {
		t.Errorf("Expected name to be preserved, got %q", mergedConfig.Name)
	}

	// Verify existing extensions were preserved and new ones added
	extensions := mergedConfig.Customizations.VSCode.Extensions
	hasGolang := false
	hasGitHubCopilot := false
	hasCopilotChat := false

	for _, ext := range extensions {
		switch ext {
		case "golang.go":
			hasGolang = true
		case "GitHub.copilot":
			hasGitHubCopilot = true
		case "GitHub.copilot-chat":
			hasCopilotChat = true
		}
	}

	if !hasGolang {
		t.Error("Expected existing golang.go extension to be preserved")
	}
	if !hasGitHubCopilot {
		t.Error("Expected GitHub.copilot extension to be added")
	}
	if !hasCopilotChat {
		t.Error("Expected GitHub.copilot-chat extension to be added")
	}

	// Verify existing features were preserved and new ones added
	if _, exists := mergedConfig.Features["ghcr.io/devcontainers/features/docker-in-docker:2"]; !exists {
		t.Error("Expected existing docker-in-docker feature to be preserved")
	}
	if _, exists := mergedConfig.Features["ghcr.io/devcontainers/features/github-cli:1"]; !exists {
		t.Error("Expected github-cli feature to be added")
	}
	if _, exists := mergedConfig.Features["ghcr.io/devcontainers/features/copilot-cli:latest"]; !exists {
		t.Error("Expected copilot-cli feature to be added")
	}

	// Verify postCreateCommand was updated to include gh-aw
	if !strings.Contains(mergedConfig.PostCreateCommand, "make setup") {
		t.Error("Expected postCreateCommand to preserve existing command")
	}
	if !strings.Contains(mergedConfig.PostCreateCommand, "install-gh-aw.sh") {
		t.Error("Expected postCreateCommand to include gh-aw installation")
	}

	// Verify codespaces repository permissions were added
	if mergedConfig.Customizations.Codespaces == nil {
		t.Fatal("Expected Codespaces configuration to be added")
	}
	if _, exists := mergedConfig.Customizations.Codespaces.Repositories["testorg/testrepo"]; !exists {
		t.Error("Expected testorg/testrepo to be in repositories")
	}
}

func TestGetCurrentRepoName(t *testing.T) {
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

	// Get repo name
	repoName := getCurrentRepoName()
	if repoName == "" {
		t.Error("Expected getCurrentRepoName() to return a non-empty string")
	}
}
