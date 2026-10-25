package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	githubRepositoryExpression              = "${{ github.repository }}"
	githubLockdownGuardPolicyWarningMessage = `'tools.github.lockdown: true' is set; GitHub guard policy fields ('allowed-repos', 'min-integrity', 'blocked-users', 'trusted-users', 'approval-labels') will be ignored.
Guard policies are only evaluated when lockdown is not active.`
)

// validateGitHubReadOnly validates that read-only: false is not set for the GitHub tool.
// The GitHub MCP server always operates in read-only mode; write access is not permitted.
func validateGitHubReadOnly(tools *Tools, workflowName string) error {
	if tools == nil || tools.GitHub == nil {
		return nil
	}

	if !tools.GitHub.ReadOnly {
		toolsValidationLog.Printf("Invalid read-only configuration in workflow: %s", workflowName)
		return errors.New("'tools.github.read-only: false' is not supported because the GitHub MCP server always operates in read-only mode. Remove the 'read-only' field or set it to 'true'. Example:\ntools:\n  github:\n    read-only: true")
	}

	return nil
}

// validateGitHubToolConfig validates that the GitHub tool configuration does not
// specify both app and github-token at the same time, as only one authentication
// method is allowed.
func validateGitHubToolConfig(tools *Tools, workflowName string) error {
	if tools == nil || tools.GitHub == nil {
		return nil
	}

	if tools.GitHub.GitHubApp != nil && tools.GitHub.GitHubToken != "" {
		toolsValidationLog.Printf("Invalid GitHub tool configuration in workflow: %s", workflowName)
		return errors.New("'tools.github.github-app' and 'tools.github.github-token' cannot both be set. Use one authentication method: either 'github-app' (GitHub App) or 'github-token' (personal access token). Example:\ntools:\n  github:\n    github-token: \"${{ secrets.GITHUB_TOKEN }}\"")
	}

	return nil
}

// hasGitHubGuardPolicyFields reports whether any GitHub guard-policy fields are
// configured on the tool. It is used to detect lockdown/guard-policy
// combinations that should surface a compile-time warning.
func hasGitHubGuardPolicyFields(github *GitHubToolConfig) bool {
	if github == nil {
		return false
	}

	// This is a presence check, not a validity check. Explicit but invalid values
	// (for example an empty string or wrong type injected programmatically) still
	// count as configured guard-policy fields and are validated later. Include the
	// deprecated Repos alias so lockdown conflict warnings also cover legacy input.
	hasRepos := github.AllowedRepos != nil || github.Repos != nil
	hasMinIntegrity := github.MinIntegrity != ""
	hasBlockedUsers := len(github.BlockedUsers) > 0 || github.BlockedUsersExpr != ""
	hasApprovalLabels := len(github.ApprovalLabels) > 0 || github.ApprovalLabelsExpr != ""
	hasTrustedUsers := len(github.TrustedUsers) > 0 || github.TrustedUsersExpr != ""

	return hasRepos || hasMinIntegrity || hasBlockedUsers || hasApprovalLabels || hasTrustedUsers
}

func hasGitHubLockdownGuardPolicyConflict(github *GitHubToolConfig) bool {
	return github != nil && github.Lockdown && hasGitHubGuardPolicyFields(github)
}

func emitGitHubLockdownGuardPolicyWarning(compiler *Compiler, tools *Tools, markdownPath string) {
	if tools == nil || tools.GitHub == nil || !hasGitHubLockdownGuardPolicyConflict(tools.GitHub) {
		return
	}

	if compiler == nil {
		toolsValidationLog.Printf("Skipping lockdown/guard-policy warning because compiler is nil for workflow: %s", markdownPath)
		return
	}

	toolsValidationLog.Printf("Emitting lockdown/guard-policy warning for workflow: %s", markdownPath)
	compiler.IncrementWarningCount()
	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", githubLockdownGuardPolicyWarningMessage))
}

