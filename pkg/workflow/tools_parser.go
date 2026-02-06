// This file provides tool configuration parsing for agentic workflows.
//
// This file handles parsing of tool configurations from the frontmatter tools section.
// It extracts and validates tool configurations for all supported tools, converting
// YAML-parsed maps into strongly-typed Go structs.
//
// # Organization Rationale
//
// All tool parsing functions are grouped in this file because they:
//   - Share a common purpose (tool configuration parsing)
//   - Follow similar parsing patterns (map[string]any -> struct)
//   - Are called together during workflow compilation
//   - Provide a single source of truth for tool configuration
//
// This follows established patterns where domain-specific parsing is grouped by
// functionality rather than scattered across files. See skills/developer/SKILL.md
// for code organization principles.
//
// # Supported Tools
//
// Built-in Tools:
//   - github: GitHub API and repository operations
//   - bash: Shell command execution
//   - web-fetch: HTTP content fetching
//   - web-search: Web search capabilities
//   - edit: File editing operations
//   - playwright: Browser automation
//   - serena: Serena integration
//   - agentic-workflows: Nested workflow execution
//   - cache-memory: In-workflow memory caching
//   - repo-memory: Repository-backed persistent memory
//
// Configuration Tools:
//   - safety-prompt: Safety prompt injection
//   - timeout: Agent timeout configuration
//   - startup-timeout: Agent startup timeout
//
// Custom Tools:
//   - MCP servers and other custom tool configurations
//
// # Parse Function Pattern
//
// Each parse function follows the pattern:
//  1. Accept any type to handle various YAML representations
//  2. Type-assert to expected structure (bool, string, map, array)
//  3. Extract and validate configuration values
//  4. Return strongly-typed configuration struct
//
// This provides type safety while accommodating flexible YAML syntax.

package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var toolsParserLog = logger.New("workflow:tools_parser")

// NewTools creates a new Tools instance from a map
func NewTools(toolsMap map[string]any) *Tools {
	toolsParserLog.Printf("Creating tools configuration from map with %d entries", len(toolsMap))
	if toolsMap == nil {
		return &Tools{
			Custom: make(map[string]MCPServerConfig),
			raw:    make(map[string]any),
		}
	}

	tools := &Tools{
		Custom: make(map[string]MCPServerConfig),
		raw:    make(map[string]any),
	}

	// Copy raw map
	for k, v := range toolsMap {
		tools.raw[k] = v
	}

	// Extract and parse known tools
	if val, exists := toolsMap["github"]; exists {
		tools.GitHub = parseGitHubTool(val)
	}
	if val, exists := toolsMap["bash"]; exists {
		tools.Bash = parseBashTool(val)
		// Check if parsing returned nil - this indicates invalid configuration
		if tools.Bash == nil {
			toolsParserLog.Print("Warning: bash tool configuration is invalid (nil/anonymous syntax not supported)")
		}
	}
	if val, exists := toolsMap["web-fetch"]; exists {
		tools.WebFetch = parseWebFetchTool(val)
	}
	if val, exists := toolsMap["web-search"]; exists {
		tools.WebSearch = parseWebSearchTool(val)
	}
	if val, exists := toolsMap["edit"]; exists {
		tools.Edit = parseEditTool(val)
	}
	if val, exists := toolsMap["playwright"]; exists {
		tools.Playwright = parsePlaywrightTool(val)
	}
	if val, exists := toolsMap["serena"]; exists {
		tools.Serena = parseSerenaTool(val)
	}
	if val, exists := toolsMap["agentic-workflows"]; exists {
		tools.AgenticWorkflows = parseAgenticWorkflowsTool(val)
	}
	if val, exists := toolsMap["cache-memory"]; exists {
		tools.CacheMemory = parseCacheMemoryTool(val)
	}
	if val, exists := toolsMap["repo-memory"]; exists {
		tools.RepoMemory = parseRepoMemoryTool(val)
	}
	if val, exists := toolsMap["timeout"]; exists {
		tools.Timeout = parseTimeoutTool(val)
	}
	if val, exists := toolsMap["startup-timeout"]; exists {
		tools.StartupTimeout = parseStartupTimeoutTool(val)
	}

	// Extract custom MCP tools (anything not in the known list)
	knownTools := map[string]bool{
		"github":            true,
		"bash":              true,
		"web-fetch":         true,
		"web-search":        true,
		"edit":              true,
		"playwright":        true,
		"serena":            true,
		"agentic-workflows": true,
		"cache-memory":      true,
		"repo-memory":       true,
		"safety-prompt":     true,
		"timeout":           true,
		"startup-timeout":   true,
	}

	customCount := 0
	for name, config := range toolsMap {
		if !knownTools[name] {
			tools.Custom[name] = parseMCPServerConfig(config)
			customCount++
		}
	}

	toolsParserLog.Printf("Parsed tools: github=%v, bash=%v, playwright=%v, serena=%v, custom=%d", tools.GitHub != nil, tools.Bash != nil, tools.Playwright != nil, tools.Serena != nil, customCount)
	return tools
}

