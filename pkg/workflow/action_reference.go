package workflow

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
)

var actionRefLog = logger.New("workflow:action_reference")

const (
	// GitHubOrgRepo is the organization and repository name for custom action references
	GitHubOrgRepo = "github/gh-aw"

	// GitHubActionsOrgRepo is the organization and repository name for the external gh-aw-actions repository
	GitHubActionsOrgRepo = "github/gh-aw-actions"
)

// ResolveSetupActionReference resolves the actions/setup action reference based on action mode and version.
// This is a standalone helper function that can be used by both Compiler methods and standalone
// workflow generators (like maintenance workflow) that don't have access to WorkflowData.
//
// Parameters:
//   - actionMode: The action mode (dev, release, or action)
//   - version: The version string to use for release/action mode
//   - actionTag: Optional override tag/SHA (takes precedence over version when in release mode)
//   - resolver: Optional SHAResolver for dynamic SHA resolution (can be nil for standalone use)
//
// Returns:
//   - For dev mode: "./actions/setup" (local path)
//   - For release mode with resolver: "github/gh-aw/actions/setup@<sha> # <version>" (SHA-pinned)
//   - For release mode without resolver: "github/gh-aw/actions/setup@<version>" (tag-based, SHA resolved later)
//   - For action mode with resolver or embedded pin: "github/gh-aw-actions/setup@<sha> # <version>" (SHA-pinned)
//   - For action mode without a resolved pin: "" (fail closed rather than emitting a mutable reference)
//   - Falls back to local path if version is invalid in release/action mode
func ResolveSetupActionReference(ctx context.Context, actionMode ActionMode, version string, actionTag string, resolver SHAResolver) string {
	return resolveSetupActionRef(ctx, actionMode, version, actionTag, resolver, "")
}

// resolveSetupActionRef is the internal implementation of ResolveSetupActionReference
// that accepts an optional actionsOrgRepo override. When actionsOrgRepo is empty,
// GitHubActionsOrgRepo is used.
func resolveSetupActionRef(ctx context.Context, actionMode ActionMode, version string, actionTag string, resolver SHAResolver, actionsOrgRepo string) string {
	if actionsOrgRepo == "" {
		actionsOrgRepo = GitHubActionsOrgRepo
	}

	localPath := "./actions/setup"

	if actionMode == ActionModeDev {
		actionRefLog.Printf("Dev mode: using local action path: %s", localPath)
		return localPath
	}
	if actionMode == ActionModeAction {
		return resolveSetupActionModeRef(ctx, actionTag, version, resolver, actionsOrgRepo, localPath)
	}
	if actionMode == ActionModeRelease {
		return resolveSetupReleaseModeRef(ctx, actionTag, version, resolver, localPath)
	}
	actionRefLog.Printf("WARNING: Unknown action mode %s, defaulting to local path", actionMode)
	return localPath
}

func resolveSetupActionModeRef(ctx context.Context, actionTag string, version string, resolver SHAResolver, actionsOrgRepo string, localPath string) string {
	tag, ok := resolveSetupTag(actionTag, version, localPath)
	if !ok {
		return localPath
	}
	actionRepo := path.Join(actionsOrgRepo, "setup")
	remoteRef := fmt.Sprintf("%s@%s", actionRepo, tag)
	ref := resolveRequiredActionModePin(ctx, resolver, actionRepo, tag, remoteRef)
	if ref != "" {
		return ref
	}
	actionRefLog.Printf("Action mode: refusing to emit mutable external actions repo reference: %s", remoteRef)
	return ""
}

func resolveSetupReleaseModeRef(ctx context.Context, actionTag string, version string, resolver SHAResolver, localPath string) string {
	tag, ok := resolveSetupTag(actionTag, version, localPath)
	if !ok {
		return localPath
	}
	actionPath := strings.TrimPrefix(localPath, "./")
	actionRepo := path.Join(GitHubOrgRepo, actionPath)
	remoteRef := fmt.Sprintf("%s@%s", actionRepo, tag)
	ref := tryResolveSetupSHA(ctx, resolver, actionRepo, tag, remoteRef, "Release mode")
	if ref != "" {
		return ref
	}
	actionRefLog.Printf("Release mode: using tag-based remote action reference: %s (SHA will be resolved later)", remoteRef)
	return remoteRef
}

