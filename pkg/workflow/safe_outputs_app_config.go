package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var safeOutputsAppLog = logger.New("workflow:safe_outputs_app")
var githubExpressionWhitespaceReplacer = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

// ========================================
// GitHub App Configuration
// ========================================

// GitHubAppConfig holds configuration for GitHub App-based token minting
type GitHubAppConfig struct {
	// AppID holds the canonical "client-id" value. The deprecated "app-id" key is
	// accepted on input and normalized to "client-id" on output.
	AppID           string            `json:"client-id,omitempty" yaml:"client-id,omitempty"`                 // GitHub App client ID (or legacy app ID) (e.g., "${{ vars.APP_ID }}")
	PrivateKey      string            `json:"private-key,omitempty" yaml:"private-key,omitempty"`             // GitHub App private key (e.g., "${{ secrets.APP_PRIVATE_KEY }}")
	IgnoreIfMissing bool              `json:"ignore-if-missing,omitempty" yaml:"ignore-if-missing,omitempty"` // If true, skip token minting when client-id/private-key resolve empty
	Owner           string            `json:"owner,omitempty" yaml:"owner,omitempty"`                         // Optional: owner of the GitHub App installation (defaults to checkout.repository owner when derivable, otherwise current repository owner)
	Repositories    []string          `json:"repositories,omitempty" yaml:"repositories,omitempty"`           // Optional: comma or newline-separated list of repositories to grant access to
	Permissions     map[string]string `json:"permissions,omitempty" yaml:"permissions,omitempty"`             // Optional: extra permission-* fields to merge into the minted token (nested wins over job-level)
}

// ========================================
// App Configuration Parsing
// ========================================

// parseAppConfig parses the app configuration from a map
func parseAppConfig(appMap map[string]any) *GitHubAppConfig {
	safeOutputsAppLog.Print("Parsing GitHub App configuration")
	appConfig := &GitHubAppConfig{}

	// Parse client-id/app-id (required)
	// Prefer client-id when both are provided; app-id is accepted for backward compatibility.
	if clientID, exists := appMap["client-id"]; exists {
		if clientIDStr, ok := clientID.(string); ok {
			appConfig.AppID = clientIDStr
		}
	} else if appID, exists := appMap["app-id"]; exists {
		if appIDStr, ok := appID.(string); ok {
			appConfig.AppID = appIDStr
		}
	}

	// Parse private-key (required)
	if privateKey, exists := appMap["private-key"]; exists {
		if privateKeyStr, ok := privateKey.(string); ok {
			appConfig.PrivateKey = privateKeyStr
		}
	}

	// Parse ignore-if-missing behavior (optional): true to skip minting when key inputs are empty
	if ignoreIfMissing, exists := appMap["ignore-if-missing"]; exists {
		if ignore, ok := ignoreIfMissing.(bool); ok {
			appConfig.IgnoreIfMissing = ignore
		} else {
			safeOutputsAppLog.Printf("Ignoring github-app.ignore-if-missing: expected boolean, got %T", ignoreIfMissing)
		}
	}

	// Parse owner (optional)
	if owner, exists := appMap["owner"]; exists {
		if ownerStr, ok := owner.(string); ok {
			appConfig.Owner = ownerStr
		}
	}

	// Parse repositories (optional)
	if repos, exists := appMap["repositories"]; exists {
		switch reposArray := repos.(type) {
		case []any:
			var repoStrings []string
			for _, repo := range reposArray {
				if repoStr, ok := repo.(string); ok {
					repoStrings = append(repoStrings, repoStr)
				}
			}
			appConfig.Repositories = repoStrings
		case []string:
			appConfig.Repositories = append([]string(nil), reposArray...)
		}
	}

	// Parse permissions (optional) - extra permission-* fields to merge into the minted token
	if perms, exists := appMap["permissions"]; exists {
		switch permsMap := perms.(type) {
		case map[string]any:
			appConfig.Permissions = make(map[string]string, len(permsMap))
			for key, val := range permsMap {
				if valStr, ok := val.(string); ok {
					appConfig.Permissions[key] = valStr
				} else {
					safeOutputsAppLog.Printf("Ignoring github-app.permissions[%q]: expected string value, got %T", key, val)
				}
			}
		case map[string]string:
			appConfig.Permissions = maps.Clone(permsMap)
		default:
			safeOutputsAppLog.Printf("Ignoring github-app.permissions: expected object, got %T", perms)
		}
	}

	return appConfig
}

