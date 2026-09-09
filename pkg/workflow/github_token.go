package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var tokenLog = logger.New("workflow:github_token")

func wrapGitHubExpression(expression string) string {
	return "${{ " + strings.TrimSpace(expression) + " }}"
}

func combineTokenExpressions(primaryExpression, fallbackExpression string) string {
	combined := BuildOr(
		&ExpressionNode{Expression: stripExpressionWrapper(primaryExpression)},
		&ExpressionNode{Expression: stripExpressionWrapper(fallbackExpression)},
	)
	return wrapGitHubExpression(RenderCondition(combined))
}

// getEffectiveGitHubToken returns the GitHub token to use, with precedence:
// 1. Custom token passed as parameter (e.g., from tool-specific config)
// 2. Default fallback: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
func getEffectiveGitHubToken(customToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom GitHub token")
		return customToken
	}
	tokenLog.Print("Using default GitHub token fallback")
	return "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
}

// getEffectiveSafeOutputGitHubToken returns the GitHub token to use for safe output operations, with precedence:
// 1. Custom token passed as parameter (e.g., from per-output config)
// 2. Default fallback: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
// This simpler chain ensures safe outputs use: safe outputs token -> GH_AW_GITHUB_TOKEN -> GitHub Actions token
func getEffectiveSafeOutputGitHubToken(customToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom safe output GitHub token")
		return customToken
	}
	tokenLog.Print("Using default safe output GitHub token (GH_AW_GITHUB_TOKEN || GITHUB_TOKEN)")
	return "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
}

// getEffectiveMaintenanceGitHubToken returns the configured GitHub token secret
// expression to use for maintenance compile-workflows operations.
//
// No fallback chain is applied here. Maintenance compile PR mode must use the
// explicitly configured secret so the generated workflow does not silently fall
// back to a token without permission to write workflow files.
func getEffectiveMaintenanceGitHubToken(secretName string) string {
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		tokenLog.Print("No maintenance compile GitHub token secret configured")
		return ""
	}
	tokenLog.Printf("Using configured maintenance compile GitHub token secret %q", secretName)
	return wrapGitHubExpression("secrets." + secretName)
}

// getEffectiveCopilotRequestsToken returns the GitHub token to use for Copilot-related operations,
// with precedence:
// 1. Custom token passed as parameter (e.g., from safe-outputs config github-token field)
// 2. secrets.COPILOT_GITHUB_TOKEN (recommended token for Copilot operations)
// Note: The default GITHUB_TOKEN is NOT included as a fallback because it does not have
// permission to create agent sessions, assign issues to bots, or add bots as reviewers.
// This is used for safe outputs that interact with GitHub Copilot features:
// - create-agent-session
// - assigning "copilot" to issues
// - adding "copilot" as PR reviewer
func getEffectiveCopilotRequestsToken(customToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom Copilot GitHub token")
		return customToken
	}
	return "${{ secrets.COPILOT_GITHUB_TOKEN }}"
}

// getEffectiveCopilotCodingAgentGitHubToken returns the GitHub token to use for agent assignment operations,
// with precedence:
// 1. Custom token passed as parameter (e.g., from safe-outputs config github-token field)
// 2. secrets.GH_AW_AGENT_TOKEN (recommended token for agent assignment with elevated permissions)
// 3. secrets.GH_AW_GITHUB_TOKEN (fallback with potentially sufficient permissions)
// 4. secrets.GITHUB_TOKEN (last resort, may lack permissions for bot assignment)
// Note: Assigning bots (like copilot-swe-agent) requires permissions that GITHUB_TOKEN may not have.
// It's recommended to configure GH_AW_AGENT_TOKEN or GH_AW_GITHUB_TOKEN with appropriate permissions.
func getEffectiveCopilotCodingAgentGitHubToken(customToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom agent GitHub token")
		return customToken
	}
	tokenLog.Print("Using default agent GitHub token fallback chain (GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN)")
	return "${{ secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
}

// getEffectiveProjectGitHubToken returns the GitHub token to use for GitHub Projects v2 operations,
// with precedence:
// 1. Custom token passed as parameter (e.g., from safe-outputs.update-project.github-token)
// 2. secrets.GH_AW_PROJECT_GITHUB_TOKEN (required token for Projects v2 operations)
// Note: GitHub Projects v2 requires a PAT (classic with project + repo scopes, or fine-grained
// with Projects: Read+Write) or GitHub App. The default GITHUB_TOKEN cannot access Projects v2.
// You must configure GH_AW_PROJECT_GITHUB_TOKEN or provide a custom token for Projects v2 operations.
// No fallback to GITHUB_TOKEN is provided as it will never work for Projects v2 operations.
func getEffectiveProjectGitHubToken(customToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom project GitHub token")
		return customToken
	}
	tokenLog.Print("Using GH_AW_PROJECT_GITHUB_TOKEN for project operations")
	return "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}"
}

