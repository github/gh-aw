// This file provides Copilot engine tool permission and error pattern logic.
//
// This file handles three key responsibilities:
//
//  1. Tool Permission Arguments (computeCopilotToolArguments):
//     Converts workflow tool configurations into --allow-tool flags for Copilot CLI.
//     Handles bash/shell tools, edit tools, safe outputs, mcp-scripts, and MCP servers.
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
	"os"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var copilotEngineToolsLog = logger.New("workflow:copilot_engine_tools")

// sanitizeCopilotShellCommand truncates a bash tool command at the first single
// quote to produce a safe prefix for the Copilot CLI --allow-tool shell() argument.
//
// Copilot CLI uses prefix matching for shell() arguments, so shell(jq) matches any
// jq invocation including "jq '.filter' ...". Single quotes in the allow-tool argument
// cause the Copilot CLI to crash at startup because of quoting conflicts in the
// multi-level shell escaping required by the AWF entrypoint.
//
// Returns the sanitized command and whether sanitization was needed.
func sanitizeCopilotShellCommand(cmdStr string) (string, bool) {
	prefix, _, found := strings.Cut(cmdStr, "'")
	if !found {
		return cmdStr, false
	}
	// Trim trailing whitespace from the prefix.
	// shell(jq) prefix-matches any jq invocation, preserving full tool access.
	return strings.TrimRight(prefix, " "), true
}