func (app *GitHubAppConfig) shouldIgnoreMissingKey() bool {
	if app == nil {
		return false
	}
	return app.IgnoreIfMissing
}

func (app *GitHubAppConfig) hasRequiredCredentials() bool {
	if app == nil {
		return false
	}
	return strings.TrimSpace(app.AppID) != "" && strings.TrimSpace(app.PrivateKey) != ""
}

// extractWrappedGitHubExpression returns the inner text for values wrapped as
// `${{ ... }}` (for example, `${{ secrets.APP_ID }}` -> `secrets.APP_ID`).
// It returns false for literals and malformed/empty wrappers.
func extractWrappedGitHubExpression(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "${{") || !strings.HasSuffix(trimmed, "}}") {
		return "", false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "${{"), "}}"))
	// Reject wrappers with no usable expression body (e.g. `${{ }}`).
	if inner == "" {
		return "", false
	}
	return inner, true
}

// buildGitHubExpressionNonEmptyCheck renders a non-empty check node from wrapped
// expressions (`${{ secrets.KEY }}` -> `secrets.KEY != ”`) or literals
// (`plain-value` -> `'plain-value' != ”`).
func buildGitHubExpressionNonEmptyCheck(value string) ConditionNode {
	trimmed := strings.TrimSpace(value)
	if inner, ok := extractWrappedGitHubExpression(trimmed); ok {
		return BuildNotEquals(&ExpressionNode{Expression: inner}, BuildStringLiteral(""))
	}
	return BuildNotEquals(BuildStringLiteral(strings.TrimSpace(githubExpressionWhitespaceReplacer.Replace(trimmed))), BuildStringLiteral(""))
}

// ifInvalidContextNames lists the GitHub Actions expression contexts that are not
// available in step-level 'if:' conditions. GitHub Actions allows matrix in this
// position, but still rejects secrets and jobs references.
// See: https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/evaluate-expressions-in-workflows-and-actions#contexts
var ifInvalidContextNames = map[string]struct{}{
	"jobs":    {},
	"secrets": {},
}

const (
	ignoreIfMissingAppIDEnvVar      = "GH_AW_IGNORE_IF_MISSING_APP_ID"
	ignoreIfMissingPrivateKeyEnvVar = "GH_AW_IGNORE_IF_MISSING_PRIVATE_KEY"
)

type stepEnvAssignment struct {
	Name  string
	Value string
}

type ignoreIfMissingGuard struct {
	Condition      string
	EnvAssignments []stepEnvAssignment
}

// isGitHubExpressionIdentifierChar reports whether a byte can appear in a GitHub
// Actions expression identifier token (ASCII letter, digit, or underscore).
func isGitHubExpressionIdentifierChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

// isGitHubExpressionIdentifierStart reports whether inner[i] begins an identifier
// token rather than landing in the middle of one.
func isGitHubExpressionIdentifierStart(inner string, i int) bool {
	if i >= len(inner) {
		return false
	}
	return isGitHubExpressionIdentifierChar(inner[i]) && (i == 0 || !isGitHubExpressionIdentifierChar(inner[i-1]))
}

// consumeSingleQuotedGitHubExpressionString skips over a single-quoted GitHub
// expression string literal, honoring doubled single quotes as escapes. It returns
// the first byte position after the closing quote, or len(inner) if unterminated.
func consumeSingleQuotedGitHubExpressionString(inner string, start int) int {
	i := start + 1
	for i < len(inner) {
		if inner[i] != '\'' {
			i++
			continue
		}
		if i+1 < len(inner) && inner[i+1] == '\'' {
			i += 2
			continue
		}
		return i + 1
	}
	return i
}

