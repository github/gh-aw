package cli

import (
	"errors"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotPermissionsLog = logger.New("cli:add_copilot_permissions")

// This file handles Copilot-specific workflow permission injection.

// isCopilotWorkflowContent returns true unless the workflow frontmatter explicitly declares a non-Copilot engine.
// It is used to guard AddCopilotRequestsPermission injection so that the flag is only applied
// to Copilot workflows even when multiple workflows of different engines are processed together.
func isCopilotWorkflowContent(content string) bool {
	lines, _, err := parseFrontmatterLines(content)
	if err != nil {
		return false
	}
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		if !isTopLevelKey(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if parseYAMLMapKey(trimmed) == "engine" {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "engine:"))
			return val == string(constants.CopilotEngine)
		}
	}
	return true
}

// addCopilotRequestsPermissionToContent injects `permissions.copilot-requests: write`
// into the workflow frontmatter, enabling GitHub Actions token auth for Copilot (org billing).
// It delegates to ensureCopilotRequestsWritePermission, which locates or creates the
// permissions block and appends the copilot-requests entry if not already present.
// The function is idempotent: calling it on content that already contains the permission
// returns the content unchanged.
// Returns an error if the permission could not be injected and is not already present
// (e.g., when `permissions:` is a non-mapping scalar like `read-all`).
func addCopilotRequestsPermissionToContent(content string) (string, error) {
	return addCopilotRequestsPermissionToContentWithLevel(content, "write", false)
}

// addCopilotRequestsNonePermissionToContent injects `permissions.copilot-requests: none`
// into the workflow frontmatter, explicitly selecting PAT-based Copilot authentication.
func addCopilotRequestsNonePermissionToContent(content string) (string, error) {
	return addCopilotRequestsPermissionToContentWithLevel(content, "none", true)
}

func addCopilotRequestsPermissionToContentWithLevel(content, level string, replaceExisting bool) (string, error) {
	var injectionFailed bool
	newContent, modified, err := applyFrontmatterLineTransform(content, func(lines []string) ([]string, bool) {
		updated := ensureCopilotRequestsPermission(lines, level, replaceExisting)
		// Detect whether ensureCopilotRequestsWritePermission actually made a change.
		// When lengths differ, a line was added — modified is true without needing element comparison.
		// When lengths are equal, compare element-by-element (safe since len(updated)==len(lines)).
		modified := len(updated) != len(lines)
		if !modified {
			for i := range lines {
				if lines[i] != updated[i] {
					modified = true
					break
				}
			}
		}
		if !modified {
			// Lines unchanged — either the permission is already present (idempotent) or
			// it could not be injected (e.g., `permissions:` is a scalar like `read-all`).
			if !copilotRequestsPermissionPresentInLines(updated) {
				injectionFailed = true
			}
		}
		return updated, modified
	})
	if injectionFailed {
		copilotPermissionsLog.Print("Failed to inject copilot-requests permission: permissions block is a non-mapping scalar")
		return content, errors.New("permissions.copilot-requests could not be injected because 'permissions' is a non-mapping scalar value. Expected 'permissions' to be a mapping object. Example:\npermissions:\n  contents: read\n  copilot-requests: " + level)
	}
	if err != nil {
		return content, err
	}
	copilotPermissionsLog.Printf("copilot-requests permission injection complete: modified=%t", modified)
	return newContent, nil
}

// copilotRequestsPermissionPresentInLines returns true when the frontmatter lines contain
// a `copilot-requests:` key (ignoring comment lines). It is used to distinguish the idempotent
// case (permission already present) from the injection-failure case (scalar permissions field).
func copilotRequestsPermissionPresentInLines(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") && parseYAMLMapKey(trimmed) == "copilot-requests" {
			return true
		}
	}
	return false
}
