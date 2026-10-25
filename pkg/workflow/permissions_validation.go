package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/goccy/go-yaml"
)

var allPermissionScopeNames = sync.OnceValue(func() []string {
	ghTokenScopes := GetAllPermissionScopes()
	appOnlyScopes := GetAllGitHubAppOnlyScopes()
	// +1 for copilot-requests which is not in GetAllPermissionScopes
	all := make([]string, 0, typeutil.SafeAllocationCapacity(len(ghTokenScopes), len(appOnlyScopes), 1))
	for _, scope := range ghTokenScopes {
		all = append(all, string(scope))
	}
	for _, scope := range appOnlyScopes {
		all = append(all, string(scope))
	}
	// copilot-requests is valid even though not in GetAllPermissionScopes
	all = append(all, string(PermissionCopilotRequests))
	return all
})

// getAllPermissionScopeNames returns all valid permission scope names for fuzzy matching.
// The values are computed once and cached across calls.
func getAllPermissionScopeNames() []string {
	return slices.Clone(allPermissionScopeNames())
}

// validPermissionMetaKeys is the set of meta-keys accepted in permissions shorthand contexts.
// Defined once at package level to avoid per-call map allocation.
var validPermissionMetaKeys = map[string]struct{}{
	"all":       {},
	"read-all":  {},
	"write-all": {},
	"none":      {},
}

// PermissionsValidationResult contains the result of permissions validation
type PermissionsValidationResult struct {
	MissingPermissions    map[PermissionScope]PermissionLevel // Permissions required but not granted
	ReadOnlyMode          bool                                // Whether the GitHub MCP is in read-only mode
	HasValidationIssues   bool                                // Whether there are any validation issues
	MissingToolsetDetails map[string][]PermissionScope        // Maps toolset name to missing permissions
}

// ValidatePermissions validates that workflow permissions match the required GitHub MCP toolsets
// This is the general-purpose permission validator used during workflow compilation to check
// that the declared permissions are sufficient for the GitHub MCP toolsets being used.
//
// Parameters:
//   - permissions: The workflow's declared permissions
//   - githubTool: The GitHub tool configuration implementing ValidatableTool interface
//   - parsedToolsets: optional pre-parsed toolsets slice; when provided it is used directly
//     instead of calling ParseGitHubToolsets(githubTool.GetToolsets()). Pass nil or omit to
//     let ValidatePermissions derive the toolsets from the tool configuration.
//
// Returns:
//   - A validation result indicating any missing permissions and which toolsets require them
//
// Use ValidatePermissions (this function) for general permission validation against GitHub MCP toolsets.
// Use ValidateIncludedPermissions (in imports.go) when validating permissions from included/imported workflow files.
func ValidatePermissions(permissions *Permissions, githubTool ValidatableTool, parsedToolsets ...[]string) *PermissionsValidationResult {
	if permissionsValidationLog.Enabled() {
		permissionsValidationLog.Print("Starting permissions validation")
	}

	// MissingPermissions and MissingToolsetDetails are lazily initialized by
	// checkMissingPermissions to avoid heap allocations on the happy path
	// (no missing permissions). Callers that read these fields get nil maps,
	// which behave like empty maps for reads (len, range, index) but must not
	// be written to outside of checkMissingPermissions.
	result := &PermissionsValidationResult{}

	// If GitHub tool is not configured, no validation needed
	// Check both for nil interface and nil concrete type
	if githubTool == nil {
		if permissionsValidationLog.Enabled() {
			permissionsValidationLog.Print("No GitHub tool configured (nil interface), skipping validation")
		}
		return result
	}

	// Check if concrete type is nil (interface wrapping nil pointer)
	if config, ok := githubTool.(*GitHubToolConfig); ok && config == nil {
		if permissionsValidationLog.Enabled() {
			permissionsValidationLog.Print("No GitHub tool configured (nil concrete type), skipping validation")
		}
		return result
	}

	readOnly := githubTool.IsReadOnly()
	result.ReadOnlyMode = readOnly

	toolsets := permissionValidationToolsets(githubTool, readOnly, parsedToolsets)
	if len(toolsets) == 0 {
		if permissionsValidationLog.Enabled() {
			permissionsValidationLog.Print("No toolsets to validate")
		}
		return result
	}
	requiredPermissions := collectRequiredPermissions(toolsets, readOnly)
	if permissionsValidationLog.Enabled() {
		permissionsValidationLog.Printf("Required permissions: %v", requiredPermissions)
	}
	checkMissingPermissions(permissions, requiredPermissions, toolsets, result)
	result.HasValidationIssues = len(result.MissingPermissions) > 0
	if permissionsValidationLog.Enabled() {
		permissionsValidationLog.Printf("Validation complete: hasIssues=%v, missingCount=%d", result.HasValidationIssues, len(result.MissingPermissions))
	}
	return result
}