// containsInvalidIfContextReference returns true when the inner expression body
// contains a jobs or secrets context token anywhere outside single-quoted string
// literals, including bracket notation such as secrets['TOKEN'].
func containsInvalidIfContextReference(inner string) bool {
	for i := 0; i < len(inner); {
		if inner[i] == '\'' {
			i = consumeSingleQuotedGitHubExpressionString(inner, i)
			continue
		}

		if !isGitHubExpressionIdentifierStart(inner, i) {
			i++
			continue
		}

		start := i
		for i < len(inner) && isGitHubExpressionIdentifierChar(inner[i]) {
			i++
		}
		name := inner[start:i]
		if _, ok := ifInvalidContextNames[name]; !ok {
			continue
		}

		j := i
		for j < len(inner) && (inner[j] == ' ' || inner[j] == '\t' || inner[j] == '\n' || inner[j] == '\r') {
			j++
		}
		if j < len(inner) && (inner[j] == '.' || inner[j] == '[') {
			return true
		}
	}
	return false
}

// combineGitHubIfExpressions accepts either wrapped `${{ ... }}` conditions or raw
// inner expression fragments and normalizes them into one wrapped if-expression.
func combineGitHubIfExpressions(expressions ...string) string {
	var parts []string
	for _, expression := range expressions {
		trimmed := strings.TrimSpace(expression)
		if trimmed == "" {
			continue
		}
		if inner, ok := extractWrappedGitHubExpression(trimmed); ok {
			parts = append(parts, inner)
			continue
		}
		parts = append(parts, trimmed)
	}
	if len(parts) == 0 {
		return ""
	}
	return wrapGitHubExpression(strings.Join(parts, " && "))
}

func appendStepEnvAssignments(steps []string, assignments []stepEnvAssignment) []string {
	if len(assignments) == 0 {
		return steps
	}
	steps = append(steps, "        env:\n")
	for _, assignment := range assignments {
		steps = append(steps, fmt.Sprintf("          %s: %s\n", assignment.Name, assignment.Value))
	}
	return steps
}

// buildIgnoreIfMissingCondition returns a GitHub Actions if-expression that requires
// all GitHub App credential inputs that can be checked in an if: condition to be non-empty.
// Values referencing the secrets or jobs contexts are routed through step-local env
// aliases so the guard can still check them through the supported env context.
func buildIgnoreIfMissingCondition(app *GitHubAppConfig) ignoreIfMissingGuard {
	var checks []ConditionNode
	guard := ignoreIfMissingGuard{}
	for _, credential := range []struct {
		value   string
		envName string
	}{
		{value: app.AppID, envName: ignoreIfMissingAppIDEnvVar},
		{value: app.PrivateKey, envName: ignoreIfMissingPrivateKeyEnvVar},
	} {
		trimmed := strings.TrimSpace(credential.value)
		if inner, ok := extractWrappedGitHubExpression(trimmed); ok {
			if containsInvalidIfContextReference(inner) {
				safeOutputsAppLog.Printf("Rewriting %q in ignore-if-missing condition through env.%s: context not valid in if: expressions", inner, credential.envName)
				guard.EnvAssignments = append(guard.EnvAssignments, stepEnvAssignment{
					Name:  credential.envName,
					Value: trimmed,
				})
				checks = append(checks, BuildNotEquals(&ExpressionNode{Expression: "env." + credential.envName}, BuildStringLiteral("")))
				continue
			}
		}
		checks = append(checks, buildGitHubExpressionNonEmptyCheck(credential.value))
	}
	if len(checks) == 0 {
		return guard
	}
	condition := checks[0]
	for i := 1; i < len(checks); i++ {
		condition = BuildAnd(condition, checks[i])
	}
	guard.Condition = wrapGitHubExpression(RenderCondition(condition))
	return guard
}

// ========================================
// App Configuration Merging
// ========================================