// resolvePRCheckoutToken returns the token to use for PR checkout and git operations.
// Applies the following precedence (highest to lowest):
//  1. Per-config PAT: create-pull-request.github-token
//  2. Per-config PAT: push-to-pull-request-branch.github-token
//  3. Checkout-scoped safe-output GitHub App token (if configured for matching checkout)
//  4. safe-outputs GitHub App minted token (if a safe-outputs github-app is configured)
//  5. safe-outputs level PAT: safe-outputs.github-token
//  6. Default fallback via getEffectiveSafeOutputGitHubToken()
//
// Per-config tokens take precedence over the GitHub App so that individual operations
// can override the app-wide authentication with a dedicated PAT when needed.
//
// Returns:
//   - token: the effective GitHub Actions token expression to use for git operations
//   - isCustom: true when a custom non-default token was explicitly configured (per-config PAT, app, or safe-outputs PAT)
func resolvePRCheckoutToken(safeOutputs *SafeOutputsConfig, checkoutMgr *CheckoutManager) (token string, isCustom bool) {
	if safeOutputs == nil {
		return getEffectiveSafeOutputGitHubToken(""), false
	}

	var createPRToken string
	if safeOutputs.CreatePullRequests != nil {
		createPRToken = safeOutputs.CreatePullRequests.GitHubToken
	}
	var pushToPRBranchToken string
	if safeOutputs.PushToPullRequestBranch != nil {
		pushToPRBranchToken = safeOutputs.PushToPullRequestBranch.GitHubToken
	}

	// Per-config PAT tokens take highest precedence (overrides GitHub App).
	// head-github-token is intentionally excluded: it is a fork-write credential
	// scoped to head-repo only and must not be used for the shared checkout or
	// upstream API calls.
	perConfigToken := createPRToken
	if perConfigToken == "" {
		perConfigToken = pushToPRBranchToken
	}
	if perConfigToken != "" {
		return getEffectiveSafeOutputGitHubToken(perConfigToken), true
	}

	if checkoutMgr != nil {
		targetRepo := resolvePRCheckoutTargetRepo(safeOutputs)
		if checkoutToken, ok := checkoutMgr.ResolveSafeOutputCheckoutTokenExpression(targetRepo); ok {
			return checkoutToken, true
		}
	}

	// GitHub App token takes precedence over the safe-outputs level PAT
	if safeOutputs.GitHubApp != nil {
		if safeOutputs.GitHubApp.shouldIgnoreMissingKey() {
			return combineTokenExpressions(
				"${{ steps.safe-outputs-app-token.outputs.token }}",
				getEffectiveSafeOutputGitHubToken(safeOutputs.GitHubToken),
			), true
		}
		//nolint:gosec // G101: False positive - this is a GitHub Actions expression template placeholder, not a hardcoded credential
		return "${{ steps.safe-outputs-app-token.outputs.token }}", true
	}

	if safeOutputs.GitHubToken != "" {
		return getEffectiveSafeOutputGitHubToken(safeOutputs.GitHubToken), true
	}

	return getEffectiveSafeOutputGitHubToken(""), false
}

func resolvePRCheckoutTargetRepo(safeOutputs *SafeOutputsConfig) string {
	if safeOutputs == nil {
		return ""
	}
	if safeOutputs.CreatePullRequests != nil {
		if repo := strings.TrimSpace(safeOutputs.CreatePullRequests.HeadRepoSlug); repo != "" && repo != "*" {
			return repo
		}
		if repo := strings.TrimSpace(safeOutputs.CreatePullRequests.TargetRepoSlug); repo != "" && repo != "*" {
			return repo
		}
	}
	if safeOutputs.PushToPullRequestBranch != nil {
		if repo := strings.TrimSpace(safeOutputs.PushToPullRequestBranch.HeadRepoSlug); repo != "" && repo != "*" {
			return repo
		}
		if repo := strings.TrimSpace(safeOutputs.PushToPullRequestBranch.TargetRepoSlug); repo != "" && repo != "*" {
			return repo
		}
	}
	return ""
}