const githubMinIntegrityNoneBashWarningMessage = `'tools.github.min-integrity' is set to 'none' without an explicit 'tools.bash' setting. ` +
	`External users may execute arbitrary commands in the sandbox. ` +
	`Set 'tools.bash' explicitly to acknowledge shell access (e.g. 'bash: ["cat", "ls", "grep"]' for read-only commands).`

// emitMinIntegrityNoneBashWarning emits a warning when min-integrity is none and bash is not explicitly specified.
// This is called in non-strict mode (strict mode rejects this combination as an error).
func emitMinIntegrityNoneBashWarning(compiler *Compiler, tools *Tools, markdownPath string) {
	if tools == nil || tools.GitHub == nil {
		return
	}
	if tools.GitHub.MinIntegrity != GitHubIntegrityNone {
		return
	}
	// Check if bash is explicitly specified (Bash field is non-nil)
	if tools.Bash != nil {
		return
	}

	if compiler == nil {
		return
	}

	toolsValidationLog.Printf("Emitting min-integrity: none bash warning for workflow: %s", markdownPath)
	compiler.IncrementWarningCount()
	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", githubMinIntegrityNoneBashWarningMessage))
}

// validateGitHubGuardPolicy validates the GitHub guard policy configuration.
// Guard policy fields (allowed-repos, min-integrity) are specified flat under github:.
// Note: 'repos' is a deprecated alias for 'allowed-repos'.
// If allowed-repos (or deprecated alias repos) is not specified but min-integrity is, allowed-repos defaults to "all".
func validateGitHubGuardPolicy(tools *Tools, workflowName string) error {
	if tools == nil || tools.GitHub == nil {
		return nil
	}

	github := tools.GitHub
	if github.reposParseErr != nil {
		return github.reposParseErr
	}
	if hasGitHubLockdownGuardPolicyConflict(github) {
		toolsValidationLog.Printf("lockdown enabled with guard policy fields in workflow: %s", workflowName)
	}

	// AllowedRepos is populated from either 'allowed-repos' (preferred) or deprecated
	// 'repos' during parsing. Normalize the deprecated alias here as well so that
	// configurations built programmatically (bypassing the parser) are validated
	// identically to parsed frontmatter.
	if github.AllowedRepos == nil && github.Repos != nil {
		toolsValidationLog.Printf("Normalizing deprecated 'repos' alias to 'allowed-repos' in workflow: %s", workflowName)
		github.AllowedRepos = github.Repos
	}
	hasRepos := github.AllowedRepos != nil
	hasMinIntegrity := github.MinIntegrity != ""
	// blocked-users / approval-labels / trusted-users can be an array or a
	// GitHub Actions expression string.
	hasBlockedUsers := len(github.BlockedUsers) > 0 || github.BlockedUsersExpr != ""
	hasApprovalLabels := len(github.ApprovalLabels) > 0 || github.ApprovalLabelsExpr != ""
	hasTrustedUsers := len(github.TrustedUsers) > 0 || github.TrustedUsersExpr != ""

	// blocked-users, trusted-users, and approval-labels require a guard policy (min-integrity)
	if (hasBlockedUsers || hasApprovalLabels || hasTrustedUsers) && !hasMinIntegrity {
		toolsValidationLog.Printf("blocked-users/trusted-users/approval-labels without guard policy in workflow: %s", workflowName)
		return errors.New("'github.blocked-users', 'github.trusted-users', and 'github.approval-labels' require 'github.min-integrity' to be set. Example:\ntools:\n  github:\n    min-integrity: approved\n    blocked-users: [\"spammer\"]")
	}

	// No guard policy fields present - nothing to validate
	if !hasRepos && !hasMinIntegrity {
		return nil
	}

	// Default allowed-repos to "all" when not specified
	if !hasRepos {
		toolsValidationLog.Printf("Defaulting allowed-repos (repos) to 'all' in guard policy for workflow: %s", workflowName)
		github.AllowedRepos = GitHubReposScope{"all"}
	}

	// Validate repos format
	if err := validateReposScope(github.AllowedRepos, workflowName); err != nil {
		return err
	}

	// Validate min-integrity field (required when repos is set)
	if !hasMinIntegrity {
		toolsValidationLog.Printf("Missing min-integrity in guard policy for workflow: %s", workflowName)
		return errors.New("'github.min-integrity' is required when 'github.allowed-repos' is set. Valid values: 'none', 'unapproved', 'approved', 'merged'. Example:\ntools:\n  github:\n    allowed-repos: all\n    min-integrity: approved")
	}

	if err := validateGuardMinIntegrity(github.MinIntegrity, workflowName); err != nil {
		return err
	}
	return validateGuardPolicyUserEntries(github, workflowName)
}

