// Package workflow provides GitHub MCP server configuration and toolset management.
//
// # GitHub MCP Server Configuration
//
// This file manages the configuration of the GitHub MCP server, which provides
// AI agents with access to GitHub's API through the Model Context Protocol (MCP).
// It handles both local (Docker-based) and remote (hosted) deployment modes.
//
// Key responsibilities:
//   - Extracting GitHub tool configuration from workflow frontmatter
//   - Managing GitHub MCP server modes (local Docker vs remote hosted)
//   - Handling GitHub authentication tokens (custom, default, GitHub App)
//   - Managing read-only and lockdown security modes
//   - Expanding and managing GitHub toolsets (repos, issues, pull_requests, etc.)
//   - Handling allowed tool lists for fine-grained access control
//   - Determining Docker image versions for local mode
//   - Generating automatic lockdown detection steps
//   - Managing GitHub App token minting and invalidation
//
// GitHub MCP modes:
//   - Local (default): Runs GitHub MCP server in Docker container
//   - Remote: Uses hosted GitHub MCP service
//
// Security features:
//   - Read-only mode: Always enforced - write operations via GitHub MCP are not permitted
//   - GitHub lockdown mode: Restricts access to current repository only
//   - Automatic lockdown: Enables lockdown for public repositories with GH_AW_GITHUB_TOKEN
//   - Allowed tools: Restricts available GitHub API operations
//
// GitHub toolsets:
//   - default/action-friendly: Standard toolsets safe for GitHub Actions
//   - repos, issues, pull_requests, discussions, search, code_scanning
//   - secret_scanning, labels, releases, milestones, projects, gists
//   - teams, actions, packages (requires specific permissions)
//   - users (excluded from action-friendly due to token limitations)
//
// Token precedence:
//  1. GitHub App token (minted from app configuration)
//  2. Custom github-token from tool configuration
//  3. Top-level github-token from frontmatter
//  4. Default GITHUB_TOKEN secret
//
// Automatic lockdown detection:
// When lockdown is not explicitly set, a step is generated to automatically
// enable lockdown for public repositories ONLY when GH_AW_GITHUB_TOKEN is configured.
//
// Related files:
//   - mcp_renderer.go: Renders GitHub MCP configuration to YAML
//   - mcp_environment.go: Manages GitHub MCP environment variables
//   - mcp_setup_generator.go: Generates GitHub MCP setup steps
//   - safe_outputs_app.go: GitHub App token minting helpers
//
// Example configuration:
//
//	tools:
//	  github:
//	    mode: remote                    # or "local" for Docker
//	    github-token: ${{ secrets.PAT }}
//	    lockdown: true                  # or omit for automatic detection
//	    toolsets: [repos, issues, pull_requests]
//	    allowed: [get_repo, list_issues, get_pull_request]
package workflow

import (
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/typeutil"
)

var githubConfigLog = logger.New("workflow:mcp_github_config")

func dynamicEnclaveGitHubGuardRepos(workflowData *WorkflowData) []string {
	enclave := enclaveDynamicRepositoryPolicyConfig(workflowData)
	if enclave == nil || enclave.Dynamic == nil {
		return nil
	}
	seen := make(map[string]struct{})
	repos := make([]string, 0, len(enclave.Dynamic.AllowedRepositories)+len(enclave.Dynamic.AllowedOwners))
	add := func(repo string) {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return
		}
		if _, ok := seen[repo]; ok {
			return
		}
		seen[repo] = struct{}{}
		repos = append(repos, repo)
	}
	for _, repo := range enclave.Dynamic.AllowedRepositories {
		add(repo)
	}
	for _, owner := range enclave.Dynamic.AllowedOwners {
		add(owner + "/*") //nolint:manualpathconcat // GitHub owner/repo guard patterns are not filesystem paths.
	}
	sort.Strings(repos)
	return repos
}

func dynamicEnclaveGitHubGuardPolicies(workflowData *WorkflowData) map[string]any {
	repos := dynamicEnclaveGitHubGuardRepos(workflowData)
	if len(repos) == 0 {
		return nil
	}
	return map[string]any{
		"allow-only": map[string]any{
			"min-integrity": GitHubIntegrityApproved,
			"repos":         repos,
		},
	}
}

