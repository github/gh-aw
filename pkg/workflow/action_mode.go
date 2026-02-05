package workflow

import (
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var actionModeLog = logger.New("workflow:action_mode")

// ActionMode defines how JavaScript is embedded in workflow steps
type ActionMode string

const (
	// ActionModeDev references custom actions using local paths (development mode, default)
	ActionModeDev ActionMode = "dev"

	// ActionModeRelease references custom actions using SHA-pinned remote paths (release mode)
	ActionModeRelease ActionMode = "release"

	// ActionModeScript runs setup.sh script from checked-out .github folder instead of using action steps
	ActionModeScript ActionMode = "script"
)

// String returns the string representation of the action mode
func (m ActionMode) String() string {
	return string(m)
}

// IsValid checks if the action mode is valid
func (m ActionMode) IsValid() bool {
	return m == ActionModeDev || m == ActionModeRelease || m == ActionModeScript
}

// IsDev returns true if the action mode is development mode
func (m ActionMode) IsDev() bool {
	return m == ActionModeDev
}

// IsRelease returns true if the action mode is release mode
func (m ActionMode) IsRelease() bool {
	return m == ActionModeRelease
}

// IsScript returns true if the action mode is script mode
func (m ActionMode) IsScript() bool {
	return m == ActionModeScript
}

// UsesExternalActions returns true (always true since inline mode was removed)
func (m ActionMode) UsesExternalActions() bool {
	return true
}

// DetectActionMode determines the appropriate action mode based on the release flag.
// Returns ActionModeRelease if this binary was built as a release (controlled by the
// isReleaseBuild flag set via -X linker flag at build time), otherwise returns ActionModeDev.
// Can be overridden with GH_AW_ACTION_MODE environment variable or GitHub Actions context.
// The version parameter is kept for backward compatibility but is no longer used for detection.
func DetectActionMode(version string) ActionMode {
	actionModeLog.Printf("Detecting action mode: version=%s, isRelease=%v", version, IsRelease())

	// Check for explicit override via environment variable
	if envMode := os.Getenv("GH_AW_ACTION_MODE"); envMode != "" {
		mode := ActionMode(envMode)
		if mode.IsValid() {
			actionModeLog.Printf("Using action mode from environment override: %s", mode)
			return mode
		}
		actionModeLog.Printf("Invalid action mode in environment: %s, falling back to auto-detection", envMode)
	}

	// Check if this binary was built as a release using the release flag
	// This flag is set at build time via -X linker flag and does not rely on version string heuristics
	if IsRelease() {
		actionModeLog.Printf("Detected release mode from build flag (isReleaseBuild=true)")
		return ActionModeRelease
	}

	// Check GitHub Actions context for additional hints
	githubRef := os.Getenv("GITHUB_REF")
	githubEventName := os.Getenv("GITHUB_EVENT_NAME")
	actionModeLog.Printf("GitHub context: ref=%s, event=%s", githubRef, githubEventName)

	// Release mode conditions from GitHub Actions context:
	// 1. Running on a release branch (refs/heads/release*)
	// 2. Running on a release tag (refs/tags/*)
	// 3. Running on a release event
	if strings.HasPrefix(githubRef, "refs/heads/release") ||
		strings.HasPrefix(githubRef, "refs/tags/") ||
		githubEventName == "release" {
		actionModeLog.Printf("Detected release mode from GitHub context: ref=%s, event=%s", githubRef, githubEventName)
		return ActionModeRelease
	}

	// Default to dev mode for all other cases:
	// 1. Running on a PR (refs/pull/*)
	// 2. Running locally (no GITHUB_REF)
	// 3. Running on any other branch (including main)
	// 4. Non-release builds (isReleaseBuild=false)
	actionModeLog.Printf("Detected dev mode (default): isRelease=%v, ref=%s", IsRelease(), githubRef)
	return ActionModeDev
}

// GetActionModeFromWorkflowData extracts the ActionMode from WorkflowData, defaulting to dev mode if nil
func GetActionModeFromWorkflowData(workflowData *WorkflowData) ActionMode {
	if workflowData != nil {
		return workflowData.ActionMode
	}
	return ActionModeDev
}
