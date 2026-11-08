package cli

import (
	"errors"
	"slices"
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
			isCopilot := val == string(constants.CopilotEngine)
			copilotPermissionsLog.Printf("Workflow declares engine=%q, treating as copilot workflow=%t", val, isCopilot)
			return isCopilot
		}
	}
	copilotPermissionsLog.Print("Workflow declares no engine field, defaulting to copilot workflow=true")
	return true
}

// addCopilotRequestsPermissionToContent injects `permissions.copilot-requests: write`
// into the workflow frontmatter, enabling GitHub Actions token auth for Copilot (org billing).
// It delegates to ensureCopilotRequestsPermission, which locates or creates the
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
		modified := !slices.Equal(updated, lines)
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

// ensureCopilotRequestsPermission retains the add command's permission insertion
// behavior independently of the AST-backed fix codemods.
func ensureCopilotRequestsPermission(lines []string, level string, replaceExisting bool) []string {
	permissionsIdx := -1
	permissionsIndent := ""
	permissionsEnd := len(lines)
	var header string
	for i, line := range lines {
		if isTopLevelKey(line) && strings.HasPrefix(strings.TrimSpace(line), "permissions:") {
			permissionsIdx = i
			permissionsIndent = getIndentation(line)
			header = line
			for offset, next := range lines[i+1:] {
				if isTopLevelKey(next) {
					permissionsEnd = i + 1 + offset
					break
				}
			}
			break
		}
	}
	if permissionsIdx == -1 {
		insertAt := findPermissionsInsertIndex(lines)
		copilotPermissionsLog.Printf("No permissions block found, inserting new block with copilot-requests: %s at line index %d", level, insertAt)
		result := make([]string, 0, len(lines)+2)
		result = append(result, lines[:insertAt]...)
		result = append(result, "permissions:", "  copilot-requests: "+level)
		return append(result, lines[insertAt:]...)
	}
	inlineValue := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(header), "permissions:"))
	if inlineValue != "" && !strings.HasPrefix(inlineValue, "#") {
		valuePart, _, _ := strings.Cut(inlineValue, "#")
		if strings.TrimSpace(valuePart) == "{}" {
			copilotPermissionsLog.Print("Expanding empty inline permissions mapping ({}) to add copilot-requests")
			result := make([]string, 0, len(lines)+1)
			result = append(result, lines[:permissionsIdx]...)
			result = append(result, permissionsIndent+"permissions:", permissionsIndent+"  copilot-requests: "+level)
			return append(result, lines[permissionsIdx+1:]...)
		}
		copilotPermissionsLog.Printf("Permissions block has inline scalar value %q, leaving lines unchanged", inlineValue)
		return lines
	}
	for offset, line := range lines[permissionsIdx+1 : permissionsEnd] {
		if parseYAMLMapKey(strings.TrimSpace(line)) == "copilot-requests" {
			copilotPermissionsLog.Printf("copilot-requests already declared in permissions block: replace_existing=%t", replaceExisting)
			if replaceExisting {
				position := permissionsIdx + 1 + offset
				result := append([]string(nil), lines[:position]...)
				result = append(result, getIndentation(line)+"copilot-requests: "+level)
				return append(result, lines[position+1:]...)
			}
			return lines
		}
	}
	copilotPermissionsLog.Printf("Appending copilot-requests: %s to existing permissions block", level)
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:permissionsEnd]...)
	result = append(result, permissionsIndent+"  copilot-requests: "+level)
	return append(result, lines[permissionsEnd:]...)
}

// copilotRequestsPermissionPresentInLines distinguishes idempotent injection from
// a permissions field that cannot accept a new mapping entry.
func copilotRequestsPermissionPresentInLines(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") && parseYAMLMapKey(trimmed) == "copilot-requests" {
			return true
		}
	}
	return false
}