func validateGuardMinIntegrity(integrity GitHubIntegrityLevel, workflowName string) error {
	validIntegrityLevels := map[GitHubIntegrityLevel]bool{
		GitHubIntegrityNone:       true,
		GitHubIntegrityUnapproved: true,
		GitHubIntegrityApproved:   true,
		GitHubIntegrityMerged:     true,
	}

	if !validIntegrityLevels[integrity] {
		toolsValidationLog.Printf("Invalid min-integrity level '%s' in workflow: %s", integrity, workflowName)
		return errors.New("'github.min-integrity' must be one of: 'none', 'unapproved', 'approved', 'merged'. Got: '" + string(integrity) + "'. Example:\ntools:\n  github:\n    min-integrity: approved")
	}
	return nil
}

func validateGuardPolicyUserEntries(github *GitHubToolConfig, workflowName string) error {
	// Validate blocked-users (must be non-empty strings; expressions are accepted as-is)
	for i, user := range github.BlockedUsers {
		if user == "" {
			toolsValidationLog.Printf("Empty blocked-users entry at index %d in workflow: %s", i, workflowName)
			return errors.New("'github.blocked-users' entries must not be empty strings. Example:\ntools:\n  github:\n    blocked-users: [\"spammer\"]")
		}
	}

	// Validate approval-labels (must be non-empty strings; expressions are accepted as-is)
	for i, label := range github.ApprovalLabels {
		if label == "" {
			toolsValidationLog.Printf("Empty approval-labels entry at index %d in workflow: %s", i, workflowName)
			return errors.New("'github.approval-labels' entries must not be empty strings. Example:\ntools:\n  github:\n    approval-labels: [\"approved\"]")
		}
	}

	// Validate trusted-users (must be non-empty strings; expressions are accepted as-is)
	for i, user := range github.TrustedUsers {
		if user == "" {
			toolsValidationLog.Printf("Empty trusted-users entry at index %d in workflow: %s", i, workflowName)
			return errors.New("'github.trusted-users' entries must not be empty strings. Example:\ntools:\n  github:\n    trusted-users: [\"octocat\"]")
		}
	}

	return nil
}

// validateReposScope validates the repos field in the guard policy
func validateReposScope(repos GitHubReposScope, workflowName string) error {
	if len(repos) == 0 {
		toolsValidationLog.Printf("Empty repos array in workflow: %s", workflowName)
		return errors.New("'github.allowed-repos' array cannot be empty. Provide at least one repository pattern. Example:\ntools:\n  github:\n    allowed-repos: [\"owner/repo\"]")
	}

	if len(repos) == 1 && (repos[0] == "all" || repos[0] == "public" || isExactGitHubRepositoryExpression(repos[0])) {
		return nil
	}

	for _, pattern := range repos {
		if err := validateRepoPattern(pattern, workflowName); err != nil {
			return err
		}
	}
	return nil
}