// staticEnclaveGitHubGuardPolicies builds a server-level guard policy for a GitHub MCP server
// that exists only to serve a static enclave agent identity. It mirrors the enclave identity
// policy exactly, so the server-level guard never broadens access beyond the identity policy
// already enforced by the gateway.
func staticEnclaveGitHubGuardPolicies(workflowData *WorkflowData) map[string]any {
	if enclaveStaticGitHubAgentConfig(workflowData) == nil {
		return nil
	}
	allowOnly := enclaveGitHubMCPAgentPolicy(workflowData).AllowOnly
	repos, _ := allowOnly["repos"].([]string)
	if len(repos) == 0 {
		return nil
	}
	policy := make(map[string]any, len(allowOnly))
	maps.Copy(policy, allowOnly)
	return map[string]any{"allow-only": policy}
}

// githubBackendIsStaticEnclaveDelegationOnly reports whether the GitHub MCP server is rendered
// solely to serve a static enclave agent identity (primary-agent GitHub access disabled).
func githubBackendIsStaticEnclaveDelegationOnly(workflowData *WorkflowData) bool {
	return enclaveGitHubIssuesEnabled(workflowData) && !primaryGitHubMCPEnabled(workflowData)
}

func githubGuardPoliciesFromStep(workflowData *WorkflowData, explicitGuardPolicies map[string]any) bool {
	if len(explicitGuardPolicies) > 0 {
		return false
	}
	if workflowData == nil {
		return true
	}
	githubTool, hasGitHub := workflowData.Tools["github"]
	if !hasGitHub {
		// Default-tool resolution removes the "github" key when tools.github is false, so an
		// absent key can still mean the GitHub MCP server is rendered for enclave delegation.
		// Only reference the lockdown step outputs when that step is actually generated.
		return githubLockdownDetectionStepEnabled(workflowData)
	}
	return githubTool != false
}

// dynamicEnclaveWriteSinkGuardPolicy builds a write-sink guard policy for data returned
// by a dynamic enclave. The destination visibility is resolved at runtime from the
// workflow repository; the dynamic sensitivity limits the accepted source secrecy labels.
func dynamicEnclaveWriteSinkGuardPolicy(workflowData *WorkflowData) map[string]any {
	enclave := enclaveDynamicRepositoryPolicyConfig(workflowData)
	if enclave == nil || enclave.Dynamic == nil {
		return nil
	}
	accept := []string{"*"}
	if enclave.Dynamic.Sensitivity != "public" {
		repos := dynamicEnclaveGitHubGuardRepos(workflowData)
		accept = make([]string, 0, len(repos))
		for _, repo := range repos {
			accept = append(accept, transformRepoPattern(repo))
		}
	}
	return map[string]any{
		"write-sink": map[string]any{
			"accept":          accept,
			"sink-visibility": sinkVisibilityRuntimeExpr,
		},
	}
}

// hasGitHubTool checks if the GitHub tool is configured (using ParsedTools)
func hasGitHubTool(parsedTools *Tools) bool {
	if parsedTools == nil {
		return false
	}
	return parsedTools.GitHub != nil
}

// hasGitHubApp checks if a GitHub App is configured in the (merged) GitHub tool configuration
func hasGitHubApp(githubTool map[string]any) bool {
	_, hasApp := githubTool["github-app"]
	return hasApp
}

