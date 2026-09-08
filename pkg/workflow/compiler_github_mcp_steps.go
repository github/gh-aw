package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

// generateGitHubMCPLockdownDetectionStep generates a step to determine the repository visibility
// and automatic guard policy for the GitHub MCP server.
// This step is added when GitHub tool is enabled. It always runs to:
//   - Output the repository visibility (used as sink-visibility in the mcpg config at runtime)
//   - Auto-configure guard policies (min-integrity, repos) when not explicitly set in the workflow
//
// For public repositories, the step automatically sets min-integrity to "approved" and
// repos to "all" if they are not already configured.
// This applies regardless of whether a GitHub App token is configured, because repo-scoping
// is not a substitute for author-integrity filtering inside a repository.
func (c *Compiler) generateGitHubMCPLockdownDetectionStep(yaml *strings.Builder, data *WorkflowData) {
	// Dynamic enclaves also need the workflow repository's visibility for Safe Outputs.
	githubTool, hasGitHub := data.Tools["github"]
	if (!hasGitHub || githubTool == false) && !enclaveDynamicRepositoryPolicyEnabled(data) {
		githubConfigLog.Print("Skipping GitHub MCP lockdown detection step: GitHub tool not enabled")
		return
	}

	// NOTE: Do NOT skip this step when guard policies are explicitly configured.
	// Even when min-integrity/repos are hardcoded, the step must still run to output
	// the repository visibility via steps.determine-automatic-lockdown.outputs.visibility,
	// which is referenced as sink-visibility in safe-outputs and other MCP server guard
	// policies. Removing the step while leaving those references in place breaks workflows
	// at runtime with undefined step output errors.
	githubConfigLog.Print("Generating automatic guard policy determination step for GitHub MCP server")

	// Resolve the latest version of actions/github-script
	actionRepo := "actions/github-script"
	actionVersion := string(constants.DefaultGitHubScriptVersion)
	pinnedAction, err := getActionPinWithData(actionRepo, actionVersion, data)
	if err != nil {
		githubConfigLog.Printf("Failed to resolve %s@%s: %v", actionRepo, actionVersion, err)
		// In strict mode, this error would have been returned by getActionPinWithData
		// In normal mode, we fall back to using the version tag without pinning
		pinnedAction = fmt.Sprintf("%s@%s", actionRepo, actionVersion)
	}

	// Extract current guard policy configuration to pass as env vars so the step can
	// detect whether each field is already configured and avoid overriding it.
	configuredMinIntegrity := ""
	configuredRepos := ""
	privateToPublicFlowsAllow := false
	if toolConfig, ok := githubTool.(map[string]any); ok {
		if v, exists := toolConfig["min-integrity"]; exists {
			configuredMinIntegrity = serializeEnvStringValue(v)
		}
		// Support both 'allowed-repos' (preferred) and deprecated 'repos'
		if v, exists := toolConfig["allowed-repos"]; exists {
			configuredRepos = serializeEnvStringValue(v)
		} else if v, exists := toolConfig["repos"]; exists {
			configuredRepos = serializeEnvStringValue(v)
		}
		// Detect private-to-public-flows: allow to inform the default repos value.
		// When set to "allow", the user has explicitly opted in to cross-visibility data
		// flows, so the repos default should be "all" rather than "public" even for
		// public repositories.
		if ptpFlows, _ := toolConfig["private-to-public-flows"].(string); ptpFlows == "allow" {
			privateToPublicFlowsAllow = true
		}
	}

	// Generate the step using the determine_automatic_lockdown.cjs action
	yaml.WriteString("      - name: Determine automatic lockdown mode for GitHub MCP Server\n")
	yaml.WriteString("        id: determine-automatic-lockdown\n")
	fmt.Fprintf(yaml, "        uses: %s\n", pinnedAction)
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}\n")
	yaml.WriteString("          GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}\n")
	if configuredMinIntegrity != "" {
		fmt.Fprintf(yaml, "          GH_AW_GITHUB_MIN_INTEGRITY: %s\n", quoteYAMLEnvValue(configuredMinIntegrity))
	}
	if configuredRepos != "" {
		fmt.Fprintf(yaml, "          GH_AW_GITHUB_REPOS: %s\n", quoteYAMLEnvValue(configuredRepos))
	}
	if privateToPublicFlowsAllow {
		yaml.WriteString("          GH_AW_PRIVATE_TO_PUBLIC_FLOWS: " + quoteYAMLEnvValue("allow") + "\n")
	}
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const determineAutomaticLockdown = require('${{ runner.temp }}/gh-aw/actions/determine_automatic_lockdown.cjs');\n")
	yaml.WriteString("            await determineAutomaticLockdown(github, context, core);\n")
}