// mergeAppFromIncludedConfigs merges app configuration from included safe-outputs configurations
// If the top-level workflow has an app configured, it takes precedence
// Otherwise, the first app configuration found in included configs is used
func (c *Compiler) mergeAppFromIncludedConfigs(topSafeOutputs *SafeOutputsConfig, includedConfigs []string) (*GitHubAppConfig, error) {
	safeOutputsAppLog.Printf("Merging app configuration: included_configs=%d", len(includedConfigs))
	// If top-level workflow already has app configured, use it (no merge needed)
	if topSafeOutputs != nil && topSafeOutputs.GitHubApp != nil {
		safeOutputsAppLog.Print("Using top-level app configuration")
		return topSafeOutputs.GitHubApp, nil
	}

	// Otherwise, find the first app configuration in included configs
	for _, configJSON := range includedConfigs {
		if configJSON == "" || configJSON == "{}" {
			continue
		}

		// Parse the safe-outputs configuration
		var safeOutputsConfig map[string]any
		if err := json.Unmarshal([]byte(configJSON), &safeOutputsConfig); err != nil {
			continue // Skip invalid JSON
		}

		// Extract app from the safe-outputs.github-app field
		if appData, exists := safeOutputsConfig["github-app"]; exists {
			if appMap, ok := appData.(map[string]any); ok {
				appConfig := parseAppConfig(appMap)

				// Return first valid app configuration found
				if appConfig.AppID != "" && appConfig.PrivateKey != "" {
					safeOutputsAppLog.Print("Found valid app configuration in included config")
					return appConfig, nil
				}
			}
		}
	}

	safeOutputsAppLog.Print("No app configuration found in included configs")
	return nil, nil
}

// ========================================
// GitHub App Token Steps Generation
// ========================================

// buildGitHubAppTokenMintStep generates the step to mint a GitHub App installation access token
// Permissions are automatically computed from the safe output job requirements.
// fallbackRepoExpr overrides the default ${{ github.event.repository.name }} fallback when
// no explicit repositories are configured (e.g. pass needs.activation.outputs.target_repo_name for
// workflow_call relay workflows so the token is scoped to the platform repo's NAME, not the full
// owner/repo slug — actions/create-github-app-token expects repo names only when owner is also set).
func (c *Compiler) buildGitHubAppTokenMintStep(app *GitHubAppConfig, permissions *Permissions, fallbackRepoExpr string) []string {
	return c.buildGitHubAppTokenMintStepWithMeta(app, permissions, fallbackRepoExpr, "", "Generate GitHub App token", "safe-outputs-app-token")
}

func (c *Compiler) buildGitHubAppTokenMintStepForRepository(app *GitHubAppConfig, permissions *Permissions, fallbackRepoExpr string, ownerSourceRepository string) []string {
	return c.buildGitHubAppTokenMintStepWithMeta(app, permissions, fallbackRepoExpr, ownerSourceRepository, "Generate GitHub App token", "safe-outputs-app-token")
}

func appTokenPermissionFields(app *GitHubAppConfig, permissions *Permissions) map[string]string {
	if permissions == nil {
		permissions = NewPermissionsContentsRead()
	}
	permissionFields := convertPermissionsToAppTokenFields(permissions)
	for key, val := range app.Permissions {
		scope := convertStringToPermissionScope(key)
		if scope == "" {
			safeOutputsAppLog.Printf("Skipping unknown permission scope %q in github-app.permissions", key)
			continue
		}
		level := strings.ToLower(strings.TrimSpace(val))
		tempPerms := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{scope: PermissionLevel(level)})
		maps.Copy(permissionFields, convertPermissionsToAppTokenFields(tempPerms))
	}
	if len(permissionFields) == 0 {
		permissionFields["permission-contents"] = "none"
	}
	return permissionFields
}

func appendGitHubAppTokenRepositoryInput(steps []string, app *GitHubAppConfig, fallbackRepoExpr string) []string {
	if len(app.Repositories) == 1 {
		for _, repository := range app.Repositories {
			if repository == "*" {
				safeOutputsAppLog.Print("Using org-wide GitHub App token (repositories: *)")
			} else {
				steps = append(steps, fmt.Sprintf("          repositories: %s\n", repository))
			}
		}
	} else if len(app.Repositories) > 1 {
		steps = append(steps, "          repositories: |-\n")
		for _, repository := range app.Repositories {
			steps = append(steps, fmt.Sprintf("            %s\n", repository))
		}
	} else {
		repoExpr := fallbackRepoExpr
		if repoExpr == "" {
			repoExpr = "${{ github.event.repository.name }}"
		}
		steps = append(steps, fmt.Sprintf("          repositories: %s\n", repoExpr))
	}
	return steps
}