// computeCopilotToolArguments computes the --allow-tool arguments for Copilot CLI based on tool configurations.
// It handles bash/shell tools, edit tools, safe outputs, mcp-scripts, and MCP server tools.
// Returns a sorted list of arguments ready to be passed to the Copilot CLI.
func (e *CopilotEngine) computeCopilotToolArguments(tools map[string]any, safeOutputs *SafeOutputsConfig, mcpScripts *MCPScriptsConfig, workflowData *WorkflowData) []string { //nolint:largefunc // Existing engine permission mapping is intentionally centralized.
	copilotEngineToolsLog.Printf("Computing tool arguments: tools=%d", len(tools))
	if tools == nil {
		tools = make(map[string]any)
	}

	var args []string
	hasRestrictedBashAllowlist := false
	hasUnrestrictedBash := false

	// Check if bash has wildcard - if so, use --allow-all-tools in CLI mode.
	// SDK mode must keep shell permission scoped because its explicit session
	// catalog can contain non-shell tools that wildcard bash must not authorize.
	if bashConfig, hasBash := tools["bash"]; hasBash {
		if bashCommands, ok := bashConfig.([]any); ok {
			// Check for :* or * wildcard - if present, allow all tools
			for _, cmd := range bashCommands {
				if cmdStr, ok := cmd.(string); ok {
					if cmdStr == ":*" || cmdStr == "*" {
						if copilotNeedsBuiltinMCPs(workflowData) || isCopilotSDKMode(workflowData) {
							hasUnrestrictedBash = true
							break
						}
						// Use --allow-all-tools flag instead of individual tool permissions
						copilotEngineToolsLog.Print("Bash wildcard detected, using --allow-all-tools")
						return []string{"--allow-all-tools"}
					}
				}
			}
		}
	}

	// Handle bash/shell tools (when no wildcard)
	if hasUnrestrictedBash {
		args = append(args, "--allow-tool", "shell")
	} else if bashConfig, hasBash := tools["bash"]; hasBash && isCopilotToolValueEnabled(tools, "bash") {
		if bashCommands, ok := bashConfig.([]any); ok {
			hasRestrictedBashAllowlist = len(bashCommands) > 0
			// Add specific shell commands
			for _, cmd := range bashCommands {
				if cmdStr, ok := cmd.(string); ok {
					// Normalize trailing " *" wildcard (e.g. "jq *" → "jq") so that
					// all engines emit the canonical prefix form (shell(jq)) regardless
					// of whether the command was written with or without the wildcard.
					cmdStr, _ = normalizeBashCommand(cmdStr)
					// For stem commands (like dotnet, npm, cargo), Copilot CLI uses
					// subcommand matching. When the user specifies just the base command
					// (e.g., "dotnet"), append :* so "dotnet build", "dotnet test", etc.
					// are all permitted. Skip if the command already has a colon (explicit
					// matching) or a space (user already specified the subcommand).
					if !strings.Contains(cmdStr, ":") && !strings.Contains(cmdStr, " ") && constants.CopilotStemCommands[cmdStr] {
						args = append(args, "--allow-tool", fmt.Sprintf("shell(%s:*)", cmdStr))
					} else {
						sanitized, wasSanitized := sanitizeCopilotShellCommand(cmdStr)
						if wasSanitized {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
								fmt.Sprintf("bash tool %q contains single quotes that crash Copilot CLI; "+
									"truncated to safe prefix %q for shell() prefix-matching. "+
									"Use %q in your workflow to silence this warning.",
									cmdStr, sanitized, sanitized)))
						}
						args = append(args, "--allow-tool", fmt.Sprintf("shell(%s)", sanitized))
					}
				}
			}
		} else {
			// Bash with no specific commands or null value - allow all shell
			args = append(args, "--allow-tool", "shell")
		}
	}

	// When MCP tools are mounted as CLI commands and bash uses a restricted allowlist,
	// ensure mounted MCP CLI commands are executable via shell(<server>:*).
	// This avoids Copilot CLI permission blocks for mounted commands such as safeoutputs.
	if hasRestrictedBashAllowlist {
		effectiveWorkflowData := buildCLIWorkflowDataForMounts(workflowData, tools, safeOutputs, mcpScripts)

		for _, serverName := range getMountedCLIServerNamesIfBashRestricted(effectiveWorkflowData, tools, safeOutputs, mcpScripts) {
			args = append(args, "--allow-tool", fmt.Sprintf("shell(%s:*)", serverName))
		}
		// When playwright is configured in CLI mode, playwright-cli must be executable.
		// Automatically add shell(playwright-cli:*) to the restricted bash allowlist.
		if isPlaywrightCLIMode(effectiveWorkflowData.Tools) {
			args = append(args, "--allow-tool", "shell(playwright-cli:*)")
		}
		// When GitHub CLI mode is enabled (tools.github.mode: gh-proxy), GitHub access
		// goes through the gh CLI, so allow shell(gh:*).
		if isGitHubCLIModeEnabled(effectiveWorkflowData) {
			args = append(args, "--allow-tool", "shell(gh:*)")
		}
	}

	// Handle edit tools requirement for file write access
	// Note: safe-outputs do not need write permission as they use MCP
	if isCopilotEditToolEnabled(tools, workflowData) {
		copilotEngineToolsLog.Print("Edit tool enabled, adding write permission")
		args = append(args, "--allow-tool", "write")
	}

	// Handle safe_outputs MCP server - allow all tools if safe outputs are enabled
	// This includes both safeOutputs config and safeOutputs.Jobs
	if HasSafeOutputsEnabled(safeOutputs) {
		copilotEngineToolsLog.Print("Safe-outputs enabled, adding MCP server permission")
		args = append(args, "--allow-tool", constants.SafeOutputsMCPServerID.String())
	}

	// Handle mcp_scripts MCP server - allow the server if mcp-scripts are configured and feature flag is enabled
	if IsMCPScriptsEnabled(mcpScripts) {
		args = append(args, "--allow-tool", constants.MCPScriptsMCPServerID.String())
	}

	// Handle web-fetch builtin tool (Copilot CLI uses web_fetch with underscore)
	if isCopilotToolValueEnabled(tools, "web-fetch") {
		copilotEngineToolsLog.Print("Web-fetch tool enabled, adding web_fetch permission")
		// web-fetch -> web_fetch
		args = append(args, "--allow-tool", "web_fetch")
	}

	// Built-in tool names that should be skipped when processing MCP servers
	// Note: GitHub is NOT included here because it needs MCP configuration in CLI mode
	// Note: web-fetch is NOT included here because it needs explicit --allow-tool argument
	builtInTools := map[string]struct {
	}{
		"bash":         {},
		"edit":         {},
		"web-search":   {},
		"playwright":   {},
		"cache-memory": {},
		"drive-memory": {},
	}

	// Handle MCP server tools
	for toolName, toolConfig := range tools {
		// Skip built-in tools we've already handled
		if setutil.Contains(builtInTools, toolName) {
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
				copilotEngineToolsLog.Printf("Adding custom MCP server permission: %s", toolName)
				hasAllowedField := false
				allowWholeServer := true
				if allowed, hasAllowed := toolConfigMap["allowed"]; hasAllowed {
					hasAllowedField = true
					allowWholeServer = false
					if allowedList, ok := allowed.([]any); ok {
						for _, allowedTool := range allowedList {
							if toolStr, ok := allowedTool.(string); ok {
								if toolStr == "*" {
									allowWholeServer = true
									break
								}
								args = append(args, "--allow-tool", fmt.Sprintf("%s(%s)", toolName, toolStr))
							}
						}
					}
				}
				// SDK permission checks use individual entries when an MCP allowlist
				// exists. CLI mode retains the server-wide grant for compatibility.
				if !isCopilotSDKMode(workflowData) || !hasAllowedField || allowWholeServer {
					args = append(args, "--allow-tool", toolName)
				}
			}
		}
	}

	// Sort and deduplicate values, then rebuild args.
	// Deduplication is needed because sanitizeCopilotShellCommand can truncate
	// multiple different commands to the same safe prefix (e.g. several jq filters
	// all become "jq"), producing duplicate --allow-tool shell(jq) entries.
	if len(args) > 0 {
		var values []string
		for i := 1; i < len(args); i += 2 {
			values = append(values, args[i])
		}
		sort.Strings(values)

		// Rebuild args with sorted, deduplicated values
		newArgs := make([]string, 0, len(args))
		prev := ""
		for _, value := range values {
			if value == prev {
				continue
			}
			newArgs = append(newArgs, "--allow-tool", value)
			prev = value
		}
		args = newArgs
	}

	copilotEngineToolsLog.Printf("Computed %d tool arguments", len(args)/2)
	return args
}

// generateCopilotToolArgumentsComment generates a multi-line comment showing each tool argument.
// This is used to document which tool permissions are being granted in the compiled workflow.
func (e *CopilotEngine) generateCopilotToolArgumentsComment(tools map[string]any, safeOutputs *SafeOutputsConfig, mcpScripts *MCPScriptsConfig, workflowData *WorkflowData, indent string) string {
	toolArgs := e.computeCopilotToolArguments(tools, safeOutputs, mcpScripts, workflowData)
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