// isGitHubCLIModeEnabled returns true when GitHub prompt/runtime mode is explicitly set
// to `tools.github.mode: gh-proxy`. If mode is explicitly set to `local` or `remote`, it
// takes precedence over the legacy features.cli-proxy flag (treated as MCP mode).
// When mode is not explicitly set, this returns the legacy `features.cli-proxy` flag
// value for backward compatibility.
func isGitHubCLIModeEnabled(data *WorkflowData) bool {
	if data == nil {
		return false
	}
	githubTool, hasGitHub := data.Tools["github"]
	if hasGitHub && githubTool == false {
		return false
	}
	if hasGitHub {
		if toolConfig, ok := githubTool.(map[string]any); ok {
			if modeSetting, exists := toolConfig["mode"]; exists {
				if stringValue, ok := modeSetting.(string); ok {
					switch GitHubMCPMode(strings.ToLower(strings.TrimSpace(stringValue))) {
					case GitHubMCPModeGHProxy, GitHubMCPModeCLI:
						return true
					case GitHubMCPModeLocal, GitHubMCPModeRemote:
						return false
					default:
						githubConfigLog.Printf("Unrecognized tools.github.mode value: %s, falling back to legacy behavior", stringValue)
					}
				}
			}
		}
	}
	return isFeatureEnabled(constants.CliProxyFeatureFlag, data)
}

// normalizeGitHubType normalizes and validates GitHub MCP transport values.
// Supported values are `local` and `remote`.
func normalizeGitHubType(value string) (GitHubMCPMode, bool) {
	normalizedValue := GitHubMCPMode(strings.ToLower(strings.TrimSpace(value)))
	switch normalizedValue {
	case GitHubMCPModeLocal, GitHubMCPModeRemote:
		return normalizedValue, true
	default:
		return "", false
	}
}

// getGitHubType extracts the MCP transport type from GitHub tool configuration
// (local or remote). Supports both `type` (preferred) and legacy `mode` values.
func getGitHubType(githubTool map[string]any) GitHubMCPMode {
	if typeSetting, exists := githubTool["type"]; exists {
		if stringValue, ok := typeSetting.(string); ok {
			if normalizedValue, valid := normalizeGitHubType(stringValue); valid {
				githubConfigLog.Printf("GitHub MCP type set explicitly: %s", normalizedValue)
				return normalizedValue
			}
			githubConfigLog.Printf("Unrecognized tools.github.type value: %q, falling back to default", stringValue)
		}
	}
	if modeSetting, exists := githubTool["mode"]; exists {
		if stringValue, ok := modeSetting.(string); ok {
			if normalizedValue, valid := normalizeGitHubType(stringValue); valid {
				githubConfigLog.Printf("GitHub MCP type read from legacy mode field: %s", normalizedValue)
				return normalizedValue
			}
		}
	}
	githubConfigLog.Print("GitHub MCP mode: local (default)")
	return GitHubMCPModeLocal // default to local (Docker)
}

// getGitHubToken extracts the custom github-token from GitHub tool configuration
func getGitHubToken(githubTool map[string]any) string {
	if tokenSetting, exists := githubTool["github-token"]; exists {
		if stringValue, ok := tokenSetting.(string); ok {
			return stringValue
		}
	}
	return ""
}

// getGitHubReadOnly returns true always, since the GitHub MCP server is always read-only.
// Setting read-only: false is not supported and will be flagged as a validation error.
func getGitHubReadOnly() bool {
	return true
}

// getGitHubLockdown checks if lockdown mode is enabled for GitHub tool
// Defaults to constants.DefaultGitHubLockdown (false)
func getGitHubLockdown(githubTool map[string]any) bool {
	if lockdownSetting, exists := githubTool["lockdown"]; exists {
		if boolValue, ok := lockdownSetting.(bool); ok {
			return boolValue
		}
	}
	return constants.DefaultGitHubLockdown
}

// hasGitHubLockdownExplicitlySet checks if lockdown field is explicitly set in GitHub tool config
func hasGitHubLockdownExplicitlySet(githubTool map[string]any) bool {
	_, exists := githubTool["lockdown"]
	return exists
}

// getGitHubToolsets extracts the toolsets configuration from GitHub tool
// Expands "default" to individual toolsets for action-friendly compatibility
func getGitHubToolsets(githubTool map[string]any) string {
	if toolsetsSetting, exists := githubTool["toolsets"]; exists {
		// Handle array format only
		if toolsets := parseStringSliceAny(toolsetsSetting, githubConfigLog); toolsets != nil {
			toolsetsStr := strings.Join(toolsets, ",")
			// Expand "default" to individual toolsets for action-friendly compatibility
			resolved := expandDefaultToolset(toolsetsStr)
			githubConfigLog.Printf("GitHub MCP toolsets resolved: %s", resolved)
			return resolved
		}
	}
	// default to action-friendly toolsets (excludes "users" which GitHub Actions tokens don't support)
	githubConfigLog.Print("GitHub MCP toolsets: using default action-friendly toolsets")
	return strings.Join(ActionFriendlyGitHubToolsets, ",")
}

