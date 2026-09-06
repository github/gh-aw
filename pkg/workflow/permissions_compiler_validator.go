// This file contains the validatePermissions compiler validator, extracted from
// compiler_validators.go for single-responsibility maintenance per AGENTS.md.
//
// # Permission Validation
//
// validatePermissions orchestrates all permission-related checks in the compilation
// pipeline. It is called from validateWorkflowData (compiler_orchestrator.go) and
// returns the parsed *Permissions object for reuse in later validation steps.
//
// # Validation Sequence
//
// The following checks are performed in order:
//
//  1. Dangerous permissions — rejects globally-dangerous permission combinations.
//  2. GitHub App-only permissions — ensures a GitHub App is configured when
//     App-only permission scopes are requested.
//  3. GitHub MCP App write restriction — rejects "write" in
//     tools.github.github-app.permissions.
//  4. Unsupported github-app.permissions contexts — emits a warning when the
//     github-app.permissions field is used in a context that does not support it.
//  5. workflow_run branch restrictions — validates that workflow_run triggers carry
//     explicit branch filters to prevent untrusted-code execution.
//  6. pull_request_target security — warns (strict) or errors when checkout is not
//     disabled, because running with write permissions on untrusted PR code is a
//     critical "pwn request" vulnerability.
//  7. GitHub MCP toolset permission alignment — validates that the workflow's
//     declared permissions cover the read/write requirements of all enabled toolsets.
//  8. id-token: write permission enforcement — rejects workflows that use OIDC auth
//     (engine, HTTP MCP server, or OTLP) without `permissions.id-token: write`.
//  9. HTTP MCP OIDC AWF version gate — rejects legacy firewall.version pins when an
//     HTTP MCP server uses `auth.type: github-oidc`, because --exclude-env (required
//     to keep Actions OIDC credentials out of the agent container) is only available
//     in AWF v0.25.3+.
//  10. id-token: write warning — emits a security reminder when OIDC tokens are
//     requested, because they can be used to authenticate to cloud providers.
//
// # Strict Mode
//
// When the Compiler is running in strict mode (c.strictMode == true), missing
// permissions for GitHub MCP toolsets are promoted to hard errors, except when all
// enabled toolsets are default-only toolsets (which are downgraded back to warnings
// to avoid blocking legacy workflows that relied on automatic permission injection).
package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var permissionsCompilerLog = logger.New("workflow:permissions_compiler_validator")

