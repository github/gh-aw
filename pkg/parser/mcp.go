package parser

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var mcpLog = logger.New("parser:mcp")

// ValidMCPTypes defines all supported MCP server types.
// "local" is an alias for "stdio" and gets normalized during parsing.
var ValidMCPTypes = []string{"stdio", "http", "local"}

// IsMCPType checks if a type string is a valid MCP server type.
// Returns true for "stdio", "http", and "local" (which is an alias for "stdio").
func IsMCPType(typeStr string) bool {
	switch typeStr {
	case "stdio", "http", "local":
		return true
	default:
		return false
	}
}

// EnsureLocalhostDomains ensures that localhost and 127.0.0.1 are always included
// in the allowed domains list for Playwright, even when custom domains are specified
// Includes port variations to allow all ports on localhost and 127.0.0.1
func EnsureLocalhostDomains(domains []string) []string {
	hasLocalhost := false
	hasLocalhostPorts := false
	hasLoopback := false
	hasLoopbackPorts := false

	for _, domain := range domains {
		switch domain {
		case "localhost":
			hasLocalhost = true
		case "localhost:*":
			hasLocalhostPorts = true
		case "127.0.0.1":
			hasLoopback = true
		case "127.0.0.1:*":
			hasLoopbackPorts = true
		}
	}

	// CWE-190: Allocation Size Overflow Prevention
	// Instead of pre-calculating capacity (len(domains)+4), which could overflow
	// if domains is extremely large, we let Go's append handle capacity growth
	// automatically. This is safe and efficient for domain arrays which are
	// typically small in practice.
	var result []string

	// Always add localhost domains first (with and without port specifications)
	if !hasLocalhost {
		result = append(result, "localhost")
	}
	if !hasLocalhostPorts {
		result = append(result, "localhost:*")
	}
	if !hasLoopback {
		result = append(result, "127.0.0.1")
	}
	if !hasLoopbackPorts {
		result = append(result, "127.0.0.1:*")
	}

	// Add the rest of the domains
	result = append(result, domains...)

	return result
}

// MCPServerConfig represents a parsed MCP server configuration.
// It embeds BaseMCPServerConfig for common fields and adds parser-specific fields.
type MCPServerConfig struct {
	types.BaseMCPServerConfig

	// Parser-specific fields
	Name      string   `json:"name"`       // Server name/identifier
	Registry  string   `json:"registry"`   // URI to installation location from registry
	ProxyArgs []string `json:"proxy-args"` // custom proxy arguments for container-based tools
	Allowed   []string `json:"allowed"`    // allowed tools
}

// MCPServerInfo contains the inspection results for an MCP server
type MCPServerInfo struct {
	Config    MCPServerConfig
	Connected bool
	Error     error
	Tools     []*mcp.Tool
	Resources []*mcp.Resource
	Roots     []*mcp.Root
}