// expandDefaultToolset expands "default" and "action-friendly" keywords to individual toolsets.
// This ensures that "default" and "action-friendly" in the source expand to action-friendly toolsets
// (excluding "users" which GitHub Actions tokens don't support).
func expandDefaultToolset(toolsetsStr string) string {
	if toolsetsStr == "" {
		return strings.Join(ActionFriendlyGitHubToolsets, ",")
	}

	// Split by comma and check if "default" or "action-friendly" is present
	toolsets := strings.Split(toolsetsStr, ",")
	var result []string
	seenToolsets := make(map[string]struct {
	})

	for _, toolset := range toolsets {
		toolset = strings.TrimSpace(toolset)
		if toolset == "" {
			continue
		}

		if toolset == "default" || toolset == "action-friendly" {
			githubConfigLog.Printf("Expanding %q keyword to action-friendly toolsets", toolset)
			// Expand "default" or "action-friendly" to action-friendly toolsets (excludes "users")
			for _, dt := range ActionFriendlyGitHubToolsets {
				if !setutil.Contains(seenToolsets, dt) {
					result = append(result, dt)
					seenToolsets[dt] = struct {
					}{}
				}
			}
		} else {
			// Keep other toolsets as-is (including "all", individual toolsets, etc.)
			if !setutil.Contains(seenToolsets, toolset) {
				result = append(result, toolset)
				seenToolsets[toolset] = struct {
				}{}
			}
		}
	}

	return strings.Join(result, ",")
}

// getGitHubAllowedTools extracts the allowed tools list from GitHub tool configuration
// Returns the list of allowed tools, or nil if no allowed list is specified (which means all tools are allowed)
func getGitHubAllowedTools(githubTool map[string]any) []string {
	if allowedSetting, exists := githubTool["allowed"]; exists {
		allowedTools, _ := parseGitHubAllowedToolsAndLimits(allowedSetting)
		return allowedTools
	}
	return nil
}

// parseGitHubAllowedToolsAndLimits parses tools.github.allowed entries.
// Supports string entries and object entries:
// {name: "tool_name", max-calls: 1}.
func parseGitHubAllowedToolsAndLimits(allowedSetting any) ([]string, map[string]int) {
	allowedItems, ok := allowedSetting.([]any)
	if !ok {
		return parseStringSliceAny(allowedSetting, nil), nil
	}

	allowedTools := make([]string, 0, len(allowedItems))
	toolCallLimits := make(map[string]int)

	for _, item := range allowedItems {
		switch entry := item.(type) {
		case string:
			toolName := strings.TrimSpace(entry)
			if toolName == "" {
				continue
			}
			allowedTools = append(allowedTools, toolName)
		case map[string]any:
			toolName, ok := entry["name"].(string)
			toolName = strings.TrimSpace(toolName)
			if !ok || toolName == "" {
				continue
			}
			allowedTools = append(allowedTools, toolName)
			if maxCalls, hasMax := entry["max-calls"]; hasMax {
				if max, ok := typeutil.ParseIntValue(maxCalls); ok && max > 0 {
					toolCallLimits[toolName] = max
				}
			}
		}
	}

	if len(toolCallLimits) == 0 {
		return allowedTools, nil
	}
	return allowedTools, toolCallLimits
}

