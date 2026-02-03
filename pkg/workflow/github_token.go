package workflow

import "github.com/github/gh-aw/pkg/logger"

var tokenLog = logger.New("workflow:github_token")

// getEffectiveGitHubToken returns the GitHub token to use, with precedence:
// 1. Custom token passed as parameter (e.g., from tool-specific config)
// 2. Top-level github-token from frontmatter
// 3. Default fallback: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
func getEffectiveGitHubToken(customToken, toplevelToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom GitHub token")
		return customToken
	}
	if toplevelToken != "" {
		tokenLog.Print("Using top-level GitHub token from frontmatter")
		return toplevelToken
	}
	tokenLog.Print("Using default GitHub token fallback")
	return "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
}

// getEffectiveSafeOutputGitHubToken returns the GitHub token to use for safe output operations, with precedence:
// 1. Custom token passed as parameter (e.g., from per-output config)
// 2. Top-level github-token from frontmatter
// 3. Default fallback: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
// This simpler chain ensures safe outputs use: safe outputs token -> workflow token -> GH_AW_GITHUB_TOKEN -> GitHub Actions token
func getEffectiveSafeOutputGitHubToken(customToken, toplevelToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom safe output GitHub token")
		return customToken
	}
	if toplevelToken != "" {
		tokenLog.Print("Using top-level safe output GitHub token from frontmatter")
		return toplevelToken
	}
	tokenLog.Print("Using default safe output GitHub token (GH_AW_GITHUB_TOKEN || GITHUB_TOKEN)")
	return "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
}

// getEffectiveCopilotGitHubToken returns the GitHub token to use for Copilot-related operations,
// with precedence:
// 1. Custom token passed as parameter (e.g., from safe-outputs config github-token field)
// 2. secrets.COPILOT_GITHUB_TOKEN (recommended token for Copilot operations)
// 3. secrets.GH_AW_GITHUB_TOKEN (legacy fallback for backward compatibility)
// Note: The default GITHUB_TOKEN is NOT included as a fallback because it does not have
// permission to create agent sessions, assign issues to bots, or add bots as reviewers.
// This is used for safe outputs that interact with GitHub Copilot features:
// - create-agent-session
// - assigning "copilot" to issues
// - adding "copilot" as PR reviewer
func getEffectiveCopilotGitHubToken(customToken, toplevelToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom Copilot GitHub token")
		return customToken
	}
	if toplevelToken != "" {
		tokenLog.Print("Using top-level Copilot GitHub token from frontmatter")
		return toplevelToken
	}
	tokenLog.Print("Using default Copilot GitHub token fallback")
	return "${{ secrets.COPILOT_GITHUB_TOKEN || secrets.GH_AW_GITHUB_TOKEN }}"
}

// getEffectiveAgentGitHubToken returns the GitHub token to use for agent assignment operations,
// with precedence:
// 1. Custom token passed as parameter (e.g., from safe-outputs config github-token field)
// 2. Top-level github-token from frontmatter
// 3. secrets.GH_AW_AGENT_TOKEN (recommended token for agent assignment with elevated permissions)
// 4. secrets.GH_AW_GITHUB_TOKEN (fallback with potentially sufficient permissions)
// 5. secrets.GITHUB_TOKEN (last resort, may lack permissions for bot assignment)
// Note: Assigning bots (like copilot-swe-agent) requires permissions that GITHUB_TOKEN may not have.
// It's recommended to configure GH_AW_AGENT_TOKEN or GH_AW_GITHUB_TOKEN with appropriate permissions.
func getEffectiveAgentGitHubToken(customToken, toplevelToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom agent GitHub token")
		return customToken
	}
	if toplevelToken != "" {
		tokenLog.Print("Using top-level agent GitHub token from frontmatter")
		return toplevelToken
	}
	tokenLog.Print("Using default agent GitHub token fallback chain (GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN)")
	return "${{ secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
}

// getEffectiveProjectGitHubToken returns the GitHub token to use for GitHub Projects v2 operations,
// with precedence:
// 1. Custom token passed as parameter (e.g., from safe-outputs.update-project.github-token)
// 2. Top-level github-token from frontmatter
// 3. secrets.GH_AW_PROJECT_GITHUB_TOKEN (required token for Projects v2 operations)
// Note: GitHub Projects v2 requires a PAT (classic with project + repo scopes, or fine-grained
// with Projects: Read+Write) or GitHub App. The default GITHUB_TOKEN cannot access Projects v2.
// You must configure GH_AW_PROJECT_GITHUB_TOKEN or provide a custom token for Projects v2 operations.
// No fallback to GITHUB_TOKEN is provided as it will never work for Projects v2 operations.
func getEffectiveProjectGitHubToken(customToken, toplevelToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom project GitHub token")
		return customToken
	}
	if toplevelToken != "" {
		tokenLog.Print("Using top-level GitHub token for project operations")
		return toplevelToken
	}
	tokenLog.Print("Using GH_AW_PROJECT_GITHUB_TOKEN for project operations")
	return "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}"
}