// ExtractMCPConfigurations extracts MCP server configurations from workflow frontmatter
func ExtractMCPConfigurations(frontmatter map[string]any, serverFilter string) ([]MCPServerConfig, error) {
	mcpLog.Printf("Extracting MCP configurations with filter: %s", serverFilter)
	var configs []MCPServerConfig

	// Check for safe-outputs configuration first (built-in MCP)
	if safeOutputsSection, hasSafeOutputs := frontmatter["safe-outputs"]; hasSafeOutputs {
		mcpLog.Print("Found safe-outputs configuration")
		// Apply server filter if specified
		if serverFilter == "" || strings.Contains(constants.SafeOutputsMCPServerID, strings.ToLower(serverFilter)) {
			config := MCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Type:    "stdio",
					Command: "node",
					Env:     make(map[string]string),
				},
				Name: constants.SafeOutputsMCPServerID,
				// Command and args will be set up dynamically when the server is started
			}

			// Parse safe-outputs configuration to determine enabled tools
			if safeOutputsMap, ok := safeOutputsSection.(map[string]any); ok {
				for toolType := range safeOutputsMap {
					// Convert tool types to the actual MCP tool names
					switch toolType {
					case "create-issue":
						config.Allowed = append(config.Allowed, "create-issue")
					case "create-discussion":
						config.Allowed = append(config.Allowed, "create-discussion")
					case "add-comment":
						config.Allowed = append(config.Allowed, "add-comment")
					case "create-pull-request":
						config.Allowed = append(config.Allowed, "create-pull-request")
					case "create-pull-request-review-comment":
						config.Allowed = append(config.Allowed, "create-pull-request-review-comment")
					case "create-code-scanning-alert":
						config.Allowed = append(config.Allowed, "create-code-scanning-alert")
					case "add-labels":
						config.Allowed = append(config.Allowed, "add-labels")
					case "update-issue":
						config.Allowed = append(config.Allowed, "update-issue")
					case "push-to-pull-request-branch":
						config.Allowed = append(config.Allowed, "push-to-pull-request-branch")
					case "missing-tool":
						config.Allowed = append(config.Allowed, "missing-tool")

					}
				}
			}

			configs = append(configs, config)
		}
	}

	// Check for top-level safe-jobs configuration
	if safeJobsSection, hasSafeJobs := frontmatter["safe-jobs"]; hasSafeJobs {
		mcpLog.Print("Found safe-jobs configuration")
		// Apply server filter if specified
		if serverFilter == "" || strings.Contains(constants.SafeOutputsMCPServerID, strings.ToLower(serverFilter)) {
			// Find existing safe-outputs config or create new one
			var config *MCPServerConfig
			for i := range configs {
				if configs[i].Name == constants.SafeOutputsMCPServerID {
					config = &configs[i]
					break
				}
			}

			if config == nil {
				newConfig := MCPServerConfig{
					BaseMCPServerConfig: types.BaseMCPServerConfig{
						Type:    "stdio",
						Command: "node",
						Env:     make(map[string]string),
					},
					Name: constants.SafeOutputsMCPServerID,
				}
				configs = append(configs, newConfig)
				config = &configs[len(configs)-1]
			}

			// Add each safe-job as a tool
			if safeJobsMap, ok := safeJobsSection.(map[string]any); ok {
				for jobName := range safeJobsMap {
					config.Allowed = append(config.Allowed, jobName)
				}
			}
		}
	}

	// Check for safe-inputs configuration (built-in MCP)
	if safeInputsSection, hasSafeInputs := frontmatter["safe-inputs"]; hasSafeInputs {
		mcpLog.Print("Found safe-inputs configuration")
		// Apply server filter if specified
		if serverFilter == "" || strings.Contains(constants.SafeInputsMCPServerID, strings.ToLower(serverFilter)) {
			config := MCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Type:    "http",
					Command: "",
					Env:     make(map[string]string),
				},
				Name: constants.SafeInputsMCPServerID,
			}

			// Parse safe-inputs configuration to determine enabled tools
			if safeInputsMap, ok := safeInputsSection.(map[string]any); ok {
				for toolName := range safeInputsMap {
					config.Allowed = append(config.Allowed, toolName)
				}
			}

			configs = append(configs, config)
		}
	}

	// Get mcp-servers section from frontmatter
	mcpServersSection, hasMCPServers := frontmatter["mcp-servers"]
	if !hasMCPServers {
		mcpLog.Print("No mcp-servers section found, checking for built-in tools")
		// Also check tools section for built-in MCP tools (github, playwright)
		toolsSection, hasTools := frontmatter["tools"]
		if hasTools {
			if tools, ok := toolsSection.(map[string]any); ok {
				for toolName, toolValue := range tools {
					// Only handle built-in MCP tools (github, playwright, and serena)
					if toolName == "github" || toolName == "playwright" || toolName == "serena" {
						config, err := processBuiltinMCPTool(toolName, toolValue, serverFilter)
						if err != nil {
							return nil, err
						}
						if config != nil {
							mcpLog.Printf("Added built-in MCP tool: %s", toolName)
							configs = append(configs, *config)
						}
					}
				}
			}
		}
		mcpLog.Printf("Extracted %d MCP configurations total", len(configs))
		return configs, nil // No mcp-servers configured, but we might have safe-outputs and built-in tools
	}

	mcpServers, ok := mcpServersSection.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp-servers section must be a map, got %T. Example:\nmcp-servers:\n  my-server:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", mcpServersSection)
	}

	// Process built-in MCP tools from tools section
	toolsSection, hasTools := frontmatter["tools"]
	if hasTools {
		if tools, ok := toolsSection.(map[string]any); ok {
			for toolName, toolValue := range tools {
				// Only handle built-in MCP tools (github, playwright, and serena)
				if toolName == "github" || toolName == "playwright" || toolName == "serena" {
					config, err := processBuiltinMCPTool(toolName, toolValue, serverFilter)
					if err != nil {
						return nil, err
					}
					if config != nil {
						configs = append(configs, *config)
					}
				}
			}
		}
	}

	// Process custom MCP servers from mcp-servers section
	mcpLog.Printf("Processing %d custom MCP servers", len(mcpServers))
	for serverName, serverValue := range mcpServers {
		// Apply server filter if specified
		if serverFilter != "" && !strings.Contains(strings.ToLower(serverName), strings.ToLower(serverFilter)) {
			continue
		}

		// Handle custom MCP tools (those with explicit MCP configuration)
		toolConfig, ok := serverValue.(map[string]any)
		if !ok {
			continue
		}

		config, err := ParseMCPConfig(serverName, toolConfig, toolConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MCP config for %s: %w", serverName, err)
		}

		mcpLog.Printf("Parsed custom MCP server: %s (type=%s)", serverName, config.Type)
		configs = append(configs, config)
	}

	mcpLog.Printf("Extracted %d MCP configurations total", len(configs))
	return configs, nil
}