// getGitHubGuardPolicies extracts guard policies from GitHub tool configuration.
// It reads the flat allowed-repos/repos/min-integrity/blocked-users/trusted-users/approval-labels fields
// and wraps them for MCP gateway rendering.
// When min-integrity is set but allowed-repos is not, repos defaults to "all" because the MCP
// Gateway requires repos to be present in the allow-only policy.
// Note: repos-only (without min-integrity) is rejected earlier by validateGitHubGuardPolicy,
// so this function will never be called with repos but without min-integrity in practice.
// When blocked-users, trusted-users, or approval-labels are set, their values are unioned with
// the org/repo variable fallback expressions so that a centrally-configured variable extends the
// per-workflow list rather than replacing it.
// Returns nil if no guard policies are configured.
func getGitHubGuardPolicies(githubTool map[string]any) map[string]any {
	var toolCallLimits map[string]int
	if allowedSetting, exists := githubTool["allowed"]; exists {
		_, toolCallLimits = parseGitHubAllowedToolsAndLimits(allowedSetting)
	}
	hasToolCallLimits := len(toolCallLimits) > 0

	// Support both 'allowed-repos' (preferred) and deprecated 'repos'
	repos, hasRepos := githubTool["allowed-repos"]
	if !hasRepos {
		repos, hasRepos = githubTool["repos"]
	}
	integrity, hasIntegrity := githubTool["min-integrity"]
	if hasRepos || hasIntegrity || hasToolCallLimits {
		policy := map[string]any{}
		if hasRepos {
			policy["repos"] = normalizeGitHubRepositoryInReposScope(repos)
		} else {
			// Default repos to "all" when min-integrity is specified without repos.
			// The MCP Gateway requires repos in the allow-only policy.
			policy["repos"] = "all"
		}
		if hasIntegrity {
			policy["min-integrity"] = integrity
		}
		if hasToolCallLimits {
			policy["tool-call-limits"] = toolCallLimits
		}
		// blocked-users, trusted-users, and approval-labels are parsed at runtime by the
		// parse-guard-vars step. The step outputs proper JSON arrays (split on comma/newline,
		// validated, jq-encoded) from both the compile-time static values and the
		// GH_AW_GITHUB_* org/repo variables.
		policy["blocked-users"] = guardExprSentinel + "${{ steps.parse-guard-vars.outputs.blocked_users }}"
		policy["trusted-users"] = guardExprSentinel + "${{ steps.parse-guard-vars.outputs.trusted_users }}"
		policy["approval-labels"] = guardExprSentinel + "${{ steps.parse-guard-vars.outputs.approval_labels }}"
		return map[string]any{
			"allow-only": policy,
		}
	}
	return nil
}

// DefaultEndorsementReactions are the default endorsement reactions injected when the
// integrity-reactions feature flag is enabled but no explicit endorsement-reactions are set.
var DefaultEndorsementReactions = []string{"THUMBS_UP", "HEART"}

// DefaultDisapprovalReactions are the default disapproval reactions injected when the
// integrity-reactions feature flag is enabled but no explicit disapproval-reactions are set.
var DefaultDisapprovalReactions = []string{"THUMBS_DOWN", "CONFUSED"}

// hasReactionFieldsInToolConfig returns true if any reaction-based integrity fields are
// explicitly set in the raw tool configuration map.
func hasReactionFieldsInToolConfig(toolConfig map[string]any) bool {
	_, hasEndorsement := toolConfig["endorsement-reactions"]
	_, hasDisapproval := toolConfig["disapproval-reactions"]
	_, hasDisapprovalIntegrity := toolConfig["disapproval-integrity"]
	_, hasEndorserMin := toolConfig["endorser-min-integrity"]
	return hasEndorsement || hasDisapproval || hasDisapprovalIntegrity || hasEndorserMin
}