// parseGitHubTool converts raw github tool configuration to GitHubToolConfig
func parseGitHubTool(val any) *GitHubToolConfig {
	if val == nil {
		toolsParserLog.Print("GitHub tool enabled with default configuration")
		return &GitHubToolConfig{
			ReadOnly: true, // default to read-only for security
		}
	}

	// Handle string type (simple enable)
	if _, ok := val.(string); ok {
		toolsParserLog.Print("GitHub tool enabled with string configuration")
		return &GitHubToolConfig{
			ReadOnly: true, // default to read-only for security
		}
	}

	// Handle map type (detailed configuration)
	if configMap, ok := val.(map[string]any); ok {
		toolsParserLog.Print("Parsing GitHub tool detailed configuration")
		config := &GitHubToolConfig{
			ReadOnly: true, // default to read-only for security
		}

		if allowed, ok := configMap["allowed"].([]any); ok {
			config.Allowed = make(GitHubAllowedTools, 0, len(allowed))
			for _, item := range allowed {
				if str, ok := item.(string); ok {
					config.Allowed = append(config.Allowed, GitHubToolName(str))
				}
			}
		}

		if mode, ok := configMap["mode"].(string); ok {
			config.Mode = mode
		}

		if version, ok := configMap["version"].(string); ok {
			config.Version = version
		}

		if args, ok := configMap["args"].([]any); ok {
			config.Args = make([]string, 0, len(args))
			for _, item := range args {
				if str, ok := item.(string); ok {
					config.Args = append(config.Args, str)
				}
			}
		}

		if readOnly, ok := configMap["read-only"].(bool); ok {
			config.ReadOnly = readOnly
		}
		// else: defaults to true (set above)

		if token, ok := configMap["github-token"].(string); ok {
			config.GitHubToken = token
		}

		// Check for both "toolset" and "toolsets" (plural is more common in user configs)
		if toolset, ok := configMap["toolsets"].([]any); ok {
			config.Toolset = make(GitHubToolsets, 0, len(toolset))
			for _, item := range toolset {
				if str, ok := item.(string); ok {
					config.Toolset = append(config.Toolset, GitHubToolset(str))
				}
			}
		} else if toolset, ok := configMap["toolset"].([]any); ok {
			config.Toolset = make(GitHubToolsets, 0, len(toolset))
			for _, item := range toolset {
				if str, ok := item.(string); ok {
					config.Toolset = append(config.Toolset, GitHubToolset(str))
				}
			}
		}

		if lockdown, ok := configMap["lockdown"].(bool); ok {
			config.Lockdown = lockdown
		}

		// Parse app configuration for GitHub App token minting
		if app, ok := configMap["app"].(map[string]any); ok {
			config.App = parseAppConfig(app)
		}

		return config
	}

	return &GitHubToolConfig{
		ReadOnly: true, // default to read-only for security
	}
}

// parseBashTool converts raw bash tool configuration to BashToolConfig
func parseBashTool(val any) *BashToolConfig {
	if val == nil {
		// nil is no longer supported - return nil to indicate invalid configuration
		// The compiler will handle this as a validation error
		return nil
	}

	// Handle boolean values
	if boolVal, ok := val.(bool); ok {
		if boolVal {
			// bash: true means all commands allowed
			return &BashToolConfig{}
		}
		// bash: false means explicitly disabled
		return &BashToolConfig{
			AllowedCommands: []string{}, // Empty slice indicates explicitly disabled
		}
	}

	// Handle array of allowed commands
	if cmdArray, ok := val.([]any); ok {
		config := &BashToolConfig{
			AllowedCommands: make([]string, 0, len(cmdArray)),
		}
		for _, item := range cmdArray {
			if str, ok := item.(string); ok {
				config.AllowedCommands = append(config.AllowedCommands, str)
			}
		}
		return config
	}

	// Invalid configuration
	return nil
}

