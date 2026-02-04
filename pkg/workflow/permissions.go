package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var permissionsLog = logger.New("workflow:permissions")

// convertStringToPermissionScope converts a string key to a PermissionScope
func convertStringToPermissionScope(key string) PermissionScope {
	switch key {
	case "actions":
		return PermissionActions
	case "attestations":
		return PermissionAttestations
	case "checks":
		return PermissionChecks
	case "contents":
		return PermissionContents
	case "deployments":
		return PermissionDeployments
	case "discussions":
		return PermissionDiscussions
	case "id-token":
		return PermissionIdToken
	case "issues":
		return PermissionIssues
	case "metadata":
		return PermissionMetadata
	case "models":
		return PermissionModels
	case "packages":
		return PermissionPackages
	case "pages":
		return PermissionPages
	case "pull-requests":
		return PermissionPullRequests
	case "repository-projects":
		return PermissionRepositoryProj
	case "organization-projects":
		return PermissionOrganizationProj
	case "security-events":
		return PermissionSecurityEvents
	case "statuses":
		return PermissionStatuses
	default:
		return ""
	}
}

// ContainsCheckout returns true if the given custom steps contain an actions/checkout step
func ContainsCheckout(customSteps string) bool {
	if customSteps == "" {
		return false
	}

	// Look for actions/checkout usage patterns
	checkoutPatterns := []string{
		"actions/checkout@",
		"uses: actions/checkout",
		"- uses: actions/checkout",
	}

	lowerSteps := strings.ToLower(customSteps)
	for _, pattern := range checkoutPatterns {
		if strings.Contains(lowerSteps, strings.ToLower(pattern)) {
			permissionsLog.Print("Detected actions/checkout in custom steps")
			return true
		}
	}

	return false
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
	PermissionActions          PermissionScope = "actions"
	PermissionAttestations     PermissionScope = "attestations"
	PermissionChecks           PermissionScope = "checks"
	PermissionContents         PermissionScope = "contents"
	PermissionDeployments      PermissionScope = "deployments"
	PermissionDiscussions      PermissionScope = "discussions"
	PermissionIdToken          PermissionScope = "id-token"
	PermissionIssues           PermissionScope = "issues"
	PermissionMetadata         PermissionScope = "metadata"
	PermissionModels           PermissionScope = "models"
	PermissionPackages         PermissionScope = "packages"
	PermissionPages            PermissionScope = "pages"
	PermissionPullRequests     PermissionScope = "pull-requests"
	PermissionRepositoryProj   PermissionScope = "repository-projects"
	PermissionOrganizationProj PermissionScope = "organization-projects"
	PermissionSecurityEvents   PermissionScope = "security-events"
	PermissionStatuses         PermissionScope = "statuses"
)

// GetAllPermissionScopes returns all available permission scopes
func GetAllPermissionScopes() []PermissionScope {
	return []PermissionScope{
		PermissionActions,
		PermissionAttestations,
		PermissionChecks,
		PermissionContents,
		PermissionDeployments,
		PermissionDiscussions,
		PermissionIdToken,
		PermissionIssues,
		PermissionMetadata,
		PermissionModels,
		PermissionPackages,
		PermissionPages,
		PermissionPullRequests,
		PermissionRepositoryProj,
		PermissionOrganizationProj,
		PermissionSecurityEvents,
		PermissionStatuses,
	}
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
