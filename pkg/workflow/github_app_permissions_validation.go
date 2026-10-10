package workflow

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var githubAppPermissionsLog = logger.New("workflow:github_app_permissions_validation")

// validateGitHubAppOnlyPermissions validates that when GitHub App-only permissions
// are specified in the workflow, a GitHub App is configured somewhere in the workflow,
// and that no GitHub App-only permission is declared with "write" access (write operations
// must be performed via safe-outputs, not through declared permissions).
//
// GitHub App-only permissions (e.g., members, administration) cannot be exercised
// through the GITHUB_TOKEN — they require a GitHub App installation access token. When such
// permissions are declared, a GitHub App must be configured via one of:
//   - tools.github.github-app
//   - safe-outputs.github-app
//   - the top-level github-app field (for the activation/pre-activation jobs)
//
// The caller must pass the pre-parsed permissions to avoid redundant YAML parsing.
// Returns an error if GitHub App-only permissions are used without any GitHub App configured,
// or if "write" level is requested for any GitHub App-only scope.
func validateGitHubAppOnlyPermissions(workflowData *WorkflowData, permissions *Permissions) error {
	githubAppPermissionsLog.Print("Starting GitHub App-only permissions validation")

	if workflowData.Permissions == "" {
		githubAppPermissionsLog.Print("No permissions defined, validation passed")
		return nil
	}

	if permissions == nil {
		githubAppPermissionsLog.Print("Could not parse permissions, validation passed")
		return nil
	}

	// Find any GitHub App-only permission scopes that are *explicitly* declared.
	// We must not use Get() here because shorthand permissions (read-all / write-all) and
	// "all: read" would cause Get() to return a value for every scope, incorrectly
	// requiring a GitHub App even when no App-only scopes were explicitly declared.
	var appOnlyScopes []PermissionScope
	for _, scope := range GetAllGitHubAppOnlyScopes() {
		if _, exists := permissions.GetExplicit(scope); exists {
			appOnlyScopes = append(appOnlyScopes, scope)
		}
	}

	if len(appOnlyScopes) == 0 {
		githubAppPermissionsLog.Print("No GitHub App-only permissions found, validation passed")
		return nil
	}

	githubAppPermissionsLog.Printf("Found %d GitHub App-only permissions, checking for GitHub App configuration", len(appOnlyScopes))

	// Check if "write" is requested for any read-only GitHub App-only scopes.
	if err := validateGitHubAppOnlyPermissionsWrite(permissions, appOnlyScopes); err != nil {
		return err
	}

	// Check if any GitHub App is configured
	if hasGitHubAppConfigured(workflowData) {
		githubAppPermissionsLog.Print("GitHub App is configured, validation passed")
		return nil
	}

	// Format the error message
	return formatGitHubAppRequiredError(appOnlyScopes)
}

// validateGitHubAppOnlyPermissionsWrite checks that no GitHub App-only scope has been
// requested with "write" access. Write operations on GitHub App tokens must be performed
// via safe-outputs, not through declared permissions.
func validateGitHubAppOnlyPermissionsWrite(permissions *Permissions, appOnlyScopes []PermissionScope) error {
	var writtenScopes []PermissionScope
	for _, scope := range appOnlyScopes {
		if level, exists := permissions.GetExplicit(scope); exists && level == PermissionWrite {
			writtenScopes = append(writtenScopes, scope)
		}
	}
	if len(writtenScopes) == 0 {
		return nil
	}
	return formatWriteOnAppScopesError(writtenScopes)
}

// formatWriteOnAppScopesError formats the error when "write" is requested for
// any GitHub App-only scope. All App-only scopes must be declared read-only;
// write operations are performed via safe-outputs.
func formatWriteOnAppScopesError(scopes []PermissionScope) error {
	scopeStrs := make([]string, len(scopes))
	for i, s := range scopes {
		scopeStrs[i] = string(s)
	}
	sort.Strings(scopeStrs)

	var lines []string
	lines = append(lines, "GitHub App permissions must be declared as \"read\" only.")
	lines = append(lines, "Write operations are performed via safe-outputs, not through declared permissions.")
	lines = append(lines, "The following GitHub App-only permissions were declared with \"write\" access:")
	lines = append(lines, "")
	for _, s := range scopeStrs {
		lines = append(lines, "  - "+s)
	}
	lines = append(lines, "")
	lines = append(lines, "Change the permission level to \"read\" or use safe-outputs for write operations.")

	return errors.New(strings.Join(lines, "\n"))
}

// validateGitHubMCPAppPermissionsNoWrite validates that every scope in
// tools.github.github-app.permissions is set to "read" or "none" (after trimming/lowercasing).
// The schema allows "write" so that editors can offer it as a completion, but
// the compiler must reject it because GitHub App-only scopes have no write-level
// semantics in this context — write operations must go through safe-outputs.
// Any other unrecognised level is also rejected here.
func validateGitHubMCPAppPermissionsNoWrite(workflowData *WorkflowData) error {
	if workflowData.ParsedTools == nil ||
		workflowData.ParsedTools.GitHub == nil ||
		workflowData.ParsedTools.GitHub.GitHubApp == nil {
		return nil
	}
	app := workflowData.ParsedTools.GitHub.GitHubApp
	if len(app.Permissions) == 0 {
		return nil
	}

	var invalidScopes []string
	var writeScopes []string
	for scope, level := range app.Permissions {
		normalized := strings.ToLower(strings.TrimSpace(level))
		switch normalized {
		case string(PermissionRead), string(PermissionNone):
			// valid
		default:
			invalidScopes = append(invalidScopes, scope+" (level: "+level+")")
			if normalized == string(PermissionWrite) {
				writeScopes = append(writeScopes, scope)
			}
		}
	}
	if len(invalidScopes) == 0 {
		return nil
	}
	sort.Strings(invalidScopes)
	sort.Strings(writeScopes)

	var lines []string
	lines = append(lines, "Invalid permission levels in tools.github.github-app.permissions.")
	lines = append(lines, "Each permission level must be exactly \"read\" or \"none\".")
	if len(writeScopes) > 0 {
		lines = append(lines, "")
		lines = append(lines, `"write" is not allowed: write operations must be performed via safe-outputs.`)
		lines = append(lines, "")
		lines = append(lines, "The following scopes were declared with \"write\" access:")
		lines = append(lines, "")
		for _, s := range writeScopes {
			lines = append(lines, "  - "+s)
		}
	}
	lines = append(lines, "")
	lines = append(lines, "The following scopes have invalid permission levels:")
	lines = append(lines, "")
	for _, s := range invalidScopes {
		lines = append(lines, "  - "+s)
	}
	lines = append(lines, "")
	lines = append(lines, "Change the permission level to \"read\" for read-only access, or \"none\" to disable the scope.")
	return errors.New(strings.Join(lines, "\n"))
}

