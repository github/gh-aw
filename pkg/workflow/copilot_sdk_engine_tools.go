// This file provides Copilot SDK engine tool configuration logic.
//
// Unlike the CLI engine which generates --allow-tool flags, the SDK engine
// builds AvailableTools and ExcludedTools arrays for the SessionConfig.
//
// The SDK uses simple tool name strings instead of the CLI's pattern syntax:
//   - CLI: --allow-tool shell(git) -> SDK: "bash" (with granular control via permissions)
//   - CLI: --allow-tool write -> SDK: "edit"
//   - CLI: --allow-tool github -> SDK: "github"
//   - CLI: --allow-tool web_fetch -> SDK: "web_fetch"
//
// Tool name mapping:
//   - bash/shell -> "bash" in SDK (was "shell" in CLI flags)
//   - edit/write -> "edit" in SDK (was "write" in CLI flags)
//   - github -> "github" (same)
//   - web-fetch -> "web_fetch" (same as CLI)
//   - safe-outputs -> safe_outputs MCP server ID
//   - safe-inputs -> safe_inputs MCP server ID

package workflow

import (
	"sort"

	"github.com/github/gh-aw/pkg/constants"
)

// computeSDKToolConfig computes the AvailableTools and ExcludedTools arrays
// for the SDK SessionConfig based on workflow tool configurations.
func (e *CopilotSDKEngine) computeSDKToolConfig(workflowData *WorkflowData) (available []string, excluded []string) {
	tools := workflowData.Tools
	if tools == nil {
		tools = make(map[string]any)
	}

	safeOutputs := workflowData.SafeOutputs
	safeInputs := workflowData.SafeInputs

	var availableTools []string

	// Check if bash has wildcard - if so, allow all tools
	if bashConfig, hasBash := tools["bash"]; hasBash {
		if bashCommands, ok := bashConfig.([]any); ok {
			for _, cmd := range bashCommands {
				if cmdStr, ok := cmd.(string); ok {
					if cmdStr == ":*" || cmdStr == "*" {
						// Equivalent of --allow-all-tools: return all tools available
						return []string{"*"}, nil
					}
				}
			}
		}
	}

	// Handle bash/shell tools
	if _, hasBash := tools["bash"]; hasBash {
		// SDK uses "bash" for the shell tool
		availableTools = append(availableTools, "bash")
	}

	// Handle edit tools
	if _, hasEdit := tools["edit"]; hasEdit {
		// SDK uses "edit" (CLI uses "write")
		availableTools = append(availableTools, "edit")
	}

	// Handle safe_outputs MCP server
	if HasSafeOutputsEnabled(safeOutputs) {
		availableTools = append(availableTools, constants.SafeOutputsMCPServerID)
	}

	// Handle safe_inputs MCP server
	if IsSafeInputsEnabled(safeInputs, workflowData) {
		availableTools = append(availableTools, constants.SafeInputsMCPServerID)
	}

	// Handle web-fetch
	if _, hasWebFetch := tools["web-fetch"]; hasWebFetch {
		availableTools = append(availableTools, "web_fetch")
	}

	// Built-in tool names that should be skipped for MCP processing
	builtInTools := map[string]bool{
		"bash":       true,
		"edit":       true,
		"web-search": true,
		"playwright": true,
	}

	// Handle MCP server tools (including GitHub)
	for toolName, toolConfig := range tools {
		if builtInTools[toolName] {
			continue
		}

		if toolName == "github" {
			// GitHub MCP server - always allow
			availableTools = append(availableTools, "github")
			continue
		}

		// Check if this is an MCP server configuration
		if toolConfigMap, ok := toolConfig.(map[string]any); ok {
			if hasMcp, _ := hasMCPConfig(toolConfigMap); hasMcp {
				availableTools = append(availableTools, toolName)
			}
		}
	}

	// Sort for consistent output
	sort.Strings(availableTools)

	// Build excluded tools list
	// web-search is excluded by default (not supported)
	var excludedTools []string
	if _, hasWebSearch := tools["web-search"]; !hasWebSearch {
		// Only exclude web_search if it's not explicitly requested
		// (Copilot CLI doesn't support it anyway)
	}

	return availableTools, excludedTools
}
