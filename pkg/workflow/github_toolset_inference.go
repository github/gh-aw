package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var toolsetInferenceLog = logger.New("workflow:github_toolset_inference")

// GitHubToolsetInferenceEngine provides functionality to infer compatible GitHub MCP toolsets
// based on granted permissions. It examines the toolset permission requirements defined in
// github_toolsets_permissions.json and determines which toolsets can be safely enabled.
//
// The inference engine is used when workflows specify permissions but do not explicitly
// configure toolsets. It helps avoid permission errors by automatically selecting only
// the toolsets that are compatible with the granted permissions.
//
// Key responsibilities:
//   - Infer compatible toolsets from granted permissions
//   - Respect read-only mode constraints
//   - Support inference from specific toolset lists (not just defaults)
//   - Provide detailed logging for debugging inference decisions
//
// Related files:
//   - data/github_toolsets_permissions.json: Permission requirements for each toolset
//   - permissions_validation.go: Uses inference results for validation
//   - github_toolsets.go: Defines default toolsets
type GitHubToolsetInferenceEngine struct {
	// toolsetPermissions maps toolset names to their permission requirements
	// This is loaded from the embedded JSON file at initialization
	toolsetPermissions map[string]GitHubToolsetPermissions
}

// NewGitHubToolsetInferenceEngine creates a new inference engine using the global
// toolset permissions map loaded from github_toolsets_permissions.json
func NewGitHubToolsetInferenceEngine() *GitHubToolsetInferenceEngine {
	return &GitHubToolsetInferenceEngine{
		toolsetPermissions: toolsetPermissionsMap,
	}
}

// InferFromDefaults infers compatible toolsets from the default GitHub MCP toolsets
// based on the provided permissions. This is used when permissions are specified
// but toolsets are not explicitly configured.
//
// The function examines each toolset in DefaultGitHubToolsets and checks if all required
// permissions (both read and write) are satisfied by the provided permissions. It returns
// only the toolsets that are fully compatible.
//
// Parameters:
//   - permissions: The workflow's declared permissions
//   - readOnly: Whether the GitHub MCP is in read-only mode (only read permissions checked)
//
// Returns:
//   - A slice of compatible toolset names that can be safely enabled
func (e *GitHubToolsetInferenceEngine) InferFromDefaults(permissions *Permissions, readOnly bool) []string {
	toolsetInferenceLog.Printf("Inferring compatible toolsets from default toolsets (read-only: %v)", readOnly)
	return e.InferFromToolsets(permissions, DefaultGitHubToolsets, readOnly)
}

// InferFromToolsets infers compatible toolsets from a given list of toolsets
// based on the provided permissions. This is more flexible than InferFromDefaults
// as it allows checking compatibility against any list of toolsets.
//
// Parameters:
//   - permissions: The workflow's declared permissions
//   - toolsets: The list of toolset names to check for compatibility
//   - readOnly: Whether the GitHub MCP is in read-only mode (only read permissions checked)
//
// Returns:
//   - A slice of compatible toolset names from the input list
func (e *GitHubToolsetInferenceEngine) InferFromToolsets(permissions *Permissions, toolsets []string, readOnly bool) []string {
	toolsetInferenceLog.Printf("Inferring compatible toolsets from %d candidate toolsets (read-only: %v)", len(toolsets), readOnly)

	if permissions == nil {
		toolsetInferenceLog.Print("No permissions provided, returning empty toolset list")
		return []string{}
	}

	var compatible []string

	// Check each toolset for compatibility
	for _, toolset := range toolsets {
		if e.isToolsetCompatible(toolset, permissions, readOnly) {
			toolsetInferenceLog.Printf("Toolset %s is compatible", toolset)
			compatible = append(compatible, toolset)
		}
	}

	toolsetInferenceLog.Printf("Inferred %d compatible toolsets from %d candidates", len(compatible), len(toolsets))
	return compatible
}

// isToolsetCompatible checks if a single toolset is compatible with the given permissions
func (e *GitHubToolsetInferenceEngine) isToolsetCompatible(toolset string, permissions *Permissions, readOnly bool) bool {
	perms, exists := e.toolsetPermissions[toolset]
	if !exists {
		toolsetInferenceLog.Printf("Toolset %s not found in permissions map, skipping", toolset)
		return false
	}

	// Check read permissions
	for _, scope := range perms.ReadPermissions {
		grantedLevel, granted := permissions.Get(scope)
		if !granted || grantedLevel == PermissionNone {
			toolsetInferenceLog.Printf("Toolset %s incompatible: missing read permission %s", toolset, scope)
			return false
		}
	}

	// Check write permissions only if not in read-only mode
	if !readOnly {
		for _, scope := range perms.WritePermissions {
			grantedLevel, granted := permissions.Get(scope)
			if !granted || grantedLevel != PermissionWrite {
				toolsetInferenceLog.Printf("Toolset %s incompatible: missing write permission %s", toolset, scope)
				return false
			}
		}
	}

	return true
}

// GetAllToolsets returns all toolset names available in the permissions map.
// This is useful for testing and for getting a complete list of available toolsets.
func (e *GitHubToolsetInferenceEngine) GetAllToolsets() []string {
	toolsets := make([]string, 0, len(e.toolsetPermissions))
	for toolset := range e.toolsetPermissions {
		toolsets = append(toolsets, toolset)
	}
	return toolsets
}

// GetToolsetPermissions returns the permission requirements for a specific toolset.
// Returns nil if the toolset is not found.
func (e *GitHubToolsetInferenceEngine) GetToolsetPermissions(toolset string) *GitHubToolsetPermissions {
	if perms, exists := e.toolsetPermissions[toolset]; exists {
		return &perms
	}
	return nil
}

// InferCompatibleToolsets is a convenience function that creates an inference engine
// and infers compatible toolsets from the default toolsets. This maintains backward
// compatibility with the original API.
//
// Deprecated: Use NewGitHubToolsetInferenceEngine().InferFromDefaults() instead for
// better testability and flexibility.
func InferCompatibleToolsets(permissions *Permissions, readOnly bool) []string {
	engine := NewGitHubToolsetInferenceEngine()
	return engine.InferFromDefaults(permissions, readOnly)
}
