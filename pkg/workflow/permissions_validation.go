package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var permissionsValidationLog = logger.New("workflow:permissions_validation")

//go:embed data/github_toolsets_permissions.json
var githubToolsetsPermissionsJSON []byte

// GitHubToolsetPermissions maps GitHub MCP toolsets to their required permissions
type GitHubToolsetPermissions struct {
	ReadPermissions  []PermissionScope
	WritePermissions []PermissionScope
	Tools            []string // List of tools in this toolset (for verification)
}

// GitHubToolsetsData represents the structure of the embedded JSON file
type GitHubToolsetsData struct {
	Version     string `json:"version"`
	Description string `json:"description"`
	Toolsets    map[string]struct {
		Description      string   `json:"description"`
		ReadPermissions  []string `json:"read_permissions"`
		WritePermissions []string `json:"write_permissions"`
		Tools            []string `json:"tools"`
	} `json:"toolsets"`
}

// toolsetPermissionsMap defines the mapping of GitHub MCP toolsets to required permissions
// This is loaded from the embedded JSON file at initialization
var toolsetPermissionsMap map[string]GitHubToolsetPermissions

// init loads the GitHub toolsets and permissions from the embedded JSON
func init() {
	permissionsValidationLog.Print("Loading GitHub toolsets permissions from embedded JSON")

	var data GitHubToolsetsData
	if err := json.Unmarshal(githubToolsetsPermissionsJSON, &data); err != nil {
		panic(fmt.Sprintf("failed to load GitHub toolsets permissions from JSON: %v", err))
	}

	// Convert JSON data to internal format
	toolsetPermissionsMap = make(map[string]GitHubToolsetPermissions)
	for toolsetName, toolsetData := range data.Toolsets {
		// Convert string permission names to PermissionScope types
		readPerms := make([]PermissionScope, len(toolsetData.ReadPermissions))
		for i, perm := range toolsetData.ReadPermissions {
			readPerms[i] = PermissionScope(perm)
		}

		writePerms := make([]PermissionScope, len(toolsetData.WritePermissions))
		for i, perm := range toolsetData.WritePermissions {
			writePerms[i] = PermissionScope(perm)
		}

		toolsetPermissionsMap[toolsetName] = GitHubToolsetPermissions{
			ReadPermissions:  readPerms,
			WritePermissions: writePerms,
			Tools:            toolsetData.Tools,
		}
	}

	permissionsValidationLog.Printf("Loaded %d GitHub toolsets from JSON", len(toolsetPermissionsMap))
}

// GetToolsetsData returns the parsed GitHub toolsets data (for use by workflows)
func GetToolsetsData() GitHubToolsetsData {
	var data GitHubToolsetsData
	if err := json.Unmarshal(githubToolsetsPermissionsJSON, &data); err != nil {
		// This should never happen as we validate in init
		panic(fmt.Sprintf("failed to parse GitHub toolsets data: %v", err))
	}
	return data
}

// ValidatableTool represents a tool configuration that can be validated for permissions
// This interface abstracts the tool configuration structure to enable type-safe permission validation
type ValidatableTool interface {
	// GetToolsets returns the comma-separated list of toolsets configured for this tool
	GetToolsets() string
	// IsReadOnly returns whether the tool is configured in read-only mode
	IsReadOnly() bool
}

// GetToolsets implements ValidatableTool for GitHubToolConfig
func (g *GitHubToolConfig) GetToolsets() string {
	if g == nil {
		// Should not happen - ValidatePermissions checks for nil before calling this
		return ""
	}
	// Convert toolset array to comma-separated string
	// If empty, expandDefaultToolset will apply defaults
	toolsetsStr := strings.Join(g.Toolset.ToStringSlice(), ",")
	return expandDefaultToolset(toolsetsStr)
}