// parsePlaywrightTool converts raw playwright tool configuration to PlaywrightToolConfig
func parsePlaywrightTool(val any) *PlaywrightToolConfig {
	if val == nil {
		return &PlaywrightToolConfig{}
	}

	if configMap, ok := val.(map[string]any); ok {
		config := &PlaywrightToolConfig{}

		// Handle version field - can be string or number
		if version, ok := configMap["version"].(string); ok {
			config.Version = version
		} else if versionNum, ok := configMap["version"].(int); ok {
			config.Version = fmt.Sprintf("%d", versionNum)
		} else if versionNum, ok := configMap["version"].(int64); ok {
			config.Version = fmt.Sprintf("%d", versionNum)
		} else if versionNum, ok := configMap["version"].(float64); ok {
			config.Version = fmt.Sprintf("%g", versionNum)
		}

		// Handle allowed_domains - can be string or array
		if allowedDomains, ok := configMap["allowed_domains"]; ok {
			if str, ok := allowedDomains.(string); ok {
				config.AllowedDomains = PlaywrightAllowedDomains{PlaywrightDomain(str)}
			} else if arr, ok := allowedDomains.([]any); ok {
				config.AllowedDomains = make(PlaywrightAllowedDomains, 0, len(arr))
				for _, item := range arr {
					if str, ok := item.(string); ok {
						config.AllowedDomains = append(config.AllowedDomains, PlaywrightDomain(str))
					}
				}
			} else if arr, ok := allowedDomains.([]string); ok {
				config.AllowedDomains = make(PlaywrightAllowedDomains, 0, len(arr))
				for _, str := range arr {
					config.AllowedDomains = append(config.AllowedDomains, PlaywrightDomain(str))
				}
			}
		}

		// Handle args field - can be []any or []string
		if argsValue, ok := configMap["args"]; ok {
			if arr, ok := argsValue.([]any); ok {
				config.Args = make([]string, 0, len(arr))
				for _, item := range arr {
					if str, ok := item.(string); ok {
						config.Args = append(config.Args, str)
					}
				}
			} else if arr, ok := argsValue.([]string); ok {
				config.Args = arr
			}
		}

		return config
	}

	return &PlaywrightToolConfig{}
}

// parseSerenaTool converts raw serena tool configuration to SerenaToolConfig
func parseSerenaTool(val any) *SerenaToolConfig {
	if val == nil {
		return &SerenaToolConfig{}
	}

	// Handle array format (short syntax): ["go", "typescript"]
	if langArray, ok := val.([]any); ok {
		config := &SerenaToolConfig{
			ShortSyntax: make([]string, 0, len(langArray)),
		}
		for _, item := range langArray {
			if str, ok := item.(string); ok {
				config.ShortSyntax = append(config.ShortSyntax, str)
			}
		}
		return config
	}

	// Handle object format with detailed configuration
	if configMap, ok := val.(map[string]any); ok {
		config := &SerenaToolConfig{}

		if version, ok := configMap["version"].(string); ok {
			config.Version = version
		}

		// Parse mode field
		if mode, ok := configMap["mode"].(string); ok {
			config.Mode = mode
		}

		if args, ok := configMap["args"].([]any); ok {
			config.Args = make([]string, 0, len(args))
			for _, item := range args {
				if str, ok := item.(string); ok {
					config.Args = append(config.Args, str)
				}
			}
		}

		// Parse languages configuration
		if languagesVal, ok := configMap["languages"].(map[string]any); ok {
			config.Languages = make(map[string]*SerenaLangConfig)
			for langName, langVal := range languagesVal {
				if langVal == nil {
					// nil means enable with defaults
					config.Languages[langName] = &SerenaLangConfig{}
					continue
				}
				if langMap, ok := langVal.(map[string]any); ok {
					langConfig := &SerenaLangConfig{}
					if version, ok := langMap["version"].(string); ok {
						langConfig.Version = version
					} else if versionNum, ok := langMap["version"].(float64); ok {
						// Convert numeric version to string
						langConfig.Version = fmt.Sprintf("%.0f", versionNum)
					}
					// Parse Go-specific fields
					if langName == "go" {
						if goModFile, ok := langMap["go-mod-file"].(string); ok {
							langConfig.GoModFile = goModFile
						}
						if goplsVersion, ok := langMap["gopls-version"].(string); ok {
							langConfig.GoplsVersion = goplsVersion
						}
					}
					config.Languages[langName] = langConfig
				}
			}
		}

		return config
	}

	return &SerenaToolConfig{}
}

// parseWebFetchTool converts raw web-fetch tool configuration
func parseWebFetchTool(val any) *WebFetchToolConfig {
	// web-fetch is either nil or an empty object
	return &WebFetchToolConfig{}
}

// parseWebSearchTool converts raw web-search tool configuration
func parseWebSearchTool(val any) *WebSearchToolConfig {
	// web-search is either nil or an empty object
	return &WebSearchToolConfig{}
}

// parseEditTool converts raw edit tool configuration
func parseEditTool(val any) *EditToolConfig {
	// edit is either nil or an empty object
	return &EditToolConfig{}
}

// parseAgenticWorkflowsTool converts raw agentic-workflows tool configuration
func parseAgenticWorkflowsTool(val any) *AgenticWorkflowsToolConfig {
	config := &AgenticWorkflowsToolConfig{}

	if boolVal, ok := val.(bool); ok {
		config.Enabled = boolVal
	} else if val == nil {
		config.Enabled = true // nil means enabled
	}

	return config
}