func (c *Compiler) buildGitHubAppTokenMintStepWithMeta(app *GitHubAppConfig, permissions *Permissions, fallbackRepoExpr string, ownerSourceRepository string, stepName string, stepID string) []string {
	safeOutputsAppLog.Printf("Building GitHub App token mint step: owner=%s, repos=%d", app.Owner, len(app.Repositories))
	var steps []string

	owner, ownerSteps := resolveGitHubAppOwner(app, ownerSourceRepository, stepName, stepID)
	steps = append(steps, ownerSteps...)
	steps = append(steps, fmt.Sprintf("      - name: %s\n", stepName))
	steps = append(steps, fmt.Sprintf("        id: %s\n", stepID))
	if app.shouldIgnoreMissingKey() {
		guard := buildIgnoreIfMissingCondition(app)
		steps = appendStepEnvAssignments(steps, guard.EnvAssignments)
		if guard.Condition != "" {
			steps = append(steps, fmt.Sprintf("        if: %s\n", guard.Condition))
		}
	}
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getActionPin("actions/create-github-app-token")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          client-id: %s\n", app.AppID))
	steps = append(steps, fmt.Sprintf("          private-key: %s\n", app.PrivateKey))

	// Add owner - default to the derived checkout owner when available, otherwise current repository owner.
	steps = append(steps, fmt.Sprintf("          owner: %s\n", owner))

	steps = appendGitHubAppTokenRepositoryInput(steps, app, fallbackRepoExpr)

	// Always add github-api-url from environment variable
	steps = append(steps, "          github-api-url: ${{ github.api_url }}\n")

	permissionFields := appTokenPermissionFields(app, permissions)
	keys := sliceutil.SortedKeys(permissionFields)

	for _, key := range keys {
		steps = append(steps, fmt.Sprintf("          %s: %s\n", key, permissionFields[key]))
	}

	return steps
}