func resolveSetupTag(actionTag string, version string, localPath string) (string, bool) {
	tag := actionTag
	if tag == "" {
		tag = version
	}
	if tag == "" || tag == "dev" {
		actionRefLog.Printf("WARNING: No release tag available in binary version (version is 'dev' or empty), falling back to local path: %s", localPath)
		return "", false
	}
	return tag, true
}

func tryResolveSetupSHA(ctx context.Context, resolver SHAResolver, actionRepo, tag, remoteRef, modeLabel string) string {
	if resolver == nil {
		return ""
	}
	sha, err := resolver.ResolveSHA(ctx, actionRepo, tag)
	if err == nil && sha != "" {
		if !gitutil.IsValidFullSHA(sha) {
			actionRefLog.Printf("Failed to resolve full SHA for %s@%s: resolver returned %q", actionRepo, tag, sha)
			return ""
		}
		pinnedRef := formatActionReference(actionRepo, sha, tag)
		actionRefLog.Printf("%s: resolved %s to SHA-pinned reference: %s", modeLabel, remoteRef, pinnedRef)
		return pinnedRef
	}
	if err != nil {
		actionRefLog.Printf("Failed to resolve SHA for %s@%s: %v", actionRepo, tag, err)
	}
	return ""
}

func resolveRequiredActionModePin(ctx context.Context, resolver SHAResolver, actionRepo, tag, remoteRef string) string {
	if ref := tryResolveSetupSHA(ctx, resolver, actionRepo, tag, remoteRef, "Action mode"); ref != "" {
		return ref
	}
	ref, err := getActionPinWithData(actionRepo, tag, actionModePinData(&WorkflowData{Ctx: ctx}))
	if err != nil {
		actionRefLog.Printf("Action mode: failed to pin action %s@%s: %v", actionRepo, tag, err)
		return ""
	}
	if isFullSHAPinnedActionRef(ref) {
		actionRefLog.Printf("Action mode: resolved %s to SHA-pinned reference: %s", remoteRef, ref)
		return ref
	}
	if ref != "" {
		actionRefLog.Printf("Action mode: refusing non-full-SHA action reference: %s", ref)
	}
	return ""
}

func isFullSHAPinnedActionRef(ref string) bool {
	_, after, ok := strings.Cut(ref, "@")
	if !ok {
		return false
	}
	actionRef := strings.TrimSpace(after)
	if before, _, ok := strings.Cut(actionRef, " "); ok {
		actionRef = before
	}
	if before, _, ok := strings.Cut(actionRef, "#"); ok {
		actionRef = before
	}
	return gitutil.IsValidFullSHA(actionRef)
}

// resolveActionReference converts a local action path to the appropriate reference
// based on the current action mode (dev vs release vs action).
// If action-tag is specified in features, it overrides the mode check and enables action mode behavior
// (using the github/gh-aw-actions external repository).
// For dev mode: returns the local path as-is (e.g., "./actions/create-issue")
// For release mode: converts to SHA-pinned remote reference (e.g., "github/gh-aw/actions/create-issue@SHA # tag")
// For action mode: converts to SHA-pinned reference in external repo, or "" when no full SHA is available
func (c *Compiler) resolveActionReference(localActionPath string, data *WorkflowData) string {
	hasActionTag, frontmatterActionTag := getFrontmatterActionTag(data)

	// For ./actions/setup, check for compiler-level actionTag override first
	if localActionPath == "./actions/setup" {
		return c.resolveSetupReference(data, hasActionTag, frontmatterActionTag)
	}

	if c.actionMode == ActionModeAction || hasActionTag {
		return c.convertToExternalActionsRef(localActionPath, data)
	}

	if c.actionMode == ActionModeRelease {
		return c.resolveReleaseActionReference(localActionPath, data)
	}
	if c.actionMode == ActionModeDev {
		actionRefLog.Printf("Dev mode: using local action path: %s", localActionPath)
		return localActionPath
	}

	// Default to dev mode for unknown modes
	actionRefLog.Printf("WARNING: Unknown action mode %s, defaulting to dev mode", c.actionMode)
	return localActionPath
}

func getFrontmatterActionTag(data *WorkflowData) (bool, string) {
	if data == nil || data.Features == nil {
		return false, ""
	}
	actionTagVal, exists := data.Features["action-tag"]
	if !exists {
		return false, ""
	}
	actionTagStr, ok := actionTagVal.(string)
	if !ok || actionTagStr == "" {
		return false, ""
	}
	actionRefLog.Printf("action-tag feature detected: %s - using action mode behavior", actionTagStr)
	return true, actionTagStr
}

