package workflow

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var claudeToolsLog = logger.New("workflow:claude_tools")

// expandNeutralToolsToClaudeTools converts neutral tool names to Claude-specific tool configurations
func (e *ClaudeEngine) expandNeutralToolsToClaudeTools(tools map[string]any) map[string]any {
	claudeToolsLog.Printf("Starting neutral tools expansion: input_tools=%d", len(tools))
	result := make(map[string]any)

	neutralToolCount := 0
	// Count neutral tools
	for key := range tools {
		switch key {
		case "bash", "web-fetch", "web-search", "edit", "playwright":
			neutralToolCount++
		}
	}

	if neutralToolCount > 0 {
		claudeToolsLog.Printf("Expanding %d neutral tools to Claude-specific tools", neutralToolCount)
	}

	// Copy existing tools that are not neutral tools
	for key, value := range tools {
		switch key {
		case "bash", "web-fetch", "web-search", "edit", "playwright":
			// These are neutral tools that need conversion - skip copying, will be converted below
			continue
		default:
			// Copy MCP servers and other non-neutral tools as-is
			result[key] = value
		}
	}

	// Create or get existing claude section
	var claudeSection map[string]any
	if existing, hasClaudeSection := result["claude"]; hasClaudeSection {
		if claudeMap, ok := existing.(map[string]any); ok {
			claudeSection = claudeMap
		} else {
			claudeSection = make(map[string]any)
		}
	} else {
		claudeSection = make(map[string]any)
	}

	// Get existing allowed tools from Claude section
	var claudeAllowed map[string]any
	if allowed, hasAllowed := claudeSection["allowed"]; hasAllowed {
		if allowedMap, ok := allowed.(map[string]any); ok {
			claudeAllowed = allowedMap
		} else {
			claudeAllowed = make(map[string]any)
		}
	} else {
		claudeAllowed = make(map[string]any)
	}

	// Convert neutral tools to Claude tools
	if bashTool, hasBash := tools["bash"]; hasBash {
		// bash -> Bash, KillBash, BashOutput
		if bashCommands, ok := bashTool.([]any); ok {
			claudeAllowed["Bash"] = bashCommands
		} else {
			claudeAllowed["Bash"] = nil // Allow all bash commands
		}
	}

	if _, hasWebFetch := tools["web-fetch"]; hasWebFetch {
		// web-fetch -> WebFetch
		claudeAllowed["WebFetch"] = nil
	}

	if _, hasWebSearch := tools["web-search"]; hasWebSearch {
		// web-search -> WebSearch
		claudeAllowed["WebSearch"] = nil
	}

	if editTool, hasEdit := tools["edit"]; hasEdit {
		// edit -> Edit, MultiEdit, NotebookEdit, Write
		claudeAllowed["Edit"] = nil
		claudeAllowed["MultiEdit"] = nil
		claudeAllowed["NotebookEdit"] = nil
		claudeAllowed["Write"] = nil

		// If edit tool has specific configuration, we could handle it here
		// For now, treating it as enabling all edit capabilities
		_ = editTool
	}

	// Handle playwright tool by converting it to an MCP tool configuration
	if _, hasPlaywright := tools["playwright"]; hasPlaywright {
		// Create playwright as an MCP tool with the same tools available as copilot agent
		playwrightMCP := map[string]any{
			"allowed": GetCopilotAgentPlaywrightTools(),
		}
		result["playwright"] = playwrightMCP
	}

	// Update claude section
	claudeSection["allowed"] = claudeAllowed
	result["claude"] = claudeSection

	claudeToolsLog.Printf("Expansion complete: result_tools=%d, claude_allowed=%d", len(result), len(claudeAllowed))
	return result
}