// serializeEnvStringValue converts a workflow config value to a string suitable for a
// YAML env variable. Strings are returned as-is; slices are JSON-encoded so that
// e.g. ["github/gh-aw", "github/*"] becomes `["github/gh-aw","github/*"]`.
func serializeEnvStringValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(encoded)
	}
}

// quoteYAMLEnvValue wraps a string in single quotes for safe use as a YAML scalar env value.
// This prevents YAML from misinterpreting values that look like sequences (e.g. JSON arrays).
// Single quotes in the value are escaped by doubling them per YAML 1.2 spec.
func quoteYAMLEnvValue(s string) string {
	// Escape embedded single quotes by doubling them
	escaped := strings.ReplaceAll(s, "'", "''")
	return "'" + escaped + "'"
}

// generateGitHubMCPAppTokenMintingSteps returns the YAML steps to mint a GitHub App token
// for the GitHub MCP server. The steps are generated with id: github-mcp-app-token and
// permissions derived from the agent job's declared permissions plus any extra permissions
// configured under tools.github.github-app.permissions.
//
// The returned steps are added directly to the agent job so that the minted token is
// available as steps.github-mcp-app-token.outputs.token within that job.
// Minting happens inside the agent job (not the activation job) because
// actions/create-github-app-token calls ::add-mask:: on the produced token, and the
// GitHub Actions runner silently drops masked values when used as job outputs (runner v2.308+).
func (c *Compiler) generateGitHubMCPAppTokenMintingSteps(data *WorkflowData) []string {
	// Check if GitHub tool has app configuration
	if data.ParsedTools == nil || data.ParsedTools.GitHub == nil || data.ParsedTools.GitHub.GitHubApp == nil {
		githubConfigLog.Print("Skipping GitHub MCP app token minting: no github-app configuration on GitHub tool")
		return nil
	}

	app := data.ParsedTools.GitHub.GitHubApp
	githubConfigLog.Printf("Generating GitHub App token minting step for GitHub MCP server: client-id=%s", app.AppID)

	// Get permissions from the agent job - use cached permissions when available to avoid YAML re-parsing.
	// We must clone CachedPermissions before applying app-specific overrides via permissions.Set() below,
	// because Set() mutates the object in place and we must not corrupt the shared cached value.
	var permissions *Permissions
	if data.CachedPermissions != nil {
		permissions = data.CachedPermissions.Clone()
	} else if data.Permissions != "" {
		permissions = NewPermissionsParser(data.Permissions).ToPermissions()
	} else {
		githubConfigLog.Print("No permissions specified, using empty permissions")
		permissions = NewPermissions()
	}

	// Apply extra permissions from github-app.permissions (nested wins over job-level)
	if len(app.Permissions) > 0 {
		githubConfigLog.Printf("Applying %d extra permissions from github-app.permissions", len(app.Permissions))
		for key, val := range app.Permissions {
			scope := convertStringToPermissionScope(key)
			if scope == "" {
				msg := fmt.Sprintf("Unknown permission scope %q in tools.github.github-app.permissions. Valid scopes include: members, organization-administration, team-discussions, organization-members, administration, etc.", key)
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
				continue
			}
			level := strings.ToLower(strings.TrimSpace(val))
			if level != string(PermissionRead) && level != string(PermissionNone) {
				msg := fmt.Sprintf("Unknown permission level %q for scope %q in tools.github.github-app.permissions. Valid levels are: read, none.", val, key)
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
				continue
			}
			permissions.Set(scope, PermissionLevel(level))
		}
	}

	// Generate the token minting step using the existing helper from safe_outputs_app.go
	rawSteps := c.buildGitHubAppTokenMintStepWithMeta(
		app,
		permissions,
		"",
		inferSingleCheckoutRepositoryForGitHubAppOwner(data),
		"Generate GitHub App token",
		"github-mcp-app-token",
	)
	return rawSteps
}