// processBuiltinMCPTool handles built-in MCP tools (github, playwright, and serena)
func processBuiltinMCPTool(toolName string, toolValue any, serverFilter string) (*MCPServerConfig, error) {
	// Apply server filter if specified
	if serverFilter != "" && !strings.Contains(strings.ToLower(toolName), strings.ToLower(serverFilter)) {
		return nil, nil
	}

	if toolName == "github" {
		// Check for custom GitHub configuration to determine mode (local vs remote)
		var useRemote bool
		var customGitHubToken string
		var readOnly bool

		if toolConfig, ok := toolValue.(map[string]any); ok {
			// Check if mode is specified (remote or local)
			if modeField, hasMode := toolConfig["mode"]; hasMode {
				if modeStr, ok := modeField.(string); ok && modeStr == "remote" {
					useRemote = true
				}
			}

			// Check for custom github-token
			if token, hasToken := toolConfig["github-token"]; hasToken {
				if tokenStr, ok := token.(string); ok {
					customGitHubToken = tokenStr
				}
			}

			// Check for read-only mode
			if readOnlyField, hasReadOnly := toolConfig["read-only"]; hasReadOnly {
				if readOnlyBool, ok := readOnlyField.(bool); ok {
					readOnly = readOnlyBool
				}
			}
		}

		var config MCPServerConfig

		if useRemote {
			// Handle GitHub MCP server in remote mode (hosted)
			config = MCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Type:    "http",
					URL:     "https://api.githubcopilot.com/mcp/",
					Headers: make(map[string]string),
					Env:     make(map[string]string),
				},
				Name: "github",
			}

			// Store custom token for later use in workflow generation
			if customGitHubToken != "" {
				config.Env["GITHUB_TOKEN"] = customGitHubToken
			}

			// Add X-MCP-Readonly header if read-only mode is enabled
			if readOnly {
				config.Headers["X-MCP-Readonly"] = "true"
			}
		} else {
			// Handle GitHub MCP server - use local/Docker by default
			config = MCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Type:    "docker", // GitHub defaults to Docker (local containerized)
					Command: "docker",
					Args: []string{
						"run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN",
						"ghcr.io/github/github-mcp-server:" + string(constants.DefaultGitHubMCPServerVersion),
					},
					Env: make(map[string]string),
				},
				Name: "github",
			}

			// Try to get GitHub token, but don't fail if it's not available
			// This allows tests to run without GitHub authentication
			if githubToken, err := GetGitHubToken(); err == nil {
				config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] = githubToken
			} else {
				// Set a placeholder that will be validated later during connection
				config.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] = "${GITHUB_TOKEN_REQUIRED}"
			}
		}

		// Check for custom GitHub configuration
		if toolConfig, ok := toolValue.(map[string]any); ok {
			// Check for read-only mode (only applicable in local/Docker mode)
			if !useRemote && readOnly {
				// When read-only is true, inline GITHUB_READ_ONLY=1 in docker args
				config.Args = append(config.Args[:5], append([]string{"-e", "GITHUB_READ_ONLY=1"}, config.Args[5:]...)...)
			}

			if allowed, hasAllowed := toolConfig["allowed"]; hasAllowed {
				if allowedSlice, ok := allowed.([]any); ok {
					for _, item := range allowedSlice {
						if str, ok := item.(string); ok {
							config.Allowed = append(config.Allowed, str)
						}
					}
				}
			}

			// Check for custom Docker image version (only applicable in local/Docker mode)
			if !useRemote {
				if version, exists := toolConfig["version"]; exists {
					if versionStr := stringutil.ParseVersionValue(version); versionStr != "" {
						dockerImage := "ghcr.io/github/github-mcp-server:" + versionStr
						// Update the Docker image in args
						for i, arg := range config.Args {
							if strings.HasPrefix(arg, "ghcr.io/github/github-mcp-server:") {
								config.Args[i] = dockerImage
								break
							}
						}
					}
				}

				// Check for custom args (only applicable in local/Docker mode)
				if argsValue, exists := toolConfig["args"]; exists {
					// Handle []any format
					if argsSlice, ok := argsValue.([]any); ok {
						for _, arg := range argsSlice {
							if argStr, ok := arg.(string); ok {
								config.Args = append(config.Args, argStr)
							}
						}
					}
					// Handle []string format
					if argsSlice, ok := argsValue.([]string); ok {
						config.Args = append(config.Args, argsSlice...)
					}
				}
			}
		}

		return &config, nil
	} else if toolName == "playwright" {
		// Handle Playwright MCP server - always use Docker by default
		config := MCPServerConfig{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Type:    "docker", // Playwright defaults to Docker (containerized)
				Command: "docker",
				Args: []string{
					"run", "-i", "--rm", "--shm-size=2gb", "--cap-add=SYS_ADMIN",
					"-e", "PLAYWRIGHT_ALLOWED_DOMAINS",
					"-v", "/tmp/gh-aw/mcp-logs:/tmp/gh-aw/mcp-logs",
					"mcr.microsoft.com/playwright:" + string(constants.DefaultPlaywrightBrowserVersion),
				},
				Env: make(map[string]string),
			},
			Name: "playwright",
		}

		// Set default allowed domains to localhost with all port variations (matches implementation)
		allowedDomains := constants.DefaultAllowedDomains

		// Check for custom Playwright configuration
		if toolConfig, ok := toolValue.(map[string]any); ok {
			// Handle allowed_domains configuration with bundle resolution
			if domainsConfig, exists := toolConfig["allowed_domains"]; exists {
				// For now, we'll use a simple conversion. In a full implementation,
				// we'd need to use the same domain bundle resolution as the compiler
				var customDomains []string
				switch domains := domainsConfig.(type) {
				case []string:
					customDomains = domains
				case []any:
					customDomains = make([]string, len(domains))
					for i, domain := range domains {
						if domainStr, ok := domain.(string); ok {
							customDomains[i] = domainStr
						}
					}
				case string:
					customDomains = []string{domains}
				}

				// Ensure localhost domains are always included
				allowedDomains = EnsureLocalhostDomains(customDomains)
			}

			// Check for custom Docker image version
			if version, exists := toolConfig["version"]; exists {
				if versionStr := stringutil.ParseVersionValue(version); versionStr != "" {
					dockerImage := "mcr.microsoft.com/playwright:" + versionStr
					// Update the Docker image in args
					for i, arg := range config.Args {
						if strings.HasPrefix(arg, "mcr.microsoft.com/playwright:") {
							config.Args[i] = dockerImage
							break
						}
					}
				}
			}

			// Check for custom args
			if argsValue, exists := toolConfig["args"]; exists {
				// Handle []any format
				if argsSlice, ok := argsValue.([]any); ok {
					for _, arg := range argsSlice {
						if argStr, ok := arg.(string); ok {
							config.Args = append(config.Args, argStr)
						}
					}
				}
				// Handle []string format
				if argsSlice, ok := argsValue.([]string); ok {
					config.Args = append(config.Args, argsSlice...)
				}
			}
		}

		config.Env["PLAYWRIGHT_ALLOWED_DOMAINS"] = strings.Join(allowedDomains, ",")
		if len(allowedDomains) == 0 {
			config.Env["PLAYWRIGHT_BLOCK_ALL_DOMAINS"] = "true"
		}

		return &config, nil
	} else if toolName == "serena" {
		// Handle Serena MCP server - uses uvx to install and run from GitHub
		config := MCPServerConfig{
			BaseMCPServerConfig: types.BaseMCPServerConfig{
				Type:    "stdio",
				Command: "uvx",
				Args: []string{
					"--from", "git+https://github.com/oraios/serena",
					"serena", "start-mcp-server",
					"--context", "codex",
					"--project", "${GITHUB_WORKSPACE}",
				},
				Env: make(map[string]string),
			},
			Name: "serena",
		}

		// Check for custom Serena configuration
		if toolConfig, ok := toolValue.(map[string]any); ok {
			// Handle custom args - these would be appended to the default args
			if argsValue, exists := toolConfig["args"]; exists {
				// Handle []any format
				if argsSlice, ok := argsValue.([]any); ok {
					for _, arg := range argsSlice {
						if argStr, ok := arg.(string); ok {
							config.Args = append(config.Args, argStr)
						}
					}
				}
				// Handle []string format
				if argsSlice, ok := argsValue.([]string); ok {
					config.Args = append(config.Args, argsSlice...)
				}
			}

			// Handle allowed tools configuration
			if allowed, hasAllowed := toolConfig["allowed"]; hasAllowed {
				if allowedSlice, ok := allowed.([]any); ok {
					for _, item := range allowedSlice {
						if str, ok := item.(string); ok {
							config.Allowed = append(config.Allowed, str)
						}
					}
				}
			}
		}

		// If no specific allowed tools are configured, allow all tools (*)
		if len(config.Allowed) == 0 {
			config.Allowed = []string{"*"}
		}

		return &config, nil
	}

	return nil, nil
}