func (c *Compiler) resolveSetupReference(data *WorkflowData, hasActionTag bool, frontmatterActionTag string) string {
	var resolver SHAResolver
	if data != nil && data.ActionResolver != nil {
		resolver = data.ActionResolver
	}
	if c.actionTag != "" {
		return resolveSetupActionRef(c.ctx, c.actionMode, c.version, c.actionTag, resolver, c.effectiveActionsRepo())
	}
	if !hasActionTag {
		return resolveSetupActionRef(c.ctx, c.actionMode, c.version, "", resolver, c.effectiveActionsRepo())
	}
	return resolveSetupActionRef(c.ctx, ActionModeAction, c.version, frontmatterActionTag, resolver, c.effectiveActionsRepo())
}

func (c *Compiler) resolveReleaseActionReference(localActionPath string, data *WorkflowData) string {
	remoteRef := c.convertToRemoteActionRef(localActionPath, data)
	if remoteRef == "" {
		actionRefLog.Printf("WARNING: Could not resolve remote reference for %s", localActionPath)
		return ""
	}
	actionRepo := extractActionRepo(remoteRef)
	version := extractActionVersion(remoteRef)
	if actionRepo == "" || version == "" {
		actionRefLog.Printf("Release mode: using tag-based remote action reference: %s", remoteRef)
		return remoteRef
	}
	pinnedRef, err := getActionPinWithData(actionRepo, version, data)
	if err != nil {
		actionRefLog.Printf("Failed to pin action %s@%s: %v", actionRepo, version, err)
		return ""
	}
	if pinnedRef != "" {
		actionRefLog.Printf("Release mode: resolved %s to SHA-pinned reference: %s", remoteRef, pinnedRef)
		return pinnedRef
	}
	actionRefLog.Printf("Release mode: using tag-based remote action reference: %s", remoteRef)
	return remoteRef
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

// convertToExternalActionsRef converts a local action path to a SHA-pinned (if possible) reference
// in the external github/gh-aw-actions repository.
// Example: "./actions/create-issue" -> "github/gh-aw-actions/create-issue@<sha> # v1.0.0"
//
// If SHA resolution fails (no resolver or pin not available), returns "" rather than a mutable tag reference.
func (c *Compiler) convertToExternalActionsRef(localPath string, data *WorkflowData) string {
	// Strip the leading "./" prefix
	actionPath := strings.TrimPrefix(localPath, "./")

	// Strip the "actions/" prefix to get just the action name
	// e.g., "actions/create-issue" -> "create-issue"
	actionName := strings.TrimPrefix(actionPath, "actions/")

	// Determine tag: use compiler actionTag or version
	tag := c.actionTag
	if tag == "" {
		if data != nil && data.Features != nil {
			if actionTagVal, exists := data.Features["action-tag"]; exists {
				if actionTagStr, ok := actionTagVal.(string); ok && actionTagStr != "" {
					tag = actionTagStr
				}
			}
		}
	}
	if tag == "" {
		tag = c.version
		if tag == "" || tag == "dev" {
			actionRefLog.Print("WARNING: No release tag available in binary version (version is 'dev' or empty)")
			return ""
		}
	}

	// Construct the external actions reference: <actionsRepo>/action-name@tag
	actionRepo := path.Join(c.effectiveActionsRepo(), actionName)
	remoteRef := fmt.Sprintf("%s@%s", actionRepo, tag)

	// Try to resolve the SHA using action pins
	pinData := actionModePinData(data)
	if pinData.Ctx == nil {
		pinData.Ctx = c.ctx
	}
	pinnedRef, err := getActionPinWithData(actionRepo, tag, pinData)
	if err != nil {
		actionRefLog.Printf("Action mode: failed to pin action %s@%s: %v", actionRepo, tag, err)
		return ""
	}
	if isFullSHAPinnedActionRef(pinnedRef) {
		actionRefLog.Printf("Action mode: resolved %s to SHA-pinned reference: %s", remoteRef, pinnedRef)
		return pinnedRef
	}
	if pinnedRef != "" {
		actionRefLog.Printf("Action mode: refusing non-full-SHA action reference: %s", pinnedRef)
	}
	actionRefLog.Printf("Action mode: refusing to emit mutable external actions repo reference: %s", remoteRef)
	return ""
}

func actionModePinData(data *WorkflowData) *WorkflowData {
	if data == nil {
		return &WorkflowData{StrictMode: true}
	}
	pinData := *data
	pinData.StrictMode = true
	return &pinData
}