// generateParseGuardVarsStep generates a step that parses the blocked-users, trusted-users, and
// approval-labels variables at runtime into proper JSON arrays.
//
// The step is only emitted when explicit guard policies are configured (min-integrity or
// allowed-repos set), because only then does the guard-policies block reference
// `steps.parse-guard-vars.outputs.*`.
//
// The step runs parse_guard_list.sh which:
//   - Accepts GH_AW_BLOCKED_USERS_EXTRA / GH_AW_TRUSTED_USERS_EXTRA / GH_AW_APPROVAL_LABELS_EXTRA
//     for compile-time static items or user-provided expressions.
//   - Accepts GH_AW_BLOCKED_USERS_VAR / GH_AW_TRUSTED_USERS_VAR / GH_AW_APPROVAL_LABELS_VAR for
//     the GH_AW_GITHUB_* org/repo variable fallbacks.
//   - Splits all inputs on commas and newlines, trims whitespace, removes empty entries.
//   - Outputs `blocked_users`, `trusted_users`, and `approval_labels` as JSON arrays via $GITHUB_OUTPUT.
//   - Fails the step if any item is invalid.
func (c *Compiler) generateParseGuardVarsStep(yaml *strings.Builder, data *WorkflowData) {
	githubTool, hasGitHub := data.Tools["github"]
	if !hasGitHub || githubTool == false {
		githubConfigLog.Print("Skipping parse-guard-vars step: GitHub tool not enabled")
		return
	}
	githubToolMap, _ := githubTool.(map[string]any)

	// Only generate the step when guard policies are configured.
	if len(getGitHubGuardPolicies(githubToolMap)) == 0 {
		githubConfigLog.Print("Skipping parse-guard-vars step: no explicit guard policies configured")
		return
	}

	githubConfigLog.Print("Generating parse-guard-vars step for blocked-users, trusted-users and approval-labels")

	// Determine the compile-time static values (or user expression) for each field.
	// These come from the parsed tools config so we don't lose data from the raw map.
	var blockedUsersExtra, trustedUsersExtra, approvalLabelsExtra string

	if data.ParsedTools != nil && data.ParsedTools.GitHub != nil {
		gh := data.ParsedTools.GitHub
		switch {
		case len(gh.BlockedUsers) > 0:
			// Static list from frontmatter — join as comma-separated for the env var.
			blockedUsersExtra = strings.Join(gh.BlockedUsers, ",")
		case gh.BlockedUsersExpr != "":
			// User-provided GitHub Actions expression — passed verbatim; GHA evaluates it.
			blockedUsersExtra = gh.BlockedUsersExpr
		}
		switch {
		case len(gh.TrustedUsers) > 0:
			trustedUsersExtra = strings.Join(gh.TrustedUsers, ",")
		case gh.TrustedUsersExpr != "":
			trustedUsersExtra = gh.TrustedUsersExpr
		}
		switch {
		case len(gh.ApprovalLabels) > 0:
			approvalLabelsExtra = strings.Join(gh.ApprovalLabels, ",")
		case gh.ApprovalLabelsExpr != "":
			approvalLabelsExtra = gh.ApprovalLabelsExpr
		}
	}

	yaml.WriteString("      - name: Parse integrity filter lists\n")
	yaml.WriteString("        id: parse-guard-vars\n")
	yaml.WriteString("        env:\n")

	if blockedUsersExtra != "" {
		fmt.Fprintf(yaml, "          GH_AW_BLOCKED_USERS_EXTRA: %s\n", blockedUsersExtra)
	}
	fmt.Fprintf(yaml, "          GH_AW_BLOCKED_USERS_VAR: ${{ vars.%s || '' }}\n", constants.EnvVarGitHubBlockedUsers)

	if trustedUsersExtra != "" {
		fmt.Fprintf(yaml, "          GH_AW_TRUSTED_USERS_EXTRA: %s\n", trustedUsersExtra)
	}
	fmt.Fprintf(yaml, "          GH_AW_TRUSTED_USERS_VAR: ${{ vars.%s || '' }}\n", constants.EnvVarGitHubTrustedUsers)

	if approvalLabelsExtra != "" {
		fmt.Fprintf(yaml, "          GH_AW_APPROVAL_LABELS_EXTRA: %s\n", approvalLabelsExtra)
	}
	fmt.Fprintf(yaml, "          GH_AW_APPROVAL_LABELS_VAR: ${{ vars.%s || '' }}\n", constants.EnvVarGitHubApprovalLabels)

	yaml.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/parse_guard_list.sh\"\n")
}