// validatePermissions validates all permission-related configuration: dangerous
// permissions, GitHub App-only constraints, MCP app write restrictions, workflow_run
// branch security, GitHub MCP toolset permissions, and the id-token write warning.
// It returns the parsed *Permissions for reuse in subsequent validation steps.
func (c *Compiler) validatePermissions(workflowData *WorkflowData, markdownPath string) (*Permissions, error) {
	// Use the cached *Permissions object when available to avoid repeated YAML parsing.
	// CachedPermissions is populated by applyDefaults after all permission mutations are applied.
	// Fall back to parsing from the raw string for code paths that bypass applyDefaults
	// (e.g., tests that construct WorkflowData directly).
	var workflowPermissions *Permissions
	if workflowData.CachedPermissions != nil {
		workflowPermissions = workflowData.CachedPermissions
	} else {
		workflowPermissions = NewPermissionsParser(workflowData.Permissions).ToPermissions()
	}

	// Validate permission scope names for typos (e.g. "contnts" → "contents")
	workflowLog.Printf("Validating permission scope names")
	var scopeValidationErr error
	if workflowData.CachedPermissionScopeNamesSet {
		scopeValidationErr = workflowData.CachedPermissionScopeNamesErr
	} else {
		scopeValidationErr = ValidatePermissionScopeNames(workflowData.Permissions)
	}
	if scopeValidationErr != nil {
		return nil, formatCompilerError(markdownPath, "error", scopeValidationErr.Error(), scopeValidationErr)
	}

	// Validate dangerous permissions
	workflowLog.Printf("Validating dangerous permissions")
	if err := validateDangerousPermissions(workflowData, workflowPermissions); err != nil {
		return nil, formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate GitHub App-only permissions require a GitHub App to be configured
	workflowLog.Printf("Validating GitHub App-only permissions")
	if err := validateGitHubAppOnlyPermissions(workflowData, workflowPermissions); err != nil {
		return nil, formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Validate tools.github.github-app.permissions does not use "write"
	workflowLog.Printf("Validating GitHub MCP app permissions (no write)")
	if err := validateGitHubMCPAppPermissionsNoWrite(workflowData); err != nil {
		return nil, formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Warn when github-app.permissions is set in contexts that don't support it
	warnGitHubAppPermissionsUnsupportedContexts(workflowData)

	// Validate workflow_run triggers have branch restrictions
	workflowLog.Printf("Validating workflow_run triggers for branch restrictions")
	if err := c.validateWorkflowRunBranches(workflowData, markdownPath); err != nil {
		return nil, err
	}

	// Validate pull_request_target trigger security
	workflowLog.Printf("Validating pull_request_target trigger security")
	if err := c.validatePullRequestTargetTrigger(workflowData, markdownPath); err != nil {
		return nil, err
	}

	// Validate permissions against GitHub MCP toolsets
	workflowLog.Printf("Validating permissions for GitHub MCP toolsets")
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.GitHub != nil {
		// Check if GitHub tool was explicitly configured in frontmatter
		// If permissions exist but tools.github was NOT explicitly configured,
		// skip validation and let the GitHub MCP server handle permission issues
		hasPermissions := workflowData.Permissions != ""

		workflowLog.Printf("Permission validation check: hasExplicitGitHubTool=%v, hasPermissions=%v",
			workflowData.HasExplicitGitHubTool, hasPermissions)

		// Skip validation if permissions exist but GitHub tool was auto-added (not explicit)
		if hasPermissions && !workflowData.HasExplicitGitHubTool {
			workflowLog.Printf("Skipping permission validation: permissions exist but tools.github not explicitly configured")
		} else {
			// Validate permissions using the typed GitHub tool configuration.
			// Pass the cached parsed toolsets from applyDefaults to avoid a redundant
			// ParseGitHubToolsets call inside ValidatePermissions.
			validationResult := ValidatePermissions(workflowPermissions, workflowData.ParsedTools.GitHub, workflowData.CachedParsedToolsets)

			if validationResult.HasValidationIssues {
				// Format the validation message
				message := FormatValidationMessage(validationResult, c.strictMode)

				if len(validationResult.MissingPermissions) > 0 {
					downgradeToWarning := c.strictMode && shouldDowngradeDefaultToolsetPermissionError(workflowData.ParsedTools.GitHub)
					if c.strictMode && !downgradeToWarning {
						// In strict mode, missing permissions are errors
						return nil, formatCompilerError(markdownPath, "error", message, nil)
					}

					if downgradeToWarning {
						message += "\n\n" + missingPermissionsDefaultToolsetWarning
					}

					// Emit to stderr once per markdown path + warning fingerprint.
					// Prefer frontmatter hash when available; otherwise use the formatted
					// message as a fallback fingerprint for code paths/tests where the hash
					// is not set.
					warningFingerprint := workflowData.FrontmatterHash
					if warningFingerprint == "" {
						warningFingerprint = message
					}
					if c.permissionWarningShown[markdownPath] != warningFingerprint {
						// In non-strict mode, missing permissions are warnings.
						// In strict mode with default-only toolsets, this is intentionally downgraded to warning.
						fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
						c.permissionWarningShown[markdownPath] = warningFingerprint
					}
					c.IncrementWarningCount()
				}
			}
		}
	}

	// Enforce required id-token: write permission for OIDC auth users.
	if err := validateOIDCPermissions(workflowData, workflowPermissions); err != nil {
		return nil, formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Reject legacy AWF pins when HTTP MCP OIDC is configured: --exclude-env is required to
	// keep Actions OIDC credentials out of the agent container, and that flag only exists in
	// AWF v0.25.3+. Compilation must fail rather than silently expose the credentials.
	if err := validateHTTPMCPOIDCAwfVersion(workflowData); err != nil {
		return nil, formatCompilerError(markdownPath, "error", err.Error(), err)
	}
	if err := validateCodexCopilotAwfVersion(workflowData); err != nil {
		return nil, formatCompilerError(markdownPath, "error", err.Error(), err)
	}

	// Emit warning if id-token: write permission is detected
	workflowLog.Printf("Checking for id-token: write permission")
	if level, exists := workflowPermissions.Get(PermissionIdToken); exists && level == PermissionWrite {
		warningMsg := `This workflow grants id-token: write permission
OIDC tokens can authenticate to cloud providers (AWS, Azure, GCP).
Ensure proper audience validation and trust policies are configured.`
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", warningMsg))
		c.IncrementWarningCount()
	}
	if !c.quiet && shouldEmitCopilotRequestsEnableTip(workflowData, workflowPermissions) && !c.repositoryOwnerIsIndividualUser() {
		if !c.copilotRequestsTipShown[markdownPath] {
			if c.batchMode {
				c.copilotTipNeeded = true
			} else {
				tipMsg := `Tip: set permissions.copilot-requests: write to use GitHub Actions token-based inference with the Copilot engine instead of a personal access token (COPILOT_GITHUB_TOKEN). This option requires that your organization has centralized Copilot billing enabled and may not be available in all organizations. To suppress this tip when using a PAT, set permissions.copilot-requests: none — see https://github.github.com/gh-aw/reference/billing/ for details.`
				fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "info", tipMsg))
			}
			c.copilotRequestsTipShown[markdownPath] = true
		}
	}

	return workflowPermissions, nil
}

// repositoryOwnerIsIndividualUser reports whether the repository owner is confirmed
// to be an individual user account (as opposed to an organization).
//
// It returns true only when the GitHub API confirms the owner type is "User". It
// returns false when the owner is an organization, when no repository slug is set, or
// when the API call fails (e.g. no authentication, network error). This fail-safe
// default ensures the copilot-requests tip continues to be shown whenever the owner
// type cannot be determined, preserving prior behavior.
func (c *Compiler) repositoryOwnerIsIndividualUser() bool {
	slug := c.repositorySlug
	owner, repo, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || repo == "" {
		workflowLog.Printf("Skipping owner-type check: slug %q is not in owner/repo format", slug)
		return false
	}

	ownerType, cached := c.ownerTypeCache[owner]
	if !cached {
		workflowLog.Printf("Checking owner type for: %s", owner)
		output, err := RunGH("Checking repository owner type...", "api", "/users/"+owner, "--jq", ".type")
		if err != nil {
			workflowLog.Printf("Could not determine owner type for %q: %v", owner, err)
			// Cache the empty string so subsequent calls for the same owner also return false
			// without retrying. This is intentional: fail-safe means "show the tip when uncertain"
			// and avoids N retry round-trips per run.
			c.ownerTypeCache[owner] = ""
			return false
		}
		ownerType = strings.TrimSpace(string(output))
		c.ownerTypeCache[owner] = ownerType
		workflowLog.Printf("Owner type for %q: %s", owner, ownerType)
	} else {
		permissionsCompilerLog.Printf("Owner type cache hit: owner=%s type=%q", owner, ownerType)
	}
	return ownerType == "User"
}

func shouldEmitCopilotRequestsEnableTip(workflowData *WorkflowData, workflowPermissions *Permissions) bool {
	if workflowData == nil || workflowPermissions == nil {
		return false
	}
	if workflowData.EngineConfig == nil || workflowData.EngineConfig.ID != "copilot" {
		return false
	}
	if level, exists := workflowPermissions.GetExplicit(PermissionCopilotRequests); exists && level == PermissionNone {
		return false
	}
	return !workflowPermissions.HasCopilotRequestsWrite()
}

func validateOIDCPermissions(workflowData *WorkflowData, workflowPermissions *Permissions) error {
	if workflowData == nil {
		return nil
	}

	requiresIDTokenWrite := false
	errorPrefix := ""

	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Auth != nil && workflowData.EngineConfig.Auth.Type == "github-oidc" {
		requiresIDTokenWrite = true
		errorPrefix = "engine.auth.type: github-oidc"
	}

	if !requiresIDTokenWrite && hasGitHubOIDCAuthInTools(workflowData.Tools) {
		requiresIDTokenWrite = true
		errorPrefix = "mcp-servers.<name>.auth.type: github-oidc"
	}

	// observability.otlp.workload-identity does not require a user-declared permission:
	// ensureOTLPOIDCJobPermissions grants id-token: write to every job that mints the token.
	if !requiresIDTokenWrite && getOTLPWorkloadIdentity(workflowData.ParsedFrontmatter, workflowData.RawFrontmatter) == nil &&
		hasOTLPGitHubOIDCAuth(workflowData.ParsedFrontmatter, workflowData.RawFrontmatter) {
		requiresIDTokenWrite = true
		errorPrefix = "observability.otlp.github-app"
	}

	if !requiresIDTokenWrite {
		return nil
	}

	permissionsCompilerLog.Printf("OIDC permission check: requiresIDTokenWrite=true prefix=%q", errorPrefix)

	if workflowPermissions == nil {
		return errors.New(errorPrefix + " requires permissions.id-token: write")
	}

	if level, exists := workflowPermissions.Get(PermissionIdToken); !exists || level != PermissionWrite {
		return errors.New(errorPrefix + " requires permissions.id-token: write")
	}

	return nil
}

// validateHTTPMCPOIDCAwfVersion rejects workflows that configure HTTP MCP OIDC auth on an AWF
// version that predates --exclude-env support (v0.25.3+). Without --exclude-env, the
// Actions OIDC token request URL and token are visible to the agent container, breaking the
// runner→gateway-only credential boundary. Compilation must fail so the exposure is never
// silently accepted.
func validateHTTPMCPOIDCAwfVersion(workflowData *WorkflowData) error {
	if workflowData == nil {
		return nil
	}
	if !hasGitHubOIDCAuthInTools(workflowData.Tools) {
		return nil
	}
	firewallConfig := getFirewallConfig(workflowData)
	if awfVersionAtLeast(firewallConfig, constants.AWFExcludeEnvMinVersion) {
		return nil
	}
	effectiveVersion := string(constants.DefaultFirewallVersion)
	if firewallConfig != nil && firewallConfig.Version != "" {
		effectiveVersion = firewallConfig.Version
	}
	return fmt.Errorf(
		"mcp-servers.<name>.auth.type: github-oidc requires AWF %s or newer to keep Actions OIDC credentials out of the agent container.\n\nThe effective AWF version is %s. Set firewall.version to %s or newer",
		constants.AWFExcludeEnvMinVersion,
		effectiveVersion,
		constants.AWFExcludeEnvMinVersion,
	)
}

func validateCodexCopilotAwfVersion(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.EngineConfig == nil ||
		workflowData.EngineConfig.ID != string(constants.CodexEngine) ||
		NewCodexEngine().ResolveLLMProvider(workflowData) != LLMProviderGitHub {
		return nil
	}
	firewallConfig := getFirewallConfig(workflowData)
	if awfSupportsExcludeEnv(firewallConfig) {
		return nil
	}
	effectiveVersion := string(constants.DefaultFirewallVersion)
	if firewallConfig != nil && firewallConfig.Version != "" {
		effectiveVersion = firewallConfig.Version
	}
	return fmt.Errorf(
		"codex with the GitHub provider requires AWF %s or newer to keep GitHub credentials out of the agent container.\n\nThe effective AWF version is %s. Set firewall.version to %s or newer",
		constants.AWFExcludeEnvMinVersion,
		effectiveVersion,
		constants.AWFExcludeEnvMinVersion,
	)
}
