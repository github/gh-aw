package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var actionRefLog = logger.New("workflow:action_reference")

const (
	// GitHubOrgRepo is the organization and repository name for custom action references
	GitHubOrgRepo = "github/gh-aw"
)

// ResolveSetupActionReference resolves the actions/setup action reference based on action mode and version.
// This is a standalone helper function that can be used by both Compiler methods and standalone
// workflow generators (like maintenance workflow) that don't have access to WorkflowData.
//
// Parameters:
//   - actionMode: The action mode (dev or release)
//   - version: The version string to use for release mode
//   - actionTag: Optional override tag/SHA (takes precedence over version when in release mode)
//   - data: Optional WorkflowData for SHA resolution (can be nil for standalone use)
//
// Returns:
//   - For dev mode: "./actions/setup" (local path)
//   - For release mode with data: "github/gh-aw/actions/setup@<sha> # <version>" (SHA-pinned)
//   - For release mode without data: "github/gh-aw/actions/setup@<version>" (tag-based, SHA resolved later)
//   - Falls back to local path if version is invalid in release mode
func ResolveSetupActionReference(actionMode ActionMode, version string, actionTag string, data *WorkflowData) string {
	localPath := "./actions/setup"

	// Dev mode - return local path
	if actionMode == ActionModeDev {
		actionRefLog.Printf("Dev mode: using local action path: %s", localPath)
		return localPath
	}

	// Release mode - convert to remote reference
	if actionMode == ActionModeRelease {
		actionPath := strings.TrimPrefix(localPath, "./")

		// Use actionTag if provided, otherwise fall back to version
		tag := actionTag
		if tag == "" {
			tag = version
		}

		// Check if tag is valid for release mode
		if tag == "" || tag == "dev" {
			actionRefLog.Print("WARNING: No release tag available in binary version (version is 'dev' or empty), falling back to local path")
			return localPath
		}

		// Construct the remote reference with tag: github/gh-aw/actions/setup@tag
		remoteRef := fmt.Sprintf("%s/%s@%s", GitHubOrgRepo, actionPath, tag)

		// If WorkflowData is available, try to resolve the SHA
		if data != nil {
			actionRepo := fmt.Sprintf("%s/%s", GitHubOrgRepo, actionPath)
			pinnedRef, err := GetActionPinWithData(actionRepo, tag, data)
			if err != nil {
				// In strict mode, GetActionPinWithData returns an error
				actionRefLog.Printf("Failed to pin action %s@%s: %v", actionRepo, tag, err)
				return ""
			}
			if pinnedRef != "" {
				// Successfully resolved to SHA
				actionRefLog.Printf("Release mode: resolved %s to SHA-pinned reference: %s", remoteRef, pinnedRef)
				return pinnedRef
			}
		}

		// If WorkflowData is not available or SHA resolution failed, return tag-based reference
		// This is for backward compatibility with standalone workflow generators
		actionRefLog.Printf("Release mode: using tag-based remote action reference: %s (SHA will be resolved later)", remoteRef)
		return remoteRef
	}

	// Unknown mode - default to local path
	actionRefLog.Printf("WARNING: Unknown action mode %s, defaulting to local path", actionMode)
	return localPath
}