// convertPermissionsToAppTokenFields converts job Permissions to permission-* action inputs
// This follows GitHub's recommendation for explicit permission control
// Note: This maps all permissions (both GitHub Actions and GitHub App-only) to their
// corresponding permission-* fields in actions/create-github-app-token.
// Some GitHub Actions permissions (like 'models', 'id-token', 'attestations', 'copilot-requests')
// don't have corresponding GitHub App permissions and are skipped.
//
// For GitHub Actions permissions (actions, checks, contents, …) we use Get() so that shorthand
// permissions like "read-all" are correctly expanded.
// For GitHub App-only permissions (administration, members, organization-secrets, …) we use
// GetExplicit() so that only scopes the user actually declared are forwarded — a "read-all"
// shorthand must never accidentally grant broad GitHub App-only permissions.
func convertPermissionsToAppTokenFields(permissions *Permissions) map[string]string {
	fields := make(map[string]string)

	// Map GitHub Actions permissions to GitHub App permissions
	// See: https://github.com/actions/create-github-app-token#permissions

	// GitHub Actions permissions that also exist in GitHub App
	if level, ok := permissions.Get(PermissionActions); ok {
		fields["permission-actions"] = string(level)
	}
	if level, ok := permissions.Get(PermissionChecks); ok {
		fields["permission-checks"] = string(level)
	}
	if level, ok := permissions.Get(PermissionContents); ok {
		fields["permission-contents"] = string(level)
	}
	if level, ok := permissions.Get(PermissionDeployments); ok {
		fields["permission-deployments"] = string(level)
	}
	if level, ok := permissions.Get(PermissionIssues); ok {
		fields["permission-issues"] = string(level)
	}
	if level, ok := permissions.Get(PermissionPackages); ok {
		fields["permission-packages"] = string(level)
	}
	if level, ok := permissions.Get(PermissionPages); ok {
		fields["permission-pages"] = string(level)
	}
	if level, ok := permissions.Get(PermissionPullRequests); ok {
		fields["permission-pull-requests"] = string(level)
	}
	if level, ok := permissions.Get(PermissionSecurityEvents); ok {
		fields["permission-security-events"] = string(level)
	}
	if level, ok := permissions.Get(PermissionStatuses); ok {
		fields["permission-statuses"] = string(level)
	}
	if level, ok := permissions.Get(PermissionVulnerabilityAlerts); ok {
		fields["permission-vulnerability-alerts"] = string(level)
	}
	// "permission-discussions" is a declared input in actions/create-github-app-token v3+.
	// Crucially, when ANY permission-* input is specified the action scopes the token to ONLY those
	// permissions (returning undefined → inherit-all only when zero permission-* inputs are present).
	// Since the compiler always emits other permission-* fields, omitting permission-discussions causes
	// the minted token to lack discussions access even when the GitHub App installation has that permission.
	if level, ok := permissions.Get(PermissionDiscussions); ok {
		fields["permission-discussions"] = string(level)
	}

	// GitHub App-only permissions (not available in GitHub Actions GITHUB_TOKEN).
	// Use GetExplicit() so that shorthand permissions like "read-all" do not accidentally
	// expand into broad GitHub App-only grants that the user never declared.
	// Repository-level
	if level, ok := permissions.GetExplicit(PermissionAdministration); ok {
		fields["permission-administration"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionEnvironments); ok {
		fields["permission-environments"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionGitSigning); ok {
		fields["permission-git-signing"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionWorkflows); ok {
		fields["permission-workflows"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionRepositoryHooks); ok {
		fields["permission-repository-hooks"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionSingleFile); ok {
		fields["permission-single-file"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionCodespaces); ok {
		fields["permission-codespaces"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionRepositoryCustomProperties); ok {
		fields["permission-repository-custom-properties"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionSecretScanningAlerts); ok && level != PermissionNone {
		fields["permission-secret-scanning-alerts"] = string(level)
	}
	// Organization-level
	if level, ok := permissions.GetExplicit(PermissionOrganizationProj); ok {
		fields["permission-organization-projects"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionMembers); ok {
		fields["permission-members"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationAdministration); ok {
		fields["permission-organization-administration"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionTeamDiscussions); ok {
		fields["permission-team-discussions"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationHooks); ok {
		fields["permission-organization-hooks"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationMembers); ok {
		fields["permission-organization-members"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationPackages); ok {
		fields["permission-organization-packages"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationSelfHostedRunners); ok {
		fields["permission-organization-self-hosted-runners"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationCustomOrgRoles); ok {
		fields["permission-organization-custom-org-roles"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationCustomProperties); ok {
		fields["permission-organization-custom-properties"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationCustomRepositoryRoles); ok {
		fields["permission-organization-custom-repository-roles"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationAnnouncementBanners); ok {
		fields["permission-organization-announcement-banners"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationEvents); ok {
		fields["permission-organization-events"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationPlan); ok {
		fields["permission-organization-plan"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationUserBlocking); ok {
		fields["permission-organization-user-blocking"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationPersonalAccessTokenReqs); ok {
		fields["permission-organization-personal-access-token-requests"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationPersonalAccessTokens); ok {
		fields["permission-organization-personal-access-tokens"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationCopilot); ok {
		fields["permission-organization-copilot"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionOrganizationCodespaces); ok {
		fields["permission-organization-codespaces"] = string(level)
	}
	// User-level
	if level, ok := permissions.GetExplicit(PermissionEmailAddresses); ok {
		fields["permission-email-addresses"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionCodespacesLifecycleAdmin); ok {
		fields["permission-codespaces-lifecycle-admin"] = string(level)
	}
	if level, ok := permissions.GetExplicit(PermissionCodespacesMetadata); ok {
		fields["permission-codespaces-metadata"] = string(level)
	}

	// Note: The following GitHub Actions permissions do NOT have GitHub App equivalents
	// and are therefore not mapped to permission-* fields:
	// - models: no GitHub App permission for AI model access
	// - id-token: not applicable to GitHub Apps (OIDC-specific)
	// - attestations: no GitHub App permission for this
	// - copilot-requests: GitHub Actions-specific Copilot authentication token
	// - metadata: GitHub App metadata permission is automatically included (read-only)

	return fields
}

// ========================================
// Activation Token Steps Generation
// ========================================

// buildActivationAppTokenMintStep generates the step to mint a GitHub App installation access token
// for use in the pre-activation (reaction) and activation (status comment) jobs.
func (c *Compiler) buildActivationAppTokenMintStep(app *GitHubAppConfig, permissions *Permissions) []string {
	safeOutputsAppLog.Printf("Building activation GitHub App token mint step: owner=%s", app.Owner)
	var steps []string

	steps = append(steps, "      - name: Generate GitHub App token for activation\n")
	steps = append(steps, "        id: activation-app-token\n")
	if app.shouldIgnoreMissingKey() {
		guard := buildIgnoreIfMissingCondition(app)
		steps = appendStepEnvAssignments(steps, guard.EnvAssignments)
		if guard.Condition != "" {
			steps = append(steps, fmt.Sprintf("        if: %s\n", guard.Condition))
		}
	}
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getActionPin("actions/create-github-app-token")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          client-id: %s\n", app.AppID))
	steps = append(steps, fmt.Sprintf("          private-key: %s\n", app.PrivateKey))

	// Add owner - default to current repository owner if not specified
	owner := app.Owner
	if owner == "" {
		owner = "${{ github.repository_owner }}"
	}
	steps = append(steps, fmt.Sprintf("          owner: %s\n", owner))

	// Add repositories - behavior depends on configuration:
	// - If repositories is ["*"], omit the field to allow org-wide access
	// - If repositories is a single value, use inline format
	// - If repositories has multiple values, use block scalar format (newline-separated)
	// - If repositories is empty/not specified, default to the current repository
	if len(app.Repositories) == 1 && app.Repositories[0] == "*" {
		// Org-wide access: omit repositories field entirely
		safeOutputsAppLog.Print("Using org-wide GitHub App token for activation (repositories: *)")
	} else if len(app.Repositories) == 1 {
		steps = append(steps, fmt.Sprintf("          repositories: %s\n", app.Repositories[0]))
	} else if len(app.Repositories) > 1 {
		steps = append(steps, "          repositories: |-\n")
		for _, repo := range app.Repositories {
			steps = append(steps, fmt.Sprintf("            %s\n", repo))
		}
	} else {
		steps = append(steps, "          repositories: ${{ github.event.repository.name }}\n")
	}

	// Always add github-api-url from environment variable
	steps = append(steps, "          github-api-url: ${{ github.api_url }}\n")

	// Add permission-* fields automatically computed from job permissions
	if permissions != nil {
		permissionFields := convertPermissionsToAppTokenFields(permissions)

		keys := sliceutil.SortedKeys(permissionFields)

		for _, key := range keys {
			steps = append(steps, fmt.Sprintf("          %s: %s\n", key, permissionFields[key]))
		}
	}

	return steps
}

// resolveActivationToken returns the GitHub token to use for activation steps (reactions, status comments).
// Priority: GitHub App minted token > custom github-token > GITHUB_TOKEN (default)
//
// When returning the app token reference, callers MUST ensure that buildActivationAppTokenMintStep
// has already been called to generate the 'activation-app-token' step, since this function returns
// a reference to that step's output (${{ steps.activation-app-token.outputs.token }}).
func (c *Compiler) resolveActivationToken(data *WorkflowData) string {
	if data.ActivationGitHubApp != nil {
		if data.ActivationGitHubApp.shouldIgnoreMissingKey() {
			return combineTokenExpressions("${{ steps.activation-app-token.outputs.token }}", "${{ secrets.GITHUB_TOKEN }}")
		}
		return "${{ steps.activation-app-token.outputs.token }}"
	}
	if data.ActivationGitHubToken != "" {
		return data.ActivationGitHubToken
	}
	return "${{ secrets.GITHUB_TOKEN }}"
}