func permissionValidationToolsets(githubTool ValidatableTool, readOnly bool, parsedToolsets [][]string) []string {
	// Use pre-parsed toolsets when provided (avoids redundant ParseGitHubToolsets calls in hot paths).
	var toolsets []string
	if len(parsedToolsets) > 0 && parsedToolsets[0] != nil {
		toolsets = parsedToolsets[0]
		if permissionsValidationLog.Enabled() {
			permissionsValidationLog.Printf("Validating with pre-parsed toolsets: %v, read-only: %v", toolsets, readOnly)
		}
	} else {
		toolsetsStr := githubTool.GetToolsets()
		if permissionsValidationLog.Enabled() {
			permissionsValidationLog.Printf("Validating toolsets: %s, read-only: %v", toolsetsStr, readOnly)
		}
		toolsets = ParseGitHubToolsets(toolsetsStr)
	}

	return toolsets
}

// checkMissingPermissions checks if all required permissions are granted
func checkMissingPermissions(permissions *Permissions, required map[PermissionScope]PermissionLevel, toolsets []string, result *PermissionsValidationResult) {
	if permissionsValidationLog.Enabled() {
		permissionsValidationLog.Printf("Checking missing permissions: required_count=%d, toolsets=%v", len(required), toolsets)
	}
	// toolsetPerms is cached on first missing permission to avoid the accessor
	// overhead on the happy path (all permissions granted).
	var toolsetPerms map[string]GitHubToolsetPermissions
	for scope, requiredLevel := range required {
		grantedLevel, granted := permissions.Get(scope)

		missing := false
		if !granted {
			missing = true
		} else if requiredLevel == PermissionWrite && grantedLevel != PermissionWrite {
			missing = true
		}

		if missing {
			// Lazily initialize maps on the first missing permission to avoid
			// heap allocations on the happy path (all permissions granted).
			if result.MissingPermissions == nil {
				result.MissingPermissions = make(map[PermissionScope]PermissionLevel)
			}
			result.MissingPermissions[scope] = requiredLevel

			if result.MissingToolsetDetails == nil {
				result.MissingToolsetDetails = make(map[string][]PermissionScope)
			}
			if toolsetPerms == nil {
				toolsetPerms = getToolsetPermissionsMap()
			}
			// Track which toolsets require this permission
			for _, toolset := range toolsets {
				perms, exists := toolsetPerms[toolset]
				if !exists {
					continue
				}

				requiresScope := slices.Contains(perms.ReadPermissions, scope)
				if !requiresScope {
					if slices.Contains(perms.WritePermissions, scope) {
						requiresScope = true
					}
				}

				if requiresScope {
					result.MissingToolsetDetails[toolset] = append(result.MissingToolsetDetails[toolset], scope)
				}
			}
		}
	}
}

// FormatValidationMessage formats the validation result into a human-readable message
func FormatValidationMessage(result *PermissionsValidationResult, strict bool) string {
	if !result.HasValidationIssues {
		return ""
	}

	// Format missing permissions
	if len(result.MissingPermissions) > 0 {
		return formatMissingPermissionsMessage(result)
	}

	return ""
}