// resolveActionReference converts a local action path to the appropriate reference
// based on the current action mode (dev vs release).
// If action-tag is specified in features, it overrides the mode check and enables release mode behavior.
// For dev mode: returns the local path as-is (e.g., "./actions/create-issue")
// For release mode: converts to SHA-pinned remote reference (e.g., "github/gh-aw/actions/create-issue@SHA # tag")
func (c *Compiler) resolveActionReference(localActionPath string, data *WorkflowData) string {
	// Check if action-tag is specified in features - if so, override mode and use release behavior
	hasActionTag := false
	if data != nil && data.Features != nil {
		if actionTagVal, exists := data.Features["action-tag"]; exists {
			if actionTagStr, ok := actionTagVal.(string); ok && actionTagStr != "" {
				hasActionTag = true
				actionRefLog.Printf("action-tag feature detected: %s - using release mode behavior", actionTagStr)
			}
		}
	}

	// For ./actions/setup, check for compiler-level actionTag override first
	if localActionPath == "./actions/setup" {
		// Use compiler actionTag if available, otherwise check features
		if c.actionTag != "" {
			return ResolveSetupActionReference(c.actionMode, c.version, c.actionTag, data)
		}
		if !hasActionTag {
			return ResolveSetupActionReference(c.actionMode, c.version, "", data)
		}
	}

	// Use release mode if either actionMode is release OR action-tag is specified
	if c.actionMode == ActionModeRelease || hasActionTag {
		// Convert to tag-based remote reference for release
		remoteRef := c.convertToRemoteActionRef(localActionPath, data)
		if remoteRef == "" {
			actionRefLog.Printf("WARNING: Could not resolve remote reference for %s", localActionPath)
			return ""
		}

		// Now resolve the tag to a SHA using action pins
		// Extract repo and version from the remote reference (format: "repo/path@version")
		actionRepo := extractActionRepo(remoteRef)
		version := extractActionVersion(remoteRef)

		if actionRepo != "" && version != "" {
			// Resolve the SHA using action pins
			pinnedRef, err := GetActionPinWithData(actionRepo, version, data)
			if err != nil {
				// In strict mode, GetActionPinWithData returns an error
				actionRefLog.Printf("Failed to pin action %s@%s: %v", actionRepo, version, err)
				return ""
			}
			if pinnedRef != "" {
				// Successfully resolved to SHA
				if hasActionTag {
					actionRefLog.Printf("action-tag override: resolved %s to SHA-pinned reference: %s", remoteRef, pinnedRef)
				} else {
					actionRefLog.Printf("Release mode: resolved %s to SHA-pinned reference: %s", remoteRef, pinnedRef)
				}
				return pinnedRef
			}
		}

		// If we couldn't resolve to SHA, return the tag-based reference
		// This happens in non-strict mode when no pin is available
		if hasActionTag {
			actionRefLog.Printf("action-tag override: using tag-based remote action reference: %s", remoteRef)
		} else {
			actionRefLog.Printf("Release mode: using tag-based remote action reference: %s", remoteRef)
		}
		return remoteRef
	}

	// Dev mode - return local path
	if c.actionMode == ActionModeDev {
		actionRefLog.Printf("Dev mode: using local action path: %s", localActionPath)
		return localActionPath
	}

	// Default to dev mode for unknown modes
	actionRefLog.Printf("WARNING: Unknown action mode %s, defaulting to dev mode", c.actionMode)
	return localActionPath
}

// convertToRemoteActionRef converts a local action path to a tag-based remote reference
// that will be resolved to a SHA later in the release pipeline using action pins.
// Uses the action-tag from WorkflowData.Features if specified (for testing), otherwise uses the version stored in the compiler binary.
// If compiler has actionTag set, it takes priority over both.
// Example: "./actions/create-issue" -> "github/gh-aw/actions/create-issue@v1.0.0"
func (c *Compiler) convertToRemoteActionRef(localPath string, data *WorkflowData) string {
	// Strip the leading "./" if present
	actionPath := strings.TrimPrefix(localPath, "./")

	// Priority order for tag selection:
	// 1. Compiler actionTag (from --action-tag flag)
	// 2. WorkflowData.Features["action-tag"] (from frontmatter)
	// 3. Compiler version
	var tag string

	// Check compiler actionTag first (highest priority)
	if c.actionTag != "" {
		tag = c.actionTag
		actionRefLog.Printf("Using action-tag from compiler: %s", tag)
	} else if data != nil && data.Features != nil {
		// Check WorkflowData.Features for action-tag
		if actionTagVal, exists := data.Features["action-tag"]; exists {
			if actionTagStr, ok := actionTagVal.(string); ok && actionTagStr != "" {
				tag = actionTagStr
				actionRefLog.Printf("Using action-tag from features: %s", tag)
			}
		}
	}

	// Fall back to compiler version if no tag specified
	if tag == "" {
		tag = c.version
		if tag == "" || tag == "dev" {
			actionRefLog.Print("WARNING: No release tag available in binary version (version is 'dev' or empty)")
			return ""
		}
		actionRefLog.Printf("Using tag from binary version: %s", tag)
	}

	// Construct the remote reference with tag: github/gh-aw/actions/name@tag
	// The SHA will be resolved later by action pinning infrastructure
	remoteRef := fmt.Sprintf("%s/%s@%s", GitHubOrgRepo, actionPath, tag)
	actionRefLog.Printf("Remote reference: %s (SHA will be resolved via action pins)", remoteRef)

	return remoteRef
}