// IsReadOnly implements ValidatableTool for GitHubToolConfig
func (g *GitHubToolConfig) IsReadOnly() bool {
	if g == nil {
		return true // default to read-only for security
	}
	return g.ReadOnly
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
//
// Returns:
//   - A validation result indicating any missing permissions and which toolsets require them
//
// Use ValidatePermissions (this function) for general permission validation against GitHub MCP toolsets.
// Use ValidateIncludedPermissions (in imports.go) when validating permissions from included/imported workflow files.
func ValidatePermissions(permissions *Permissions, githubTool ValidatableTool) *PermissionsValidationResult {
	permissionsValidationLog.Print("Starting permissions validation")

	result := &PermissionsValidationResult{
		MissingPermissions:    make(map[PermissionScope]PermissionLevel),
		MissingToolsetDetails: make(map[string][]PermissionScope),
	}

	// If GitHub tool is not configured, no validation needed
	// Check both for nil interface and nil concrete type
	if githubTool == nil {
		permissionsValidationLog.Print("No GitHub tool configured (nil interface), skipping validation")
		return result
	}

	// Check if concrete type is nil (interface wrapping nil pointer)
	if config, ok := githubTool.(*GitHubToolConfig); ok && config == nil {
		permissionsValidationLog.Print("No GitHub tool configured (nil concrete type), skipping validation")
		return result
	}

	// Extract toolsets from GitHub tool configuration
	toolsetsStr := githubTool.GetToolsets()
	readOnly := githubTool.IsReadOnly()
	result.ReadOnlyMode = readOnly

	permissionsValidationLog.Printf("Validating toolsets: %s, read-only: %v", toolsetsStr, readOnly)

	// Parse toolsets
	toolsets := ParseGitHubToolsets(toolsetsStr)
	if len(toolsets) == 0 {
		permissionsValidationLog.Print("No toolsets to validate")
		return result
	}

	// Collect required permissions for all toolsets
	requiredPermissions := collectRequiredPermissions(toolsets, readOnly)
	permissionsValidationLog.Printf("Required permissions: %v", requiredPermissions)

	// Check for missing permissions
	checkMissingPermissions(permissions, requiredPermissions, toolsets, result)

	result.HasValidationIssues = len(result.MissingPermissions) > 0
	permissionsValidationLog.Printf("Validation complete: hasIssues=%v, missingCount=%d", result.HasValidationIssues, len(result.MissingPermissions))

	return result
}

// collectRequiredPermissions collects all required permissions for the given toolsets
func collectRequiredPermissions(toolsets []string, readOnly bool) map[PermissionScope]PermissionLevel {
	permissionsValidationLog.Printf("Collecting required permissions for %d toolsets, read_only=%t", len(toolsets), readOnly)
	required := make(map[PermissionScope]PermissionLevel)

	for _, toolset := range toolsets {
		perms, exists := toolsetPermissionsMap[toolset]
		if !exists {
			permissionsValidationLog.Printf("Unknown toolset: %s", toolset)
			continue
		}

		// Add read permissions
		for _, scope := range perms.ReadPermissions {
			// Always require at least read access
			if existing, found := required[scope]; !found || existing == PermissionNone {
				required[scope] = PermissionRead
			}
		}

		// Add write permissions only if not in read-only mode
		if !readOnly {
			for _, scope := range perms.WritePermissions {
				required[scope] = PermissionWrite
			}
		}
	}

	return required
}

// checkMissingPermissions checks if all required permissions are granted
func checkMissingPermissions(permissions *Permissions, required map[PermissionScope]PermissionLevel, toolsets []string, result *PermissionsValidationResult) {
	permissionsValidationLog.Printf("Checking missing permissions: required_count=%d, toolsets=%v", len(required), toolsets)
	for scope, requiredLevel := range required {
		grantedLevel, granted := permissions.Get(scope)

		missing := false
		if !granted {
			missing = true
		} else if requiredLevel == PermissionWrite && grantedLevel != PermissionWrite {
			missing = true
		}

		if missing {
			result.MissingPermissions[scope] = requiredLevel

			// Track which toolsets require this permission
			for _, toolset := range toolsets {
				perms, exists := toolsetPermissionsMap[toolset]
				if !exists {
					continue
				}

				requiresScope := false
				for _, readScope := range perms.ReadPermissions {
					if readScope == scope {
						requiresScope = true
						break
					}
				}
				if !requiresScope {
					for _, writeScope := range perms.WritePermissions {
						if writeScope == scope {
							requiresScope = true
							break
						}
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
	var permLines []string
	for _, scopeStr := range scopes {
		scope := PermissionScope(scopeStr)
		level := result.MissingPermissions[scope]

		// Find which toolsets need this permission
		var requiredBy []string
		if len(result.MissingToolsetDetails) > 0 {
			for toolset, toolsetScopes := range result.MissingToolsetDetails {
				for _, ts := range toolsetScopes {
					if ts == scope {
						requiredBy = append(requiredBy, toolset)
						break
					}
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

	lines = append(lines, "Missing required permissions for github toolsets:")
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

	// Add suggestion to reduce toolsets if we have toolset details
	if len(result.MissingToolsetDetails) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Option 2: Reduce the required toolsets in your workflow:")
		lines = append(lines, "Remove or adjust toolsets that require these permissions:")

		// Get unique toolsets from MissingToolsetDetails
		toolsetsMap := make(map[string]bool)
		for toolset := range result.MissingToolsetDetails {
			toolsetsMap[toolset] = true
		}
		var toolsetsList []string
		for toolset := range toolsetsMap {
			toolsetsList = append(toolsetsList, toolset)
		}
		sort.Strings(toolsetsList)

		for _, toolset := range toolsetsList {
			lines = append(lines, fmt.Sprintf("  - %s", toolset))
		}
	}

	return strings.Join(lines, "\n")
}