// parseCacheMemoryTool converts raw cache-memory tool configuration
func parseCacheMemoryTool(val any) *CacheMemoryToolConfig {
	// cache-memory can be boolean, object, or array - store raw value
	return &CacheMemoryToolConfig{Raw: val}
}

// parseRepoMemoryTool converts raw repo-memory tool configuration
func parseRepoMemoryTool(val any) *RepoMemoryToolConfig {
	// repo-memory can be boolean, object, or array - store raw value
	return &RepoMemoryToolConfig{Raw: val}
}

// parseTimeoutTool converts raw timeout tool configuration
func parseTimeoutTool(val any) *int {
	if intVal, ok := val.(int); ok {
		return &intVal
	}
	if floatVal, ok := val.(float64); ok {
		intVal := int(floatVal)
		return &intVal
	}
	return nil
}

// parseStartupTimeoutTool converts raw startup-timeout tool configuration
func parseStartupTimeoutTool(val any) *int {
	if intVal, ok := val.(int); ok {
		return &intVal
	}
	if floatVal, ok := val.(float64); ok {
		intVal := int(floatVal)
		return &intVal
	}
	return nil
}

// parseMCPServerConfig converts raw MCP server configuration to MCPServerConfig
func parseMCPServerConfig(val any) MCPServerConfig {
	config := MCPServerConfig{
		CustomFields: make(map[string]any),
	}

	// If val is nil, return empty config
	if val == nil {
		return config
	}

	// If it's not a map, store it as a custom field
	configMap, ok := val.(map[string]any)
	if !ok {
		config.CustomFields["value"] = val
		return config
	}

	// Parse common MCP server fields
	if command, ok := configMap["command"].(string); ok {
		config.Command = command
	}

	if args, ok := configMap["args"].([]any); ok {
		config.Args = make([]string, 0, len(args))
		for _, arg := range args {
			if str, ok := arg.(string); ok {
				config.Args = append(config.Args, str)
			}
		}
	}

	if env, ok := configMap["env"].(map[string]any); ok {
		config.Env = make(map[string]string)
		for k, v := range env {
			if str, ok := v.(string); ok {
				config.Env[k] = str
			}
		}
	}

	if mode, ok := configMap["mode"].(string); ok {
		config.Mode = mode
	}

	if mcpType, ok := configMap["type"].(string); ok {
		config.Type = mcpType
	}

	if version, ok := configMap["version"].(string); ok {
		config.Version = version
	} else if versionNum, ok := configMap["version"].(float64); ok {
		config.Version = fmt.Sprintf("%.0f", versionNum)
	}

	if toolsets, ok := configMap["toolsets"].([]any); ok {
		config.Toolsets = make([]string, 0, len(toolsets))
		for _, item := range toolsets {
			if str, ok := item.(string); ok {
				config.Toolsets = append(config.Toolsets, str)
			}
		}
	}

	// Parse HTTP-specific fields
	if url, ok := configMap["url"].(string); ok {
		config.URL = url
	}

	if headers, ok := configMap["headers"].(map[string]any); ok {
		config.Headers = make(map[string]string)
		for k, v := range headers {
			if str, ok := v.(string); ok {
				config.Headers[k] = str
			}
		}
	}

	// Parse container-specific fields
	if container, ok := configMap["container"].(string); ok {
		config.Container = container
	}

	if entrypoint, ok := configMap["entrypoint"].(string); ok {
		config.Entrypoint = entrypoint
	}

	if entrypointArgs, ok := configMap["entrypointArgs"].([]any); ok {
		config.EntrypointArgs = make([]string, 0, len(entrypointArgs))
		for _, arg := range entrypointArgs {
			if str, ok := arg.(string); ok {
				config.EntrypointArgs = append(config.EntrypointArgs, str)
			}
		}
	}

	if mounts, ok := configMap["mounts"].([]any); ok {
		config.Mounts = make([]string, 0, len(mounts))
		for _, mount := range mounts {
			if str, ok := mount.(string); ok {
				config.Mounts = append(config.Mounts, str)
			}
		}
	}

	// Store any unknown fields in CustomFields
	knownFields := map[string]bool{
		"command":        true,
		"args":           true,
		"env":            true,
		"mode":           true,
		"type":           true,
		"version":        true,
		"toolsets":       true,
		"url":            true,
		"headers":        true,
		"container":      true,
		"entrypoint":     true,
		"entrypointArgs": true,
		"mounts":         true,
	}

	for key, value := range configMap {
		if !knownFields[key] {
			config.CustomFields[key] = value
		}
	}

	return config
}
