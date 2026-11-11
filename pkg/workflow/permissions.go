package workflow

import (
	"slices"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var permissionsLog = logger.New("workflow:permissions")

var validPermissionScopes = func() map[string]struct{} {
	scopes := GetAllPermissionScopes()
	appOnlyScopes := GetAllGitHubAppOnlyScopes()

	m := make(map[string]struct{}, typeutil.SafeAllocationCapacity(len(scopes), len(appOnlyScopes), 1))
	for _, scope := range scopes {
		m[string(scope)] = struct{}{}
	}
	for _, scope := range appOnlyScopes {
		m[string(scope)] = struct{}{}
	}
	// copilot-requests is intentionally excluded from GetAllPermissionScopes()
	// because read-all should not grant this write-capable scope, but it must still
	// be recognized when explicitly set in frontmatter.
	m[string(PermissionCopilotRequests)] = struct{}{}

	return m
}()

// convertStringToPermissionScope converts a string key to a PermissionScope
func convertStringToPermissionScope(key string) PermissionScope {
	if key == "all" {
		// "all" is a meta-key handled at the parser level; it is not a real scope
		return ""
	}

	if _, exists := validPermissionScopes[key]; !exists {
		permissionsLog.Printf("Unknown permission scope key: %s", key)
		return ""
	}

	return PermissionScope(key)
}

// PermissionLevel represents the level of access (read, write, none)
type PermissionLevel string

const (
	PermissionRead  PermissionLevel = "read"
	PermissionWrite PermissionLevel = "write"
	PermissionNone  PermissionLevel = "none"
)

// PermissionScope represents a GitHub Actions permission scope
type PermissionScope string

const (
	// GitHub Actions permission scopes (supported by GITHUB_TOKEN), except
	// organization-projects which is declared here for historical grouping but
	// treated as GitHub App-only by GetAllGitHubAppOnlyScopes/IsGitHubAppOnlyScope.
	PermissionActions             PermissionScope = "actions"
	PermissionAttestations        PermissionScope = "attestations"
	PermissionChecks              PermissionScope = "checks"
	PermissionCodeQuality         PermissionScope = "code-quality"
	PermissionContents            PermissionScope = "contents"
	PermissionDeployments         PermissionScope = "deployments"
	PermissionDiscussions         PermissionScope = "discussions"
	PermissionDrives              PermissionScope = "drives"
	PermissionIdToken             PermissionScope = "id-token"
	PermissionIssues              PermissionScope = "issues"
	PermissionMetadata            PermissionScope = "metadata"
	PermissionModels              PermissionScope = "models"
	PermissionPackages            PermissionScope = "packages"
	PermissionPages               PermissionScope = "pages"
	PermissionPullRequests        PermissionScope = "pull-requests"
	PermissionRepositoryProj      PermissionScope = "repository-projects"
	PermissionSecurityEvents      PermissionScope = "security-events"
	PermissionStatuses            PermissionScope = "statuses"
	PermissionVulnerabilityAlerts PermissionScope = "vulnerability-alerts"

	// PermissionOrganizationProj is declared here for constant grouping but is treated as
	// GitHub App-only at runtime (excluded from GetAllPermissionScopes(), included in
	// GetAllGitHubAppOnlyScopes() and IsGitHubAppOnlyScope).
	PermissionOrganizationProj PermissionScope = "organization-projects"
	// PermissionCopilotRequests is a GitHub Actions permission scope that enables
	// use of the GitHub Actions token as the Copilot authentication token.
	PermissionCopilotRequests PermissionScope = "copilot-requests"

	// GitHub App-only permission scopes (not supported by GITHUB_TOKEN, require a GitHub App token).
	// When any of these are specified in the workflow permissions, a GitHub App must be configured.
	// These permissions are skipped when rendering GitHub Actions workflow YAML, but are passed
	// as permission-* inputs when minting GitHub App installation access tokens.

	// Repository-level GitHub App permissions
	PermissionAdministration             PermissionScope = "administration"
	PermissionEnvironments               PermissionScope = "environments"
	PermissionGitSigning                 PermissionScope = "git-signing"
	PermissionWorkflows                  PermissionScope = "workflows"
	PermissionRepositoryHooks            PermissionScope = "repository-hooks"
	PermissionSingleFile                 PermissionScope = "single-file"
	PermissionCodespaces                 PermissionScope = "codespaces"
	PermissionRepositoryCustomProperties PermissionScope = "repository-custom-properties"
	PermissionSecretScanningAlerts       PermissionScope = "secret-scanning-alerts"
	PermissionRepositoryAdvisories       PermissionScope = "repository-advisories"

	// Organization-level GitHub App permissions
	PermissionMembers                             PermissionScope = "members"
	PermissionOrganizationAdministration          PermissionScope = "organization-administration"
	PermissionTeamDiscussions                     PermissionScope = "team-discussions"
	PermissionOrganizationHooks                   PermissionScope = "organization-hooks"
	PermissionOrganizationMembers                 PermissionScope = "organization-members"
	PermissionOrganizationPackages                PermissionScope = "organization-packages"
	PermissionOrganizationSelfHostedRunners       PermissionScope = "organization-self-hosted-runners"
	PermissionOrganizationCustomOrgRoles          PermissionScope = "organization-custom-org-roles"
	PermissionOrganizationCustomProperties        PermissionScope = "organization-custom-properties"
	PermissionOrganizationCustomRepositoryRoles   PermissionScope = "organization-custom-repository-roles"
	PermissionOrganizationAnnouncementBanners     PermissionScope = "organization-announcement-banners"
	PermissionOrganizationEvents                  PermissionScope = "organization-events"
	PermissionOrganizationPlan                    PermissionScope = "organization-plan"
	PermissionOrganizationUserBlocking            PermissionScope = "organization-user-blocking"
	PermissionOrganizationPersonalAccessTokenReqs PermissionScope = "organization-personal-access-token-requests"
	PermissionOrganizationPersonalAccessTokens    PermissionScope = "organization-personal-access-tokens"
	PermissionOrganizationCopilot                 PermissionScope = "organization-copilot"
	PermissionOrganizationCodespaces              PermissionScope = "organization-codespaces"

	// User-level GitHub App permissions
	PermissionEmailAddresses           PermissionScope = "email-addresses"
	PermissionCodespacesLifecycleAdmin PermissionScope = "codespaces-lifecycle-admin"
	PermissionCodespacesMetadata       PermissionScope = "codespaces-metadata"
)

// GetAllPermissionScopes returns all GitHub Actions permission scopes (supported by GITHUB_TOKEN).
// These are the scopes that can be set on the workflow's GITHUB_TOKEN.
// For GitHub App-only scopes, see GetAllGitHubAppOnlyScopes.
func GetAllPermissionScopes() []PermissionScope {
	return []PermissionScope{
		PermissionActions,
		PermissionAttestations,
		PermissionChecks,
		PermissionCodeQuality,
		PermissionContents,
		PermissionDeployments,
		PermissionDiscussions,
		PermissionDrives,
		PermissionIdToken,
		PermissionIssues,
		PermissionMetadata,
		PermissionModels,
		PermissionPackages,
		PermissionPages,
		PermissionPullRequests,
		PermissionRepositoryProj,
		PermissionSecurityEvents,
		PermissionStatuses,
		PermissionVulnerabilityAlerts,
	}
}

// GetAllGitHubAppOnlyScopes returns all GitHub App-only permission scopes.
// These scopes are not supported by GITHUB_TOKEN and require a GitHub App installation token.
// When any of these scopes are used in a workflow, a GitHub App must be configured.
func GetAllGitHubAppOnlyScopes() []PermissionScope {
	return []PermissionScope{
		// Repository-level GitHub App permissions
		PermissionAdministration,
		PermissionEnvironments,
		PermissionGitSigning,
		PermissionWorkflows,
		PermissionRepositoryHooks,
		PermissionSingleFile,
		PermissionCodespaces,
		PermissionRepositoryCustomProperties,
		PermissionSecretScanningAlerts,
		PermissionRepositoryAdvisories,
		// Organization-level GitHub App permissions
		PermissionOrganizationProj,
		PermissionMembers,
		PermissionOrganizationAdministration,
		PermissionTeamDiscussions,
		PermissionOrganizationHooks,
		PermissionOrganizationMembers,
		PermissionOrganizationPackages,
		PermissionOrganizationSelfHostedRunners,
		PermissionOrganizationCustomOrgRoles,
		PermissionOrganizationCustomProperties,
		PermissionOrganizationCustomRepositoryRoles,
		PermissionOrganizationAnnouncementBanners,
		PermissionOrganizationEvents,
		PermissionOrganizationPlan,
		PermissionOrganizationUserBlocking,
		PermissionOrganizationPersonalAccessTokenReqs,
		PermissionOrganizationPersonalAccessTokens,
		PermissionOrganizationCopilot,
		PermissionOrganizationCodespaces,
		// User-level GitHub App permissions
		PermissionEmailAddresses,
		PermissionCodespacesLifecycleAdmin,
		PermissionCodespacesMetadata,
	}
}

// IsGitHubAppOnlyScope returns true if the scope is a GitHub App-only permission
// (not supported by GITHUB_TOKEN). These scopes require a GitHub App to exercise.
func IsGitHubAppOnlyScope(scope PermissionScope) bool {
	isAppOnly := slices.Contains(GetAllGitHubAppOnlyScopes(), scope)
	if isAppOnly {
		permissionsLog.Printf("Scope %q requires GitHub App (not supported by GITHUB_TOKEN)", scope)
	}
	return isAppOnly
}

// Permissions represents GitHub Actions permissions
// It can be a shorthand (read-all, write-all, read, write, none) or a map of scopes to levels
// It can also have an "all" permission that expands to all scopes
type Permissions struct {
	shorthand     string
	permissions   map[PermissionScope]PermissionLevel
	hasAll        bool
	allLevel      PermissionLevel
	explicitEmpty bool // When true, renders "permissions: {}" even if no permissions are set
}
