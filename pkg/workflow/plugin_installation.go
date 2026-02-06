package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var pluginInstallLog = logger.New("workflow:plugin_installation")

// getEffectivePluginGitHubToken returns the GitHub token to use for plugin installation, with cascading precedence:
// 1. Custom token from plugins.github-token field (highest priority, overrides all defaults)
// 2. secrets.GH_AW_PLUGINS_TOKEN (recommended token for plugin operations)
// 3. secrets.GH_AW_GITHUB_TOKEN (general-purpose gh-aw token)
// 4. secrets.GITHUB_TOKEN (default GitHub Actions token)
// This cascading approach allows users to configure a dedicated token for plugin operations while
// providing sensible fallbacks for common use cases.
func getEffectivePluginGitHubToken(customToken string) string {
	if customToken != "" {
		pluginInstallLog.Print("Using custom plugin GitHub token (from plugins.github-token or top-level github-token)")
		return customToken
	}
	pluginInstallLog.Print("Using cascading plugin GitHub token (GH_AW_PLUGINS_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN)")
	return "${{ secrets.GH_AW_PLUGINS_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
}

// GeneratePluginInstallationSteps generates GitHub Actions steps to install plugins for the given engine.
// Each plugin is installed using the engine-specific CLI command with the github-token environment variable set.
//
// Parameters:
//   - plugins: List of plugin repository slugs (e.g., ["org/repo", "org2/repo2"])
//   - engineID: The engine identifier ("copilot", "claude", "codex")
//   - githubToken: The GitHub token expression to use for authentication (uses cascading resolution if empty)
//
// Returns:
//   - Slice of GitHubActionStep containing the installation steps for all plugins
func GeneratePluginInstallationSteps(plugins []string, engineID string, githubToken string) []GitHubActionStep {
	if len(plugins) == 0 {
		pluginInstallLog.Print("No plugins to install")
		return []GitHubActionStep{}
	}

	pluginInstallLog.Printf("Generating plugin installation steps: engine=%s, plugins=%d", engineID, len(plugins))

	// Use cascading token resolution
	effectiveToken := getEffectivePluginGitHubToken(githubToken)

	var steps []GitHubActionStep

	// Generate installation steps for each plugin
	for _, plugin := range plugins {
		step := generatePluginInstallStep(plugin, engineID, effectiveToken)
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
		command = fmt.Sprintf("copilot plugin install %s", plugin)
	case "claude":
		// TODO: validate the correct claude CLI plugin install command syntax
		command = fmt.Sprintf("claude plugin install %s", plugin)
	case "codex":
		// TODO: validate the correct codex CLI plugin install command syntax
		command = fmt.Sprintf("codex plugin install %s", plugin)
	default:
		// For unknown engines, use a generic format
		command = fmt.Sprintf("%s plugin install %s", engineID, plugin)
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