// resolveStaticCheckoutToken returns the effective checkout token as a static GitHub Actions
// expression (secret reference or default). Unlike resolvePRCheckoutToken, this function
// never returns a step-output expression because step outputs are not accessible outside the job
// they were created in.
//
// Token precedence:
//  1. checkout.github-token override
//  2. create-pull-request.github-token
//  3. push-to-pull-request-branch.github-token
//  4. safe-outputs.github-token
//  5. Default fallback (GH_AW_GITHUB_TOKEN || GITHUB_TOKEN)
func resolveStaticCheckoutToken(safeOutputs *SafeOutputsConfig, checkoutMgr *CheckoutManager) string {
	if checkoutMgr != nil {
		override := checkoutMgr.GetDefaultCheckoutOverride()
		if override != nil && override.token != "" {
			return getEffectiveSafeOutputGitHubToken(override.token)
		}
	}

	if safeOutputs == nil {
		return getEffectiveSafeOutputGitHubToken("")
	}

	if safeOutputs.CreatePullRequests != nil && safeOutputs.CreatePullRequests.HeadGitHubToken != "" {
		return getEffectiveSafeOutputGitHubToken(safeOutputs.CreatePullRequests.HeadGitHubToken)
	}
	if safeOutputs.CreatePullRequests != nil && safeOutputs.CreatePullRequests.GitHubToken != "" {
		return getEffectiveSafeOutputGitHubToken(safeOutputs.CreatePullRequests.GitHubToken)
	}
	if safeOutputs.PushToPullRequestBranch != nil && safeOutputs.PushToPullRequestBranch.HeadGitHubToken != "" {
		return getEffectiveSafeOutputGitHubToken(safeOutputs.PushToPullRequestBranch.HeadGitHubToken)
	}
	if safeOutputs.PushToPullRequestBranch != nil && safeOutputs.PushToPullRequestBranch.GitHubToken != "" {
		return getEffectiveSafeOutputGitHubToken(safeOutputs.PushToPullRequestBranch.GitHubToken)
	}
	if safeOutputs.GitHubToken != "" {
		return getEffectiveSafeOutputGitHubToken(safeOutputs.GitHubToken)
	}

	return getEffectiveSafeOutputGitHubToken("")
}

// resolveProjectToken resolves the project token using precedence:
//  1. Per-config token (e.g., update-project/create-project/create-project-status-update)
//  2. safe-outputs.github-token
//  3. GH_AW_PROJECT_GITHUB_TOKEN fallback via getEffectiveProjectGitHubToken()
func resolveProjectToken(perConfigToken string, safeOutputsToken string) string {
	token := perConfigToken
	if token == "" {
		token = safeOutputsToken
	}
	return getEffectiveProjectGitHubToken(token)
}

// resolveProjectURLAndToken resolves project URL/token from project-related safe output config.
// Priority: update-project > create-project-status-update > create-project.
func resolveProjectURLAndToken(safeOutputs *SafeOutputsConfig) (projectURL, projectToken string) {
	if safeOutputs == nil {
		return "", ""
	}

	safeOutputsToken := safeOutputs.GitHubToken

	if safeOutputs.UpdateProjects != nil && safeOutputs.UpdateProjects.Project != "" {
		projectURL = safeOutputs.UpdateProjects.Project
		projectToken = resolveProjectToken(safeOutputs.UpdateProjects.GitHubToken, safeOutputsToken)
		tokenLog.Printf("Setting GH_AW_PROJECT_URL from update-project config: %s", projectURL)
		tokenLog.Printf("Setting GH_AW_PROJECT_GITHUB_TOKEN from update-project config")
		return
	}

	if safeOutputs.CreateProjectStatusUpdates != nil && safeOutputs.CreateProjectStatusUpdates.Project != "" {
		projectURL = safeOutputs.CreateProjectStatusUpdates.Project
		projectToken = resolveProjectToken(safeOutputs.CreateProjectStatusUpdates.GitHubToken, safeOutputsToken)
		tokenLog.Printf("Setting GH_AW_PROJECT_URL from create-project-status-update config: %s", projectURL)
		tokenLog.Printf("Setting GH_AW_PROJECT_GITHUB_TOKEN from create-project-status-update config")
		return
	}

	if safeOutputs.CreateProjects != nil {
		projectToken = resolveProjectToken(safeOutputs.CreateProjects.GitHubToken, safeOutputsToken)
		tokenLog.Printf("Setting GH_AW_PROJECT_GITHUB_TOKEN from create-project config")
	}

	return
}

// getEffectiveCITriggerGitHubToken returns the GitHub token to use for CI trigger operations
// (pushing empty commits to trigger workflow runs), with precedence:
// 1. Custom token passed as parameter (e.g., from github-token-for-extra-empty-commit field)
// 2. secrets.GH_AW_CI_TRIGGER_TOKEN (recommended token for CI trigger operations)
// Note: The default GITHUB_TOKEN is NOT included as a fallback because events created with
// GITHUB_TOKEN do not trigger other workflow runs (GitHub Actions security feature).
// This is used when pushing an empty commit to trigger CI checks on PRs created by workflows.
func getEffectiveCITriggerGitHubToken(customToken string) string {
	if customToken != "" {
		tokenLog.Print("Using custom CI trigger GitHub token")
		return customToken
	}
	tokenLog.Print("Using GH_AW_CI_TRIGGER_TOKEN for CI trigger operations")
	return "${{ secrets.GH_AW_CI_TRIGGER_TOKEN }}"
}