// formatMissingPermissionsMessage formats the missing permissions error message
func formatMissingPermissionsMessage(result *PermissionsValidationResult) string {
	var scopes []string
	for scope := range result.MissingPermissions {
		scopes = append(scopes, string(scope))
	}
	sort.Strings(scopes)

	var lines []string

	// Build permission list with toolset details inline
	permLines := missingPermissionDetailLines(result, scopes)

	lines = append(lines, "Missing required permissions for GitHub toolsets:")
	lines = append(lines, permLines...)
	lines = append(lines, "")
	lines = append(lines, "To fix this, you can either:")
	lines = append(lines, "")
	lines = append(lines, "Option 1: Add missing permissions to your workflow frontmatter:")
	lines = append(lines, "permissions:")
	for _, scopeStr := range scopes {
		scope := PermissionScope(scopeStr)
		level := result.MissingPermissions[scope]
		lines = append(lines, fmt.Sprintf("  %s: %s", scope, level))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("See: %s", constants.DocsPermissionsURL))
	lines = appendMissingPermissionToolsetSuggestion(lines, result)
	return strings.Join(lines, "\n")
}

func missingPermissionDetailLines(result *PermissionsValidationResult, scopes []string) []string {
	var permLines []string
	for _, scopeStr := range scopes {
		scope := PermissionScope(scopeStr)
		level := result.MissingPermissions[scope]

		// Find which toolsets need this permission
		var requiredBy []string
		if len(result.MissingToolsetDetails) > 0 {
			for toolset, toolsetScopes := range result.MissingToolsetDetails {
				if slices.Contains(toolsetScopes, scope) {
					requiredBy = append(requiredBy, toolset)
				}
			}
		}

		// Format: "- scope: level (required by toolset1, toolset2)"
		if len(requiredBy) > 0 {
			sort.Strings(requiredBy)
			permLines = append(permLines, fmt.Sprintf("  - %s: %s (required by %s)", scope, level, strings.Join(requiredBy, ", ")))
		} else {
			permLines = append(permLines, fmt.Sprintf("  - %s: %s", scope, level))
		}
	}

	return permLines
}

func appendMissingPermissionToolsetSuggestion(lines []string, result *PermissionsValidationResult) []string {
	// Add suggestion to reduce toolsets if we have toolset details
	if len(result.MissingToolsetDetails) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Option 2: Reduce the required toolsets in your workflow:")
		lines = append(lines, "Remove or adjust toolsets that require these permissions:")

		// Get unique toolsets from MissingToolsetDetails
		toolsetsMap := make(map[string]struct {
		})
		for toolset := range result.MissingToolsetDetails {
			toolsetsMap[toolset] = struct {
			}{}
		}
		toolsetsList := sliceutil.SortedKeys(toolsetsMap)

		for _, toolset := range toolsetsList {
			lines = append(lines, "  - "+toolset)
		}
	}

	return lines
}

// ValidateIncludedPermissions validates that the main workflow permissions satisfy the imported
// workflow requirements. This function is specifically used when merging included/imported workflow
// files to ensure the main workflow has sufficient permissions to support all imported files.
//
// Use ValidatePermissions (in permissions_validator.go) for general permission validation against
// GitHub MCP toolsets. Use ValidateIncludedPermissions (this function) when validating permissions
// from included/imported workflow files.
func (c *Compiler) ValidateIncludedPermissions(topPermissionsYAML string, importedPermissionsJSON string) error {
	permissionsValidationLog.Print("Validating included workflow permissions")

	// If no imported permissions, no validation needed
	if importedPermissionsJSON == "" || importedPermissionsJSON == "{}" {
		permissionsValidationLog.Print("No included workflow permissions to validate")
		return nil
	}

	// Parse top-level permissions
	var topPerms *Permissions
	if topPermissionsYAML != "" {
		topPerms = NewPermissionsParser(topPermissionsYAML).ToPermissions()
	} else {
		topPerms = NewPermissions()
	}

	// Track missing permissions
	missingPermissions := make(map[PermissionScope]PermissionLevel)
	insufficientPermissions := make(map[PermissionScope]includedPermissionDeficit)

	// Split by newlines to handle multiple JSON objects from different imports
	lines := strings.Split(importedPermissionsJSON, "\n")
	permissionsValidationLog.Printf("Processing %d permission definition lines", len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}

		// Parse JSON line to permissions map
		var importedPermsMap map[string]any
		if err := json.Unmarshal([]byte(line), &importedPermsMap); err != nil {
			permissionsValidationLog.Printf("Skipping malformed permission entry: %q (error: %v)", line, err)
			continue
		}

		checkIncludedPermissionLevels(topPerms, importedPermsMap, missingPermissions, insufficientPermissions)
	}
	if len(missingPermissions) > 0 || len(insufficientPermissions) > 0 {
		return formatIncludedPermissionDeficits(missingPermissions, insufficientPermissions)
	}
	permissionsValidationLog.Print("All included workflow permissions are satisfied by main workflow")
	return nil
}