// injectIntegrityReactionFields adds endorsement-reactions, disapproval-reactions,
// disapproval-integrity, and endorser-min-integrity into an existing allow-only policy
// map when the integrity-reactions feature flag is enabled and the MCPG version supports it.
//
// This function is used exclusively for proxy mode (DIFC proxy / CLI proxy). Reaction-based
// integrity is not supported in MCP gateway mode because the GitHub MCP server protocol does
// not expose reaction author information, which is required for integrity decisions.
//
//   - policy is the inner allow-only map (not the outer allow-only wrapper).
//   - toolConfig is the raw github tool configuration map.
//   - data contains workflow data including feature flags used to check if integrity-reactions is enabled.
//   - gatewayConfig contains MCP gateway version configuration used to version-gate the injection.
//
// When the feature flag is enabled and endorsement-reactions or disapproval-reactions are
// not explicitly set in toolConfig, sensible defaults are injected:
//   - endorsement-reactions: ["THUMBS_UP", "HEART"]
//   - disapproval-reactions: ["THUMBS_DOWN", "CONFUSED"]
//
// No-op when the feature flag is disabled or the MCPG version is too old.
func injectIntegrityReactionFields(policy map[string]any, toolConfig map[string]any, data *WorkflowData, gatewayConfig *MCPGatewayRuntimeConfig) {
	if !isFeatureEnabled(constants.IntegrityReactionsFeatureFlag, data) {
		return
	}
	if !mcpgSupportsIntegrityReactions(gatewayConfig) {
		return
	}
	if endorsement, ok := toolConfig["endorsement-reactions"]; ok {
		policy["endorsement-reactions"] = endorsement
	} else {
		policy["endorsement-reactions"] = DefaultEndorsementReactions
	}
	if disapproval, ok := toolConfig["disapproval-reactions"]; ok {
		policy["disapproval-reactions"] = disapproval
	} else {
		policy["disapproval-reactions"] = DefaultDisapprovalReactions
	}
	if disapprovalIntegrity, ok := toolConfig["disapproval-integrity"]; ok {
		policy["disapproval-integrity"] = disapprovalIntegrity
	}
	if endorserMinIntegrity, ok := toolConfig["endorser-min-integrity"]; ok {
		policy["endorser-min-integrity"] = endorserMinIntegrity
	}
}

// mcpgSupportsIntegrityReactions returns true when the effective MCPG version supports
// endorsement-reactions and disapproval-reactions in the allow-only policy (>= v0.2.18).
//
// Special cases:
//   - gatewayConfig is nil or has no Version: use DefaultMCPGatewayVersion for comparison.
//   - "latest": always returns true (latest is always a new release).
//   - Any semver string >= MCPGIntegrityReactionsMinVersion: returns true.
//   - Any semver string < MCPGIntegrityReactionsMinVersion: returns false.
//   - Non-semver string (e.g. a branch name): returns false (conservative).
func mcpgSupportsIntegrityReactions(gatewayConfig *MCPGatewayRuntimeConfig) bool {
	var version string
	if gatewayConfig != nil && gatewayConfig.Version != "" {
		version = gatewayConfig.Version
	}
	return versionAtLeast(
		version,
		string(constants.DefaultMCPGatewayVersion),
		string(constants.MCPGIntegrityReactionsMinVersion),
	)
}