// validateRepoPattern validates a single repository pattern
func validateRepoPattern(pattern string, workflowName string) error {
	if isExactGitHubRepositoryExpression(pattern) {
		return nil
	}

	// Pattern must be lowercase
	if strings.ToLower(pattern) != pattern {
		toolsValidationLog.Printf("Repository pattern '%s' is not lowercase in workflow: %s", pattern, workflowName)
		return errors.New("repository pattern '" + pattern + "' must be lowercase. Example: 'owner/repo' instead of 'Owner/Repo'")
	}

	// Check for valid pattern formats:
	// 1. owner/repo (exact match)
	// 2. owner/* (owner wildcard)
	// 3. owner/re* (repository prefix wildcard)
	parts := strings.Split(pattern, "/")
	if len(parts) != 2 {
		toolsValidationLog.Printf("Invalid repository pattern '%s' in workflow: %s", pattern, workflowName)
		return errors.New("repository pattern '" + pattern + "' must be in format 'owner/repo', 'owner/*', or 'owner/prefix*'. Example: 'owner/repo'")
	}

	owner := parts[0]
	repo := parts[1]

	// Validate owner part (must be non-empty and contain only valid characters)
	if owner == "" {
		return errors.New("repository pattern '" + pattern + "' has an empty owner. Expected 'owner/repo' format. Example: 'owner/repo'")
	}

	if !isValidOwnerOrRepo(owner) {
		return errors.New("repository pattern '" + pattern + "' has an unsupported owner. Expected only lowercase letters, numbers, hyphens, and underscores. Example: 'owner/repo'")
	}

	// Validate repo part
	if repo == "" {
		return errors.New("repository pattern '" + pattern + "' has an empty repository name. Expected 'owner/repo' format. Example: 'owner/repo'")
	}

	// Allow wildcard '*' or prefix with trailing '*'
	if repo != "*" && !isValidOwnerOrRepo(strings.TrimSuffix(repo, "*")) {
		return errors.New("repository pattern '" + pattern + "' has an unsupported repository name. Expected only lowercase letters, numbers, hyphens, underscores, or a wildcard like '*' or 'prefix*'. Example: 'owner/repo' or 'owner/prefix*'")
	}

	// Validate that wildcard is only at the end (not in the middle)
	if strings.Contains(strings.TrimSuffix(repo, "*"), "*") {
		return errors.New("repository pattern '" + pattern + "' has a wildcard in the middle. Wildcards are only allowed at the end. Example: 'owner/prefix*'")
	}

	return nil
}

// isValidOwnerOrRepo checks if a string contains only valid GitHub owner/repo characters
func isValidOwnerOrRepo(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
			return false
		}
	}
	return true
}

func isExactGitHubRepositoryExpression(value string) bool {
	return value == githubRepositoryExpression
}

func normalizeGitHubRepositoryInReposScope(repos any) any {
	switch r := repos.(type) {
	case string:
		if r == "all" || r == "public" {
			return r
		}
		return []string{r}
	case []string:
		normalized := make([]string, len(r))
		for i, repo := range r {
			if isExactGitHubRepositoryExpression(repo) {
				normalized[i] = githubRepositoryExpression
				continue
			}
			normalized[i] = repo
		}
		return normalized
	case []any:
		normalized := make([]any, len(r))
		for i, repo := range r {
			if repoStr, ok := repo.(string); ok {
				if isExactGitHubRepositoryExpression(repoStr) {
					normalized[i] = githubRepositoryExpression
					continue
				}
				normalized[i] = repoStr
				continue
			}
			normalized[i] = repo
		}
		return normalized
	default:
		return repos
	}
}

// Note: validateGitToolForSafeOutputs was removed because git commands are automatically
// injected by the compiler when safe-outputs needs them (see compiler_safe_outputs.go).
// The validation was misleading - it would fail even though the compiler would add the
// necessary git commands during compilation.

// ValidateGitHubToolsAgainstToolsets validates that all allowed GitHub tools have their
// corresponding toolsets enabled in the configuration.
func ValidateGitHubToolsAgainstToolsets(allowedTools []string, enabledToolsets []string) error {
	return validateGitHubToolsAgainstToolsetsCore(allowedTools, enabledToolsets)
}