type includedPermissionDeficit struct {
	required PermissionLevel
	current  PermissionLevel
}

func checkIncludedPermissionLevels(topPerms *Permissions, importedPermsMap map[string]any, missingPermissions map[PermissionScope]PermissionLevel, insufficientPermissions map[PermissionScope]includedPermissionDeficit) {
	for scopeStr, levelValue := range importedPermsMap {
		scope := PermissionScope(scopeStr)

		// Parse the level - it might be a string or already unmarshaled
		var requiredLevel PermissionLevel
		if levelStr, ok := levelValue.(string); ok {
			requiredLevel = PermissionLevel(levelStr)
		} else {
			// Skip invalid level values
			continue
		}

		// Get current level for this scope
		currentLevel, exists := topPerms.Get(scope)

		// Validate that the main workflow has sufficient permissions
		if !exists || currentLevel == PermissionNone {
			// Permission is missing entirely
			missingPermissions[scope] = requiredLevel
			permissionsValidationLog.Printf("Missing permission: %s: %s", scope, requiredLevel)
		} else if !isPermissionSufficient(currentLevel, requiredLevel) {
			// Permission exists but is insufficient
			insufficientPermissions[scope] = includedPermissionDeficit{requiredLevel, currentLevel}
			permissionsValidationLog.Printf("Insufficient permission: %s: has %s, needs %s", scope, currentLevel, requiredLevel)
		}
	}
}

func formatIncludedPermissionDeficits(missingPermissions map[PermissionScope]PermissionLevel, insufficientPermissions map[PermissionScope]includedPermissionDeficit) error {
	// If there are missing or insufficient permissions, return an error
	if len(missingPermissions) > 0 || len(insufficientPermissions) > 0 {
		var errorMsg strings.Builder
		errorMsg.WriteString("ERROR: Imported workflows require permissions that are not granted in the main workflow.\n\n")
		errorMsg.WriteString("The permission set must be explicitly declared in the main workflow.\n\n")

		if len(missingPermissions) > 0 {
			errorMsg.WriteString("Missing permissions:\n")
			// Sort for consistent output
			var scopes []PermissionScope
			for scope := range missingPermissions {
				scopes = append(scopes, scope)
			}
			SortPermissionScopes(scopes)
			for _, scope := range scopes {
				level := missingPermissions[scope]
				fmt.Fprintf(&errorMsg, "  - %s: %s\n", scope, level)
			}
			errorMsg.WriteString("\n")
		}

		if len(insufficientPermissions) > 0 {
			errorMsg.WriteString("Insufficient permissions:\n")
			// Sort for consistent output
			var scopes []PermissionScope
			for scope := range insufficientPermissions {
				scopes = append(scopes, scope)
			}
			SortPermissionScopes(scopes)
			for _, scope := range scopes {
				info := insufficientPermissions[scope]
				fmt.Fprintf(&errorMsg, "  - %s: has %s, requires %s\n", scope, info.current, info.required)
			}
			errorMsg.WriteString("\n")
		}

		writeIncludedPermissionSuggestion(&errorMsg, missingPermissions, insufficientPermissions)
		return errors.New(errorMsg.String())
	}
	return nil
}

