// This file provides Copilot engine tool permission and error pattern logic.
//
// This file handles three key responsibilities:
//
//  1. Tool Permission Arguments (computeCopilotToolArguments):
//     Converts workflow tool configurations into --allow-tool flags for Copilot CLI.
//     Handles bash/shell tools, edit tools, safe outputs, safe inputs, and MCP servers.
//     Supports granular permissions (e.g., "github(get_file)") and server-level wildcards.
//
//  2. Tool Argument Comments (generateCopilotToolArgumentsComment):
//     Generates human-readable comments documenting which tool permissions are granted.
//     Used in compiled workflows for transparency and debugging.
//
//  3. Error Patterns (GetErrorPatterns):
//     Defines regex patterns for extracting error messages from Copilot CLI logs.
//     Includes timestamped log formats, command failures, module errors, and permission issues.
//     Used by log parsers to detect and categorize errors.
//
// These functions are grouped together because they all relate to tool configuration
// and error handling in the Copilot engine.

package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// computeCopilotToolArguments computes the --allow-tool arguments for Copilot CLI based on tool configurations.
// It handles bash/shell tools, edit tools, safe outputs, safe inputs, and MCP server tools.
// Returns a sorted list of arguments ready to be passed to the Copilot CLI.
func (e *CopilotEngine) computeCopilotToolArguments(tools map[string]any, safeOutputs *SafeOutputsConfig, safeInputs *SafeInputsConfig, workflowData *WorkflowData) []string {
	if tools == nil {
		tools = make(map[string]any)
	}

	var args []string

	// Check if bash has wildcard - if so, use --allow-all-tools instead
	if bashConfig, hasBash := tools["bash"]; hasBash {
		if bashCommands, ok := bashConfig.([]any); ok {
			// Check for :* or * wildcard - if present, allow all tools
			for _, cmd := range bashCommands {
				if cmdStr, ok := cmd.(string); ok {
					if cmdStr == ":*" || cmdStr == "*" {
						// Use --allow-all-tools flag instead of individual tool permissions
						return []string{"--allow-all-tools"}
					}
				}
			}
		}
	}

	// Handle bash/shell tools (when no wildcard)
	if bashConfig, hasBash := tools["bash"]; hasBash {
		if bashCommands, ok := bashConfig.([]any); ok {
			// Add specific shell commands
			for _, cmd := range bashCommands {
				if cmdStr, ok := cmd.(string); ok {
					args = append(args, "--allow-tool", fmt.Sprintf("shell(%s)", cmdStr))
				}
			}
		} else {
			// Bash with no specific commands or null value - allow all shell
			args = append(args, "--allow-tool", "shell")
		}
	}

	// Handle edit tools requirement for file write access
	// Note: safe-outputs do not need write permission as they use MCP
	if _, hasEdit := tools["edit"]; hasEdit {
		args = append(args, "--allow-tool", "write")
	}

	// Handle safe_outputs MCP server - allow all tools if safe outputs are enabled
	// This includes both safeOutputs config and safeOutputs.Jobs
	if HasSafeOutputsEnabled(safeOutputs) {
		args = append(args, "--allow-tool", constants.SafeOutputsMCPServerID)
	}

	// Handle safe_inputs MCP server - allow the server if safe inputs are configured and feature flag is enabled
	if IsSafeInputsEnabled(safeInputs, workflowData) {
		args = append(args, "--allow-tool", constants.SafeInputsMCPServerID)
	}

	// Handle web-fetch builtin tool (Copilot CLI uses web_fetch with underscore)
	if _, hasWebFetch := tools["web-fetch"]; hasWebFetch {
		// web-fetch -> web_fetch
		args = append(args, "--allow-tool", "web_fetch")
	}

	// Built-in tool names that should be skipped when processing MCP servers
	// Note: GitHub is NOT included here because it needs MCP configuration in CLI mode
	// Note: web-fetch is NOT included here because it needs explicit --allow-tool argument
	builtInTools := map[string]bool{
		"bash":       true,
		"edit":       true,
		"web-search": true,
		"playwright": true,
	}

	// Handle MCP server tools
	for toolName, toolConfig := range tools {
		// Skip built-in tools we've already handled
		if builtInTools[toolName] {
			continue
		}

		// GitHub is a special case - it's an MCP server but doesn't have explicit MCP config in the workflow
		// It gets MCP configuration through the parser's processBuiltinMCPTool
		if toolName == "github" {
			if toolConfigMap, ok := toolConfig.(map[string]any); ok {
				if allowed, hasAllowed := toolConfigMap["allowed"]; hasAllowed {
					if allowedList, ok := allowed.([]any); ok {
						// Process allowed list in a single pass
						hasWildcard := false
						for _, allowedTool := range allowedList {
							if toolStr, ok := allowedTool.(string); ok {
								if toolStr == "*" {
									// Wildcard means allow entire GitHub MCP server
									hasWildcard = true
								} else {
									// Add individual tool permission
									args = append(args, "--allow-tool", fmt.Sprintf("github(%s)", toolStr))
								}
							}
						}

						// Add server-level permission only if wildcard was present
						if hasWildcard {
							args = append(args, "--allow-tool", "github")
						}
					}
				} else {
					// No allowed field specified - allow entire GitHub MCP server
					args = append(args, "--allow-tool", "github")
				}
			} else {
				// GitHub tool exists but is not a map (e.g., github: null) - allow entire server
				args = append(args, "--allow-tool", "github")
			}
			continue
		}

		// Check if this is an MCP server configuration
		if toolConfigMap, ok := toolConfig.(map[string]any); ok {
			if hasMcp, _ := hasMCPConfig(toolConfigMap); hasMcp {
				// Allow the entire MCP server
				args = append(args, "--allow-tool", toolName)

				// If it has specific allowed tools, add them individually
				if allowed, hasAllowed := toolConfigMap["allowed"]; hasAllowed {
					if allowedList, ok := allowed.([]any); ok {
						for _, allowedTool := range allowedList {
							if toolStr, ok := allowedTool.(string); ok {
								args = append(args, "--allow-tool", fmt.Sprintf("%s(%s)", toolName, toolStr))
							}
						}
					}
				}
			}
		}
	}

	// Simple sort - extract values, sort them, and rebuild args
	if len(args) > 0 {
		var values []string
		for i := 1; i < len(args); i += 2 {
			values = append(values, args[i])
		}
		sort.Strings(values)

		// Rebuild args with sorted values
		newArgs := make([]string, 0, len(args))
		for _, value := range values {
			newArgs = append(newArgs, "--allow-tool", value)
		}
		args = newArgs
	}

	return args
}

// generateCopilotToolArgumentsComment generates a multi-line comment showing each tool argument.
// This is used to document which tool permissions are being granted in the compiled workflow.
func (e *CopilotEngine) generateCopilotToolArgumentsComment(tools map[string]any, safeOutputs *SafeOutputsConfig, safeInputs *SafeInputsConfig, workflowData *WorkflowData, indent string) string {
	toolArgs := e.computeCopilotToolArguments(tools, safeOutputs, safeInputs, workflowData)
	if len(toolArgs) == 0 {
		return ""
	}

	var comment strings.Builder
	comment.WriteString(indent + "# Copilot CLI tool arguments (sorted):\n")

	// Group flag-value pairs for better readability
	for i := 0; i < len(toolArgs); i += 2 {
		if i+1 < len(toolArgs) {
			fmt.Fprintf(&comment, "%s# %s %s\n", indent, toolArgs[i], toolArgs[i+1])
		}
	}

	return comment.String()
}