// deriveSafeOutputsGuardPolicyFromGitHub generates a safeoutputs guard-policy from GitHub guard-policy.
// When the GitHub MCP server has a guard-policy with repos, the safeoutputs MCP must also have
// a linked guard-policy with accept field derived from repos according to these rules:
//
// Rules by repos value:
//   - repos="all" or repos="public": accept=["*"] (allow all safe output operations)
//   - repos=["O/*"]: accept=["private:O"] (owner wildcard → strip wildcard)
//   - repos=["O/P*"]: accept=["private:O/P*"] (prefix wildcard → keep as-is)
//   - repos=["O/R"]: accept=["private:O/R"] (specific repo → keep as-is)
//
// This allows the gateway to read data from the GitHub MCP server and still write to safeoutputs.
// sink-visibility is emitted as a runtime expression so that the write-sink guard can enforce
// public/private/internal semantics based on the actual repository visibility at workflow execution time.
// Returns nil if no GitHub guard policies are configured.
func deriveSafeOutputsGuardPolicyFromGitHub(githubTool map[string]any) map[string]any {
	githubPolicies := getGitHubGuardPolicies(githubTool)
	if githubPolicies == nil {
		return nil
	}

	// Extract the allow-only policy from GitHub guard policies
	allowOnly, ok := githubPolicies["allow-only"].(map[string]any)
	if !ok || allowOnly == nil {
		return nil
	}

	// Extract repos from the allow-only policy
	repos, hasRepos := allowOnly["repos"]
	if !hasRepos {
		return nil
	}

	// Convert repos to accept list according to the specification
	var acceptList []string

	switch r := repos.(type) {
	case string:
		// Single string value (e.g., "all", "public", or a pattern)
		switch r {
		case "all", "public":
			// For "all" or "public", accept all safe output operations
			acceptList = []string{"*"}
		default:
			// Single pattern - transform according to rules
			acceptList = []string{transformRepoPattern(r)}
		}
	case []any:
		// Array of patterns
		acceptList = make([]string, 0, len(r))
		for _, item := range r {
			if pattern, ok := item.(string); ok {
				acceptList = append(acceptList, transformRepoPattern(pattern))
			}
		}
	case []string:
		// Array of patterns (already strings)
		acceptList = make([]string, 0, len(r))
		for _, pattern := range r {
			acceptList = append(acceptList, transformRepoPattern(pattern))
		}
	default:
		// Unknown type, return nil
		githubConfigLog.Printf("Unknown repos type in guard-policy: %T", repos)
		return nil
	}

	writeSink := map[string]any{
		"accept":          acceptList,
		"sink-visibility": sinkVisibilityRuntimeExpr,
	}

	// Build the write-sink policy for safeoutputs
	return map[string]any{
		"write-sink": writeSink,
	}
}

// transformRepoPattern transforms a repos pattern to the corresponding accept pattern.
// Rules:
//   - "O/*"  → "private:O" (owner wildcard → strip wildcard)
//   - "O/P*" → "private:O/P*" (prefix wildcard → keep as-is)
//   - "O/R"  → "private:O/R" (specific repo → keep as-is)
func transformRepoPattern(pattern string) string {
	// Check if pattern ends with "/*" (owner wildcard)
	if owner, found := strings.CutSuffix(pattern, "/*"); found {
		// Strip the wildcard: "owner/*" → "private:owner"
		return "private:" + owner
	}
	// All other patterns (including "O/P*" prefix wildcards): add "private:" prefix
	return "private:" + pattern
}

// deriveWriteSinkGuardPolicyFromWorkflow derives a write-sink guard policy for non-GitHub MCP servers
// from the workflow's GitHub guard-policy configuration. This uses the same derivation as
// deriveSafeOutputsGuardPolicyFromGitHub, ensuring that as guard policies are rolled out, only
// GitHub inputs are filtered while outputs to non-GitHub servers are not restricted.
//
// Two cases produce a non-nil policy:
//  1. Explicit guard policy — when repos/min-integrity are set on the GitHub tool, a write-sink
//     policy is derived from those settings (e.g. "private:myorg/myrepo").
//  2. Auto-lockdown — when the GitHub tool is present without explicit guard policies,
//     auto-lockdown detection will set repos=all at runtime, so a
//     write-sink policy with accept=["*"] is returned to match that runtime behaviour.
//
// When private-to-public-flows: allow is declared, sink-visibility is omitted from the returned
// policy per MCP Gateway Specification Section 10.9.3: the blanket allow disables both
// forcePublicRepos and sink-visibility enforcement.
//
// Returns nil when workflowData is nil, or when neither the GitHub tool nor a dynamic repository enclave is present.
func deriveWriteSinkGuardPolicyFromWorkflow(workflowData *WorkflowData) map[string]any {
	if workflowData == nil || workflowData.Tools == nil {
		return dynamicEnclaveWriteSinkGuardPolicy(workflowData)
	}
	rawGithubTool, hasGitHub := workflowData.Tools["github"]
	if !hasGitHub {
		return dynamicEnclaveWriteSinkGuardPolicy(workflowData)
	}

	toolConfig, _ := rawGithubTool.(map[string]any)

	// Detect blanket opt-out: private-to-public-flows: allow.
	// Per Section 10.9.3, allow disables sink-visibility enforcement in addition to forcePublicRepos.
	blanketAllow := workflowData.ParsedTools != nil &&
		workflowData.ParsedTools.GitHub != nil &&
		workflowData.ParsedTools.GitHub.PrivateToPublicFlows == "allow"

	// Try to derive from explicit guard policy first
	policy := deriveSafeOutputsGuardPolicyFromGitHub(toolConfig)
	if policy != nil {
		if blanketAllow {
			// Strip sink-visibility from write-sink when blanket allow is declared.
			if writeSink, ok := policy["write-sink"].(map[string]any); ok {
				delete(writeSink, "sink-visibility")
			}
		}
		return policy
	}

	// When no explicit guard policy is configured but automatic lockdown detection would run
	// (GitHub tool present and not disabled), return accept=["*"] because automatic lockdown
	// always sets repos=all at runtime. GitHub App token scope is authentication, not a
	// substitute for the DIFC sink labels enforced by the MCP gateway.
	// sink-visibility is set as a runtime expression so that the write-sink guard can enforce
	// public/private/internal semantics based on the actual repository visibility at workflow execution time.
	if rawGithubTool != false && len(getGitHubGuardPolicies(toolConfig)) == 0 {
		writeSink := map[string]any{
			"accept": []string{"*"},
		}
		if !blanketAllow {
			writeSink["sink-visibility"] = sinkVisibilityRuntimeExpr
		}
		return map[string]any{
			"write-sink": writeSink,
		}
	}

	if rawGithubTool == false {
		return dynamicEnclaveWriteSinkGuardPolicy(workflowData)
	}

	return nil
}