func writeIncludedPermissionSuggestion(errorMsg *strings.Builder, missingPermissions map[PermissionScope]PermissionLevel, insufficientPermissions map[PermissionScope]includedPermissionDeficit) {
	errorMsg.WriteString("Suggested fix: Add the required permissions to your main workflow frontmatter:\n")
	errorMsg.WriteString("permissions:\n")

	// Combine all required permissions for the suggestion
	allRequired := make(map[PermissionScope]PermissionLevel)
	maps.Copy(allRequired, missingPermissions)
	for scope, info := range insufficientPermissions {
		allRequired[scope] = info.required
	}

	var scopes []PermissionScope
	for scope := range allRequired {
		scopes = append(scopes, scope)
	}
	SortPermissionScopes(scopes)
	for _, scope := range scopes {
		level := allRequired[scope]
		fmt.Fprintf(errorMsg, "  %s: %s\n", scope, level)
	}
}

// ValidatePermissionScopeNames validates that all permission scope names in the
// permissions YAML are recognized GitHub Actions permission scopes. When an
// unrecognized scope that closely resembles a valid scope is found, a "Did you
// mean?" suggestion is returned so users can quickly fix typos.
//
// Example: "contnts: read" → suggests "contents"
func ValidatePermissionScopeNames(permissionsYAML string) error {
	if permissionsYAML == "" {
		return nil
	}

	permissionsValidationLog.Print("Validating permission scope names")

	// Strip optional "permissions:" prefix so we can parse just the map content
	content := strings.TrimSpace(permissionsYAML)
	if strings.HasPrefix(content, "permissions:") {
		lines := strings.SplitN(content, "\n", 2)
		if len(lines) == 2 {
			content = lines[1]
		} else {
			// Single-line shorthand like "permissions: read-all" – no scope keys to check
			return nil
		}
	}

	// Try to parse the content as a YAML map of scope → level
	var permsMap map[string]any
	if err := yaml.Unmarshal([]byte(content), &permsMap); err != nil {
		// Not a map (e.g., a shorthand like "read-all"); nothing to validate
		return nil
	}
	return validatePermissionScopeMap(permsMap)
}

func validatePermissionScopeMap(permsMap map[string]any) error {
	for scopeKey := range permsMap {
		if setutil.Contains(validPermissionMetaKeys, scopeKey) {
			continue
		}
		if _, ok := validPermissionScopes[scopeKey]; ok {
			continue
		}

		// Unknown scope key — retrieve all valid scopes lazily (only on the error path)
		allScopes := getAllPermissionScopeNames()

		// Check for a case-only difference first (e.g. "Contents" → "contents")
		lowerScopeKey := strings.ToLower(scopeKey)
		if lowerScopeKey != scopeKey {
			if _, ok := validPermissionScopes[lowerScopeKey]; ok {
				return fmt.Errorf(
					"unknown permission scope %q.\n\nDid you mean: %s?\n\nValid permission scopes include: %s\n\nSee: %s",
					scopeKey,
					lowerScopeKey,
					strings.Join(allScopes[:min(10, len(allScopes))], ", ")+"...",
					constants.DocsPermissionsURL,
				)
			}
		}

		// Check for a close fuzzy match
		permissionsValidationLog.Printf("Unknown permission scope key: %q", scopeKey)
		suggestions := stringutil.FindClosestMatches(scopeKey, allScopes, 3)
		if len(suggestions) == 0 {
			continue // too different to be a typo, ignore silently
		}

		return fmt.Errorf(
			"unknown permission scope %q.\n\nDid you mean: %s?\n\nValid permission scopes include: %s\n\nSee: %s",
			scopeKey,
			strings.Join(suggestions, ", "),
			strings.Join(allScopes[:min(10, len(allScopes))], ", ")+"...",
			constants.DocsPermissionsURL,
		)
	}

	return nil
}
