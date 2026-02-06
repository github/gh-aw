package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var pluginInstallLog = logger.New("workflow:plugin_installation")

// GeneratePluginInstallationSteps generates GitHub Actions steps to install plugins for the given engine.
// Each plugin is installed using the engine-specific CLI command with the github-token environment variable set.
//
// Parameters:
//   - plugins: List of plugin repository slugs (e.g., ["org/repo", "org2/repo2"])
//   - engineID: The engine identifier ("copilot", "claude", "codex")
//   - githubToken: The GitHub token expression to use for authentication (defaults to "${{ secrets.GITHUB_TOKEN }}")
//
// Returns:
//   - Slice of GitHubActionStep containing the installation steps for all plugins
func GeneratePluginInstallationSteps(plugins []string, engineID string, githubToken string) []GitHubActionStep {
	if len(plugins) == 0 {
		pluginInstallLog.Print("No plugins to install")
		return []GitHubActionStep{}
	}

	pluginInstallLog.Printf("Generating plugin installation steps: engine=%s, plugins=%d", engineID, len(plugins))

	// Default to GITHUB_TOKEN if no token is specified
	if githubToken == "" {
		githubToken = "${{ secrets.GITHUB_TOKEN }}"
	}

	var steps []GitHubActionStep

	// Generate installation steps for each plugin
	for _, plugin := range plugins {
		step := generatePluginInstallStep(plugin, engineID, githubToken)
		steps = append(steps, step)
		pluginInstallLog.Printf("Generated plugin install step: plugin=%s, engine=%s", plugin, engineID)
	}

	return steps
}

// generatePluginInstallStep generates a single GitHub Actions step to install a plugin.
// The step uses the engine-specific CLI command with proper authentication.
func generatePluginInstallStep(plugin, engineID, githubToken string) GitHubActionStep {
	// Determine the command based on the engine
	var command string
	switch engineID {
	case "copilot":
		command = fmt.Sprintf("copilot install plugin %s", plugin)
	case "claude":
		command = fmt.Sprintf("claude install plugin %s", plugin)
	case "codex":
		command = fmt.Sprintf("codex install plugin %s", plugin)
	default:
		// For unknown engines, use a generic format
		command = fmt.Sprintf("%s install plugin %s", engineID, plugin)
	}

	// Quote the step name to avoid YAML syntax issues with special characters
	stepName := fmt.Sprintf("'Install plugin: %s'", plugin)

	return GitHubActionStep{
		fmt.Sprintf("      - name: %s", stepName),
		"        env:",
		fmt.Sprintf("          GITHUB_TOKEN: %s", githubToken),
		fmt.Sprintf("        run: %s", command),
	}
}