// ParseMCPConfig parses MCP configuration from various formats (map or JSON string)
func ParseMCPConfig(toolName string, mcpSection any, toolConfig map[string]any) (MCPServerConfig, error) {
	mcpLog.Printf("Parsing MCP configuration for tool: %s", toolName)
	config := MCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Env:     make(map[string]string),
			Headers: make(map[string]string),
		},
		Name: toolName,
	}

	// Parse allowed tools
	if allowed, hasAllowed := toolConfig["allowed"]; hasAllowed {
		if allowedSlice, ok := allowed.([]any); ok {
			for _, item := range allowedSlice {
				if str, ok := item.(string); ok {
					config.Allowed = append(config.Allowed, str)
				}
			}
		}
	}

	var mcpConfig map[string]any

	// Handle different MCP section formats
	switch v := mcpSection.(type) {
	case map[string]any:
		mcpConfig = v
	case string:
		// Parse JSON string
		if err := json.Unmarshal([]byte(v), &mcpConfig); err != nil {
			return config, fmt.Errorf("invalid JSON in mcp configuration: %w", err)
		}
	default:
		return config, fmt.Errorf("mcp configuration must be a map or JSON string, got %T. Example:\nmcp-servers:\n  %s:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", v, toolName)
	}

	// Extract type (explicit or inferred)
	if typeVal, hasType := mcpConfig["type"]; hasType {
		if typeStr, ok := typeVal.(string); ok {
			// Normalize "local" to "stdio"
			if typeStr == "local" {
				config.Type = "stdio"
			} else {
				config.Type = typeStr
			}
		} else {
			return config, fmt.Errorf("type field must be a string, got %T. Valid types are: stdio, http. Example:\nmcp-servers:\n  %s:\n    type: stdio\n    command: \"npx @my/tool\"", typeVal, toolName)
		}
	} else {
		// Infer type from presence of fields
		if _, hasURL := mcpConfig["url"]; hasURL {
			config.Type = "http"
			mcpLog.Printf("Inferred MCP type 'http' for tool %s based on url field", toolName)
		} else if _, hasCommand := mcpConfig["command"]; hasCommand {
			config.Type = "stdio"
			mcpLog.Printf("Inferred MCP type 'stdio' for tool %s based on command field", toolName)
		} else if _, hasContainer := mcpConfig["container"]; hasContainer {
			config.Type = "stdio"
			mcpLog.Printf("Inferred MCP type 'stdio' for tool %s based on container field", toolName)
		} else {
			return config, fmt.Errorf("unable to determine MCP type for tool '%s': missing type, url, command, or container. Must specify one of: 'type' (stdio/http), 'url' (for HTTP MCP), 'command' (for command-based), or 'container' (for Docker-based). Example:\nmcp-servers:\n  %s:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", toolName, toolName)
		}
	}

	// Extract registry field (available for both stdio and http)
	if registry, hasRegistry := mcpConfig["registry"]; hasRegistry {
		if registryStr, ok := registry.(string); ok {
			config.Registry = registryStr
		} else {
			return config, fmt.Errorf("registry field must be a string, got %T. Example:\nmcp-servers:\n  %s:\n    registry: \"https://registry.npmjs.org/@my/tool\"\n    command: \"npx @my/tool\"", registry, toolName)
		}
	}

	// Extract configuration based on type
	mcpLog.Printf("Extracting %s configuration for tool: %s", config.Type, toolName)
	switch config.Type {
	case "stdio":
		// Handle container field (simplified Docker run)
		if container, hasContainer := mcpConfig["container"]; hasContainer {
			if containerStr, ok := container.(string); ok {
				mcpLog.Printf("Tool %s uses container: %s", toolName, containerStr)
				config.Container = containerStr
				config.Command = "docker"
				config.Args = []string{"run", "--rm", "-i"}

				// Add environment variables
				if env, hasEnv := mcpConfig["env"]; hasEnv {
					if envMap, ok := env.(map[string]any); ok {
						// Sort environment variable keys to ensure deterministic arg order
						var envKeys []string
						for key := range envMap {
							envKeys = append(envKeys, key)
						}
						sort.Strings(envKeys)

						for _, key := range envKeys {
							if valueStr, ok := envMap[key].(string); ok {
								config.Args = append(config.Args, "-e", key)
								config.Env[key] = valueStr
							}
						}
					}
				}

				// Add volume mounts if configured (sorted for deterministic output)
				if mounts, hasMounts := mcpConfig["mounts"]; hasMounts {
					if mountsSlice, ok := mounts.([]any); ok {
						// Collect mounts first
						var mountStrings []string
						for _, mount := range mountsSlice {
							if mountStr, ok := mount.(string); ok {
								mountStrings = append(mountStrings, mountStr)
								config.Mounts = append(config.Mounts, mountStr)
							}
						}
						// Sort for deterministic output
						sort.Strings(mountStrings)
						for _, mountStr := range mountStrings {
							config.Args = append(config.Args, "-v", mountStr)
						}
					}
				}

				// Add entrypoint override if specified
				if entrypoint, hasEntrypoint := mcpConfig["entrypoint"]; hasEntrypoint {
					if entrypointStr, ok := entrypoint.(string); ok {
						config.Entrypoint = entrypointStr
						config.Args = append(config.Args, "--entrypoint", entrypointStr)
					}
				}

				config.Args = append(config.Args, containerStr)

				// Add entrypoint args after the container image
				if entrypointArgs, hasEntrypointArgs := mcpConfig["entrypointArgs"]; hasEntrypointArgs {
					if entrypointArgsSlice, ok := entrypointArgs.([]any); ok {
						for _, arg := range entrypointArgsSlice {
							if argStr, ok := arg.(string); ok {
								config.Args = append(config.Args, argStr)
							}
						}
					}
				}
			}
		} else {
			// Handle command and args
			if command, hasCommand := mcpConfig["command"]; hasCommand {
				if commandStr, ok := command.(string); ok {
					config.Command = commandStr
				} else {
					return config, fmt.Errorf("command field must be a string, got %T. Example:\nmcp-servers:\n  %s:\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", command, toolName)
				}
			} else {
				return config, fmt.Errorf(
					"stdio MCP tool '%s' must specify either 'command' or 'container' field. Cannot specify both. "+
						"Example with command:\n"+
						"mcp-servers:\n"+
						"  %s:\n"+
						"    command: \"npx @my/tool\"\n"+
						"    args: [\"--port\", \"3000\"]\n\n"+
						"Example with container:\n"+
						"mcp-servers:\n"+
						"  %s:\n"+
						"    container: \"myorg/my-tool:latest\"\n"+
						"    env:\n"+
						"      API_KEY: \"${{ secrets.API_KEY }}\"",
					toolName, toolName, toolName,
				)
			}

			if args, hasArgs := mcpConfig["args"]; hasArgs {
				if argsSlice, ok := args.([]any); ok {
					for _, arg := range argsSlice {
						if argStr, ok := arg.(string); ok {
							config.Args = append(config.Args, argStr)
						}
					}
				}
			}
		}

		// Extract environment variables for stdio
		if env, hasEnv := mcpConfig["env"]; hasEnv {
			if envMap, ok := env.(map[string]any); ok {
				for key, value := range envMap {
					if valueStr, ok := value.(string); ok {
						config.Env[key] = valueStr
					}
				}
			}
		}

		// Extract network configuration for stdio (container-based tools)
		if network, hasNetwork := mcpConfig["network"]; hasNetwork {
			if networkMap, ok := network.(map[string]any); ok {
				// Extract proxy arguments from network config
				if proxyArgs, hasProxyArgs := networkMap["proxy-args"]; hasProxyArgs {
					if proxyArgsSlice, ok := proxyArgs.([]any); ok {
						for _, arg := range proxyArgsSlice {
							if argStr, ok := arg.(string); ok {
								config.ProxyArgs = append(config.ProxyArgs, argStr)
							}
						}
					}
				}
			}
		}

	case "http":
		if url, hasURL := mcpConfig["url"]; hasURL {
			if urlStr, ok := url.(string); ok {
				mcpLog.Printf("Tool %s uses HTTP transport with URL: %s", toolName, urlStr)
				config.URL = urlStr
			} else {
				return config, fmt.Errorf(
					"url field must be a string, got %T. Example:\n"+
						"mcp-servers:\n"+
						"  %s:\n"+
						"    type: http\n"+
						"    url: \"https://api.example.com/mcp\"\n"+
						"    headers:\n"+
						"      Authorization: \"Bearer ${{ secrets.API_KEY }}\"",
					url, toolName)
			}
		} else {
			return config, fmt.Errorf(
				"http MCP tool '%s' missing required 'url' field. HTTP MCP servers must specify a URL endpoint. "+
					"Example:\n"+
					"mcp-servers:\n"+
					"  %s:\n"+
					"    type: http\n"+
					"    url: \"https://api.example.com/mcp\"\n"+
					"    headers:\n"+
					"      Authorization: \"Bearer ${{ secrets.API_KEY }}\"",
				toolName, toolName,
			)
		}

		// Extract headers
		if headers, hasHeaders := mcpConfig["headers"]; hasHeaders {
			if headersMap, ok := headers.(map[string]any); ok {
				for key, value := range headersMap {
					if valueStr, ok := value.(string); ok {
						config.Headers[key] = valueStr
					}
				}
			}
		}

	default:
		return config, fmt.Errorf("unsupported MCP type '%s' for tool '%s'. Valid types are: stdio, http. Example:\nmcp-servers:\n  %s:\n    type: stdio\n    command: \"npx @my/tool\"\n    args: [\"--port\", \"3000\"]", config.Type, toolName, toolName)
	}

	return config, nil
}
