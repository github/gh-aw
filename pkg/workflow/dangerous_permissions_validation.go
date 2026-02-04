package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var dangerousPermissionsLog = logger.New("workflow:dangerous_permissions_validation")

// validateDangerousPermissions validates that write permissions are not used unless
// the dangerous-permissions-write feature flag is enabled.
//
// This validation applies to:
// - Top-level workflow permissions
//
// This validation does NOT apply to:
// - Custom jobs (jobs defined in the jobs: section)
// - Safe outputs jobs (jobs defined in safe-outputs.job section)
//
// Returns an error if write permissions are found without the feature flag enabled.
func validateDangerousPermissions(workflowData *WorkflowData) error {
	dangerousPermissionsLog.Print("Starting dangerous permissions validation")

	// Check if the feature flag is enabled
	featureEnabled := isFeatureEnabled(constants.DangerousPermissionsWriteFeatureFlag, workflowData)
	if featureEnabled {
		dangerousPermissionsLog.Print("dangerous-permissions-write feature flag is enabled, allowing write permissions")
		return nil
	}

	// Parse the top-level workflow permissions
	if workflowData.Permissions == "" {
		dangerousPermissionsLog.Print("No permissions defined, validation passed")
		return nil
	}

	permissions := NewPermissionsParser(workflowData.Permissions).ToPermissions()
	if permissions == nil {
		dangerousPermissionsLog.Print("Could not parse permissions, validation passed")
		return nil
	}

	// Check for write permissions
	writePermissions := findWritePermissions(permissions)
	if len(writePermissions) > 0 {
		dangerousPermissionsLog.Printf("Found %d write permissions without feature flag", len(writePermissions))
		return formatDangerousPermissionsError(writePermissions)
	}

	dangerousPermissionsLog.Print("No write permissions found, validation passed")
	return nil
}

// findWritePermissions returns a list of permission scopes that have write access
// Excludes id-token since it's safe (used for OIDC authentication) and doesn't modify repository content
// Excludes metadata since it's a built-in read-only permission
func findWritePermissions(permissions *Permissions) []PermissionScope {
	if permissions == nil {
		return nil
	}

	var writePerms []PermissionScope

	// Check all permission scopes
	for _, scope := range GetAllPermissionScopes() {
		// Skip id-token as it's safe and used for OIDC authentication
		if scope == PermissionIdToken {
			continue
		}

		// Skip metadata as it's a built-in read-only permission
		if scope == PermissionMetadata {
			continue
		}

		level, exists := permissions.Get(scope)
		if exists && level == PermissionWrite {
			writePerms = append(writePerms, scope)
		}
	}

	return writePerms
}

// formatDangerousPermissionsError formats an error message for write permissions violations
func formatDangerousPermissionsError(writePermissions []PermissionScope) error {
	var lines []string
	lines = append(lines, "Write permissions are not allowed.")
	lines = append(lines, "")
	lines = append(lines, "Found write permissions:")
	for _, scope := range writePermissions {
		lines = append(lines, fmt.Sprintf("  - %s: write", scope))
	}
	lines = append(lines, "")
	lines = append(lines, "To fix this issue, change write permissions to read:")
	lines = append(lines, "permissions:")
	for _, scope := range writePermissions {
		lines = append(lines, fmt.Sprintf("  %s: read", scope))
	}

	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}