func validateMentionGitHubAppPermissionsReadOnly(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.SafeOutputs == nil ||
		workflowData.SafeOutputs.Mentions == nil || workflowData.SafeOutputs.Mentions.GitHubApp == nil {
		return nil
	}

	var invalidScopes []string
	for scope, level := range workflowData.SafeOutputs.Mentions.GitHubApp.Permissions {
		normalized := strings.ToLower(strings.TrimSpace(level))
		if normalized != string(PermissionRead) && normalized != string(PermissionNone) {
			invalidScopes = append(invalidScopes, scope+" (level: "+level+")")
		}
	}
	if len(invalidScopes) == 0 {
		return nil
	}
	sort.Strings(invalidScopes)
	return NewValidationError(
		"safe-outputs.mentions.github-app.permissions",
		strings.Join(invalidScopes, ", "),
		"mention-resolution GitHub App permissions must be read-only; each level must be \"read\" or \"none\"",
		"Change each permission level to \"read\" or \"none\". Write permissions are not allowed for mention resolution.",
	)
}

// warnGitHubAppPermissionsUnsupportedContexts emits a warning when
// github-app.permissions is set in contexts that do not support it.
// The permissions field takes effect for tools.github.github-app and
// safe-outputs.github-app, but is silently ignored for on.github-app
// and the top-level github-app fallback.
func warnGitHubAppPermissionsUnsupportedContexts(workflowData *WorkflowData) {
	type context struct {
		label string
		app   *GitHubAppConfig
	}
	unsupported := []context{
		{"on.github-app", workflowData.ActivationGitHubApp},
		{"github-app (top-level fallback)", workflowData.TopLevelGitHubApp},
	}
	for _, ctx := range unsupported {
		if ctx.app != nil && len(ctx.app.Permissions) > 0 {
			msg := fmt.Sprintf(
				"The 'permissions' field under '%s' has no effect. "+
					"Extra GitHub App permissions apply to tools.github.github-app and safe-outputs.github-app.",
				ctx.label,
			)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(msg))
		}
	}
}

func hasGitHubAppConfigured(workflowData *WorkflowData) bool {
	// Check tools.github.github-app
	if workflowData.ParsedTools != nil &&
		workflowData.ParsedTools.GitHub != nil &&
		workflowData.ParsedTools.GitHub.GitHubApp != nil {
		githubAppPermissionsLog.Print("Found GitHub App in tools.github")
		return true
	}

	// Check safe-outputs.github-app
	if workflowData.SafeOutputs != nil && workflowData.SafeOutputs.GitHubApp != nil {
		githubAppPermissionsLog.Print("Found GitHub App in safe-outputs")
		return true
	}

	// Check the activation job github-app
	if workflowData.ActivationGitHubApp != nil {
		githubAppPermissionsLog.Print("Found GitHub App in activation config")
		return true
	}

	return false
}

// formatGitHubAppRequiredError formats an error message when GitHub App-only permissions
// are used without a GitHub App configured.
func formatGitHubAppRequiredError(appOnlyScopes []PermissionScope) error {
	// Sort for deterministic output
	scopeStrs := make([]string, len(appOnlyScopes))
	for i, s := range appOnlyScopes {
		scopeStrs[i] = string(s)
	}
	sort.Strings(scopeStrs)

	var lines []string
	lines = append(lines, "GitHub App-only permissions require a GitHub App to be configured.")
	lines = append(lines, "The following permissions are not supported by the GITHUB_TOKEN and")
	lines = append(lines, "can only be exercised through a GitHub App installation access token:")
	lines = append(lines, "")
	for _, s := range scopeStrs {
		lines = append(lines, "  - "+s)
	}
	lines = append(lines, "")
	lines = append(lines, "To fix this, configure a GitHub App in your workflow. For example:")
	lines = append(lines, ""+"tools:")
	lines = append(lines, "  github:")
	lines = append(lines, "    github-app:")
	lines = append(lines, "      client-id: ${{ vars.APP_ID }}")
	lines = append(lines, "      private-key: ${{ secrets.APP_PRIVATE_KEY }}")
	lines = append(lines, "")
	lines = append(lines, "Or in the safe-outputs section:")
	lines = append(lines, "safe-outputs:")
	lines = append(lines, "  github-app:")
	lines = append(lines, "    client-id: ${{ vars.APP_ID }}")
	lines = append(lines, "    private-key: ${{ secrets.APP_PRIVATE_KEY }}")

	return errors.New(strings.Join(lines, "\n"))
}