// computeAllowedClaudeToolsString generates the tool specification string for Claude's --allowed-tools flag.
//
// Why --allowed-tools instead of --tools (introduced in v2.0.31)?
// While --tools is simpler (e.g., "Bash,Edit,Read"), it lacks the fine-grained control gh-aw requires:
// - Specific bash commands: Bash(git:*), Bash(ls)
// - MCP tool prefixes: mcp__github__issue_read, mcp__github__*
// - Path-specific access: Read(/tmp/gh-aw/cache-memory/*)
//
// This function:
// 1. validates that only neutral tools are provided (no claude section)
// 2. converts neutral tools to Claude-specific tools format
// 3. adds default Claude tools and git commands based on safe outputs configuration
// 4. generates the allowed tools string for Claude
func (e *ClaudeEngine) computeAllowedClaudeToolsString(tools map[string]any, safeOutputs *SafeOutputsConfig, cacheMemoryConfig *CacheMemoryConfig) string {
	claudeToolsLog.Print("Computing allowed Claude tools string")

	// Initialize tools map if nil
	if tools == nil {
		tools = make(map[string]any)
	}

	// Enforce that only neutral tools are provided - fail if claude section is present
	if _, hasClaudeSection := tools["claude"]; hasClaudeSection {
		claudeToolsLog.Print("ERROR: Claude section found in input tools, should only contain neutral tools")
		panic("computeAllowedClaudeToolsString should only receive neutral tools, not claude section tools")
	}

	// Convert neutral tools to Claude-specific tools
	claudeToolsLog.Print("Converting neutral tools to Claude-specific format")
	tools = e.expandNeutralToolsToClaudeTools(tools)

	defaultClaudeTools := []string{
		"Task",
		"Glob",
		"Grep",
		"ExitPlanMode",
		"TodoWrite",
		"LS",
		"Read",
		"NotebookRead",
	}

	// Ensure claude section exists with the new format
	var claudeSection map[string]any
	if existing, hasClaudeSection := tools["claude"]; hasClaudeSection {
		if claudeMap, ok := existing.(map[string]any); ok {
			claudeSection = claudeMap
		} else {
			claudeSection = make(map[string]any)
		}
	} else {
		claudeSection = make(map[string]any)
	}

	// Get existing allowed tools from the new format (map structure)
	var claudeExistingAllowed map[string]any
	if allowed, hasAllowed := claudeSection["allowed"]; hasAllowed {
		if allowedMap, ok := allowed.(map[string]any); ok {
			claudeExistingAllowed = allowedMap
		} else {
			claudeExistingAllowed = make(map[string]any)
		}
	} else {
		claudeExistingAllowed = make(map[string]any)
	}

	// Add default tools that aren't already present
	for _, defaultTool := range defaultClaudeTools {
		if _, exists := claudeExistingAllowed[defaultTool]; !exists {
			claudeExistingAllowed[defaultTool] = nil // Add tool with null value
		}
	}

	// Check if Bash tools are present and add implicit KillBash and BashOutput
	if _, hasBash := claudeExistingAllowed["Bash"]; hasBash {
		// Implicitly add KillBash and BashOutput when any Bash tools are allowed
		if _, exists := claudeExistingAllowed["KillBash"]; !exists {
			claudeExistingAllowed["KillBash"] = nil
		}
		if _, exists := claudeExistingAllowed["BashOutput"]; !exists {
			claudeExistingAllowed["BashOutput"] = nil
		}
	}

	// Update the claude section with the new format
	claudeSection["allowed"] = claudeExistingAllowed
	tools["claude"] = claudeSection

	claudeToolsLog.Printf("Added %d default Claude tools to allowed list", len(defaultClaudeTools))

	var allowedTools []string

	// Process claude-specific tools from the claude section (new format only)
	if claudeSection, hasClaudeSection := tools["claude"]; hasClaudeSection {
		if claudeConfig, ok := claudeSection.(map[string]any); ok {
			if allowed, hasAllowed := claudeConfig["allowed"]; hasAllowed {
				// In the new format, allowed is a map where keys are tool names
				if allowedMap, ok := allowed.(map[string]any); ok {
					for toolName, toolValue := range allowedMap {
						if toolName == "Bash" {
							// Handle Bash tool with specific commands
							if bashCommands, ok := toolValue.([]any); ok {
								// Check for :* wildcard first - if present, ignore all other bash commands
								for _, cmd := range bashCommands {
									if cmdStr, ok := cmd.(string); ok {
										if cmdStr == ":*" {
											// :* means allow all bash and ignore other commands
											allowedTools = append(allowedTools, "Bash")
											goto nextClaudeTool
										}
									}
								}
								// Process the allowed bash commands (no :* found)
								for _, cmd := range bashCommands {
									if cmdStr, ok := cmd.(string); ok {
										if cmdStr == "*" {
											// Wildcard means allow all bash
											allowedTools = append(allowedTools, "Bash")
											goto nextClaudeTool
										}
									}
								}
								// Add individual bash commands with Bash() prefix
								for _, cmd := range bashCommands {
									if cmdStr, ok := cmd.(string); ok {
										allowedTools = append(allowedTools, fmt.Sprintf("Bash(%s)", cmdStr))
									}
								}
							} else {
								// Bash with no specific commands or null value - allow all bash
								allowedTools = append(allowedTools, "Bash")
							}
						} else if strings.HasPrefix(toolName, strings.ToUpper(toolName[:1])) {
							// Tool name starts with uppercase letter - regular Claude tool
							allowedTools = append(allowedTools, toolName)
						}
					nextClaudeTool:
					}
				}
			}
		}
	}

	// Process top-level tools (MCP tools and claude)
	for toolName, toolValue := range tools {
		if toolName == "claude" {
			// Skip the claude section as we've already processed it
			continue
		} else {
			// Handle cache-memory as a special case - it provides file system access but no MCP tool
			if toolName == "cache-memory" {
				// Cache-memory provides file share access
				// Default cache uses /tmp/gh-aw/cache-memory/, others use /tmp/gh-aw/cache-memory-{id}/
				// Add path-specific Read and Write tools for each cache directory
				if cacheMemoryConfig != nil {
					for _, cache := range cacheMemoryConfig.Caches {
						var cacheDirPattern string
						if cache.ID == "default" {
							cacheDirPattern = "/tmp/gh-aw/cache-memory/*"
						} else {
							cacheDirPattern = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s/*", cache.ID)
						}

						// Add path-specific tools for cache directory access
						if !slices.Contains(allowedTools, fmt.Sprintf("Read(%s)", cacheDirPattern)) {
							allowedTools = append(allowedTools, fmt.Sprintf("Read(%s)", cacheDirPattern))
						}
						if !slices.Contains(allowedTools, fmt.Sprintf("Write(%s)", cacheDirPattern)) {
							allowedTools = append(allowedTools, fmt.Sprintf("Write(%s)", cacheDirPattern))
						}
						if !slices.Contains(allowedTools, fmt.Sprintf("Edit(%s)", cacheDirPattern)) {
							allowedTools = append(allowedTools, fmt.Sprintf("Edit(%s)", cacheDirPattern))
						}
						if !slices.Contains(allowedTools, fmt.Sprintf("MultiEdit(%s)", cacheDirPattern)) {
							allowedTools = append(allowedTools, fmt.Sprintf("MultiEdit(%s)", cacheDirPattern))
						}
					}
				}
				continue
			}

			// Check if this is an MCP tool (has MCP-compatible type) or standard MCP tool (github)
			if mcpConfig, ok := toolValue.(map[string]any); ok {
				// Check if it's explicitly marked as MCP type
				isCustomMCP := false
				if hasMcp, _ := hasMCPConfig(mcpConfig); hasMcp {
					isCustomMCP = true
				}

				// Handle standard MCP tools (github, playwright) or tools with MCP-compatible type
				if toolName == "github" {
					// Parse GitHub tool configuration for type safety
					githubConfig := parseGitHubTool(toolValue)
					if githubConfig != nil && len(githubConfig.Allowed) > 0 {
						// Check for wildcard access first
						hasWildcard := false
						for _, tool := range githubConfig.Allowed {
							if string(tool) == "*" {
								hasWildcard = true
								break
							}
						}

						if hasWildcard {
							// For wildcard access, just add the server name with mcp__ prefix
							allowedTools = append(allowedTools, fmt.Sprintf("mcp__%s", toolName))
						} else {
							// For specific tools, add each one individually
							for _, tool := range githubConfig.Allowed {
								allowedTools = append(allowedTools, fmt.Sprintf("mcp__%s__%s", toolName, string(tool)))
							}
						}
					} else {
						// For GitHub tools without explicit allowed list, use appropriate default GitHub tools based on mode
						githubMode := getGitHubType(mcpConfig)
						var defaultTools []string
						if githubMode == "remote" {
							defaultTools = constants.DefaultGitHubToolsRemote
						} else {
							defaultTools = constants.DefaultGitHubToolsLocal
						}
						for _, defaultTool := range defaultTools {
							allowedTools = append(allowedTools, fmt.Sprintf("mcp__github__%s", defaultTool))
						}
					}
				} else if toolName == "playwright" || isCustomMCP {
					// Handle playwright and custom MCP tools with generic parsing
					if allowed, hasAllowed := mcpConfig["allowed"]; hasAllowed {
						if allowedSlice, ok := allowed.([]any); ok {
							// Check for wildcard access first
							hasWildcard := false
							for _, item := range allowedSlice {
								if str, ok := item.(string); ok && str == "*" {
									hasWildcard = true
									break
								}
							}

							if hasWildcard {
								// For wildcard access, just add the server name with mcp__ prefix
								allowedTools = append(allowedTools, fmt.Sprintf("mcp__%s", toolName))
							} else {
								// For specific tools, add each one individually
								for _, item := range allowedSlice {
									if str, ok := item.(string); ok {
										allowedTools = append(allowedTools, fmt.Sprintf("mcp__%s__%s", toolName, str))
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Handle SafeOutputs requirement for file write access
	if safeOutputs != nil {
		// Check if a general "Write" permission is already granted
		hasGeneralWrite := slices.Contains(allowedTools, "Write")

		// If no general Write permission and SafeOutputs is configured,
		// add specific write permission for GH_AW_SAFE_OUTPUTS
		if !hasGeneralWrite {
			allowedTools = append(allowedTools, "Write")
			// Ideally we would only give permission to the exact file, but that doesn't seem
			// to be working with Claude. See https://github.com/github/gh-aw/issues/244#issuecomment-3240319103
			//allowedTools = append(allowedTools, "Write(${{ env.GH_AW_SAFE_OUTPUTS }})")
		}
	}

	// Sort the allowed tools alphabetically for consistent output
	sort.Strings(allowedTools)

	if log.Enabled() {
		claudeToolsLog.Printf("Generated allowed tools string with %d tools", len(allowedTools))
	}

	return strings.Join(allowedTools, ",")
}

// generateAllowedToolsComment generates a multi-line comment showing each allowed tool
func (e *ClaudeEngine) generateAllowedToolsComment(allowedToolsStr string, indent string) string {
	if allowedToolsStr == "" {
		return ""
	}

	tools := strings.Split(allowedToolsStr, ",")
	if len(tools) == 0 {
		return ""
	}

	var comment strings.Builder
	comment.WriteString(indent + "# Allowed tools (sorted):\n")
	for _, tool := range tools {
		fmt.Fprintf(&comment, "%s# - %s\n", indent, tool)
	}

	return comment.String()
}
