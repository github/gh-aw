package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var devcontainerLog = logger.New("cli:devcontainer")

// DevcontainerCustomizations represents VSCode customizations in devcontainer.json
type DevcontainerCustomizations struct {
	VSCode     *DevcontainerVSCode     `json:"vscode,omitempty"`
	Codespaces *DevcontainerCodespaces `json:"codespaces,omitempty"`
}

// DevcontainerVSCode represents VSCode-specific settings
type DevcontainerVSCode struct {
	Settings   map[string]any `json:"settings,omitempty"`
	Extensions []string       `json:"extensions,omitempty"`
}

// DevcontainerCodespaces represents GitHub Codespaces-specific settings
type DevcontainerCodespaces struct {
	Repositories map[string]DevcontainerRepoPermissions `json:"repositories"`
}

// DevcontainerRepoPermissions represents permissions for a repository
type DevcontainerRepoPermissions struct {
	Permissions map[string]string `json:"permissions"`
}

// DevcontainerFeatures represents features to install in the devcontainer
type DevcontainerFeatures map[string]any

// DevcontainerBuild represents the build configuration for a devcontainer
type DevcontainerBuild struct {
	Dockerfile string            `json:"dockerfile,omitempty"`
	Context    string            `json:"context,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
}

// DevcontainerConfig represents the structure of devcontainer.json
type DevcontainerConfig struct {
	Name              string                      `json:"name"`
	Image             string                      `json:"image,omitempty"`
	Build             *DevcontainerBuild          `json:"build,omitempty"`
	Customizations    *DevcontainerCustomizations `json:"customizations,omitempty"`
	Features          DevcontainerFeatures        `json:"features,omitempty"`
	PostCreateCommand string                      `json:"postCreateCommand,omitempty"`
}

// ensureDevcontainerConfig creates or updates devcontainer.json
// If .devcontainer/devcontainer.json exists, it updates it with gh-aw configuration.
// If it doesn't exist, it creates it at the default location.
func ensureDevcontainerConfig(verbose bool, additionalRepos []string) error {
	devcontainerLog.Printf("Creating or updating devcontainer.json with additional repos: %v", additionalRepos)

	// Check for existing devcontainer at default location first
	defaultDevcontainerPath := filepath.Join(".devcontainer", "devcontainer.json")
	devcontainerPath := defaultDevcontainerPath

	// Create .devcontainer directory if it doesn't exist
	devcontainerDir := ".devcontainer"
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		return fmt.Errorf("failed to create .devcontainer directory: %w", err)
	}
	devcontainerLog.Printf("Ensured directory exists: %s", devcontainerDir)

	// Check if file already exists at default location
	var existingConfig *DevcontainerConfig
	if _, err := os.Stat(devcontainerPath); err == nil {
		devcontainerLog.Printf("File already exists: %s", devcontainerPath)

		// Read existing config to update it
		existingData, err := os.ReadFile(devcontainerPath)
		if err != nil {
			devcontainerLog.Printf("Failed to read existing config: %v", err)
			return fmt.Errorf("failed to read existing devcontainer.json: %w", err)
		}

		var config DevcontainerConfig
		if err := json.Unmarshal(existingData, &config); err != nil {
			devcontainerLog.Printf("Failed to parse existing config: %v", err)
			return fmt.Errorf("failed to parse existing devcontainer.json: %w", err)
		}
		existingConfig = &config
		devcontainerLog.Printf("Successfully parsed existing devcontainer.json")
	}

	// Get current repository name from git remote
	repoName := getCurrentRepoName()
	if repoName == "" {
		repoName = "current-repo"
	}

	// Get the owner from the current repository
	owner := getRepoOwner()

	// Prepare gh-aw specific configuration
	ghAwRepositories := buildRepositoryPermissions(repoName, owner, additionalRepos)

	var config DevcontainerConfig

	if existingConfig != nil {
		// Update existing configuration
		devcontainerLog.Printf("Updating existing devcontainer.json")
		config = *existingConfig

		// Ensure customizations exists
		if config.Customizations == nil {
			config.Customizations = &DevcontainerCustomizations{}
		}

		// Merge VSCode extensions
		if config.Customizations.VSCode == nil {
			config.Customizations.VSCode = &DevcontainerVSCode{}
		}
		config.Customizations.VSCode.Extensions = mergeExtensions(
			config.Customizations.VSCode.Extensions,
			[]string{"GitHub.copilot", "GitHub.copilot-chat"},
		)

		// Merge Codespaces repositories
		if config.Customizations.Codespaces == nil {
			config.Customizations.Codespaces = &DevcontainerCodespaces{
				Repositories: make(map[string]DevcontainerRepoPermissions),
			}
		}
		if config.Customizations.Codespaces.Repositories == nil {
			config.Customizations.Codespaces.Repositories = make(map[string]DevcontainerRepoPermissions)
		}
		for repo, perms := range ghAwRepositories {
			config.Customizations.Codespaces.Repositories[repo] = perms
			devcontainerLog.Printf("Updated permissions for repo: %s", repo)
		}

		// Merge features
		if config.Features == nil {
			config.Features = make(DevcontainerFeatures)
		}
		mergeFeatures(config.Features, map[string]any{
			"ghcr.io/devcontainers/features/github-cli:1":       map[string]any{},
			"ghcr.io/devcontainers/features/copilot-cli:latest": map[string]any{},
		})

		// Update postCreateCommand if not set or if it doesn't include gh-aw install
		if config.PostCreateCommand == "" || !strings.Contains(config.PostCreateCommand, "install-gh-aw.sh") {
			ghAwInstall := "curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash"
			if config.PostCreateCommand == "" {
				config.PostCreateCommand = ghAwInstall
			} else {
				config.PostCreateCommand = config.PostCreateCommand + " && " + ghAwInstall
			}
			devcontainerLog.Printf("Updated postCreateCommand to include gh-aw installation")
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "Updated existing devcontainer at %s\n", devcontainerPath)
		}
	} else {
		// Create new configuration
		devcontainerLog.Printf("Creating new devcontainer.json at default location")
		config = DevcontainerConfig{
			Name:  "Agentic Workflows Development",
			Image: "mcr.microsoft.com/devcontainers/universal:latest",
			Customizations: &DevcontainerCustomizations{
				VSCode: &DevcontainerVSCode{
					Extensions: []string{
						"GitHub.copilot",
						"GitHub.copilot-chat",
					},
				},
				Codespaces: &DevcontainerCodespaces{
					Repositories: ghAwRepositories,
				},
			},
			Features: DevcontainerFeatures{
				"ghcr.io/devcontainers/features/github-cli:1":       map[string]any{},
				"ghcr.io/devcontainers/features/copilot-cli:latest": map[string]any{},
			},
			PostCreateCommand: "curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash",
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "Created new devcontainer at %s\n", devcontainerPath)
		}
	}

	// Write config file with proper indentation
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal devcontainer.json: %w", err)
	}

	// Add newline at end of file
	data = append(data, '\n')

	// Use owner-only read/write permissions (0600) for security best practices
	if err := os.WriteFile(devcontainerPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write devcontainer.json: %w", err)
	}
	devcontainerLog.Printf("Wrote file: %s", devcontainerPath)

	return nil
}

// buildRepositoryPermissions creates the repository permissions map for gh-aw
func buildRepositoryPermissions(repoName, owner string, additionalRepos []string) map[string]DevcontainerRepoPermissions {
	// Create repository permissions map
	// Reference: https://docs.github.com/en/codespaces/managing-your-codespaces/managing-repository-access-for-your-codespaces
	// Default codespace permissions are read/write to the repository from which it was created.
	// For the current repo, we grant the standard codespace write permissions plus workflows:write
	// to enable triggering GitHub Actions workflows.
	// Note: Repository permissions can only be set for repositories in the same organization.
	repositories := map[string]DevcontainerRepoPermissions{
		repoName: {
			Permissions: map[string]string{
				"actions":       "write",
				"contents":      "write",
				"discussions":   "read",
				"issues":        "read",
				"pull-requests": "write",
				"workflows":     "write",
			},
		},
	}

	// Add additional repositories with read permissions
	// For additional repos, we grant default codespace read permissions plus workflows:read
	// to allow reading workflow definitions without write access.
	// Since permissions must be in the same organization, we automatically prepend the owner.
	// Reference: https://docs.github.com/en/codespaces/managing-your-codespaces/managing-repository-access-for-your-codespaces#setting-additional-repository-permissions
	for _, repo := range additionalRepos {
		if repo == "" {
			continue
		}

		// If repo already contains '/', validate that the owner matches
		// Otherwise, prepend the owner
		fullRepoName := repo
		if strings.Contains(repo, "/") {
			// Validate that the owner matches the current repo's owner
			parts := strings.Split(repo, "/")
			if len(parts) >= 2 {
				repoOwner := parts[0]
				if owner != "" && repoOwner != owner {
					// Skip repos with different owners rather than error
					devcontainerLog.Printf("Skipping repository '%s' - different owner than current repo (expected: '%s')", repo, owner)
					continue
				}
			}
		} else if owner != "" {
			fullRepoName = owner + "/" + repo
		}

		if fullRepoName != repoName {
			repositories[fullRepoName] = DevcontainerRepoPermissions{
				Permissions: map[string]string{
					"actions":       "read",
					"contents":      "read",
					"discussions":   "read",
					"issues":        "read",
					"pull-requests": "read",
					"workflows":     "read",
				},
			}
			devcontainerLog.Printf("Added read permissions for additional repo: %s", fullRepoName)
		}
	}

	return repositories
}

// mergeExtensions adds new extensions to existing list, avoiding duplicates
func mergeExtensions(existing, toAdd []string) []string {
	extensionSet := make(map[string]bool)
	result := make([]string, 0, len(existing)+len(toAdd))

	// Add existing extensions
	for _, ext := range existing {
		if !extensionSet[ext] {
			extensionSet[ext] = true
			result = append(result, ext)
		}
	}

	// Add new extensions if not already present
	for _, ext := range toAdd {
		if !extensionSet[ext] {
			extensionSet[ext] = true
			result = append(result, ext)
		}
	}

	return result
}

// mergeFeatures adds new features to existing features map, updating old copilot-cli versions
func mergeFeatures(existing DevcontainerFeatures, toAdd map[string]any) {
	// First, remove old copilot-cli versions
	for key := range existing {
		if strings.HasPrefix(key, "ghcr.io/devcontainers/features/copilot-cli:") &&
			key != "ghcr.io/devcontainers/features/copilot-cli:latest" {
			delete(existing, key)
			devcontainerLog.Printf("Removed old copilot-cli version: %s", key)
		}
	}

	// Add new features
	for key, value := range toAdd {
		existing[key] = value
	}
}

// getCurrentRepoName gets the current repository name from git remote in owner/repo format
func getCurrentRepoName() string {
	// Try to get the repository name from git remote using centralized helper
	slug := getRepositorySlugFromRemote()
	if slug != "" {
		return slug
	}

	// Fallback to directory name
	gitRoot, err := findGitRoot()
	if err != nil {
		return ""
	}
	return filepath.Base(gitRoot)
}

// getRepoOwner extracts the owner from the git remote URL
func getRepoOwner() string {
	// Use centralized helper to get full repo slug
	fullRepo := getRepositorySlugFromRemote()
	if fullRepo == "" {
		return ""
	}

	// Extract owner from "owner/repo" format
	parts := strings.Split(fullRepo, "/")
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}