func getGitHubDockerImageVersion(githubTool map[string]any) string {
	githubDockerImageVersion := string(constants.DefaultGitHubMCPServerVersion) // Default Docker image version
	// Extract version setting from tool properties
	if versionSetting, exists := githubTool["version"]; exists {
		// Handle different version types
		switch v := versionSetting.(type) {
		case string:
			githubDockerImageVersion = v
		case int:
			githubDockerImageVersion = strconv.Itoa(v)
		case int64:
			githubDockerImageVersion = strconv.FormatInt(v, 10)
		case uint64:
			githubDockerImageVersion = strconv.FormatUint(v, 10)
		case float64:
			// Use %g to avoid trailing zeros and scientific notation for simple numbers
			githubDockerImageVersion = fmt.Sprintf("%g", v)
		}
	}
	githubConfigLog.Printf("GitHub MCP Docker image version: %s", githubDockerImageVersion)
	return githubDockerImageVersion
}

// getGitHubFeatures returns the comma-separated feature flag string for the GitHub MCP server.
// When "features" is explicitly set in tools.github, that value is returned as-is.
// Otherwise, "fields_param" is enabled by default when the effective server version is v1.6.0
// or later, because that version introduced the optional fields-filtering parameter for
// list/search tools (search_code, list_pull_requests, search_issues, etc.).
// As of v1.8.0, the fields parameter is available by default without the feature flag;
// the flag is still emitted for v1.6.0–v1.7.x compatibility and is a no-op on v1.8.0+.
// Enabling fields_param reduces token usage by letting agents request only the fields they need.
func getGitHubFeatures(githubTool map[string]any) string {
	// Respect an explicit user-supplied features override.
	// An explicit empty string disables all feature flags; any other string is forwarded as-is.
	if featuresRaw, exists := githubTool["features"]; exists {
		if features, ok := featuresRaw.(string); ok {
			githubConfigLog.Printf("GitHub MCP features (explicit): %q", features)
			return features
		}
	}

	// Default: enable fields_param when the server version supports it (v1.6.0+).
	// On v1.8.0+ the fields parameter is available by default, so the flag is a no-op.
	version := getGitHubDockerImageVersion(githubTool)
	if versionAtLeast(version, string(constants.DefaultGitHubMCPServerVersion), "v1.6.0") {
		githubConfigLog.Printf("GitHub MCP features (default v1.6.0+): fields_param")
		return GitHubMCPFeatureFieldsParam
	}

	githubConfigLog.Printf("GitHub MCP features: none (version %s < v1.6.0)", version)
	return ""
}
