package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/types"
)

var toolsTypesLog = logger.New("workflow:tools_types")

// ToolsConfig represents the unified configuration for all tools in a workflow.
// This type provides a structured alternative to the pervasive map[string]any pattern.
// It includes strongly-typed fields for built-in tools and a flexible Custom map for
// MCP server configurations.
//
// # Migration Pattern
//
// This unified type helps eliminate unnecessary type assertions and runtime validation
// by replacing map[string]any with strongly-typed configuration structs.
//
// # Usage Examples
//
// Creating a ToolsConfig from a map[string]any:
//
//	toolsMap := map[string]any{
//	    "github": map[string]any{"allowed": []any{"issue_read"}},
//	    "bash":   []any{"echo", "ls"},
//	}
//	config, err := ParseToolsConfig(toolsMap)
//	if err != nil {
//	    // handle error
//	}
//
// Converting back to map[string]any for legacy code:
//
//	toolsMap := config.ToMap()
//
// # Backward Compatibility
//
// For functions that currently accept map[string]any, create wrapper functions
// that handle conversion:
//
//	// New signature using ToolsConfig
//	func processTools(config *ToolsConfig) error {
//	    if config.GitHub != nil {
//	        // Access strongly-typed GitHub config
//	    }
//	    return nil
//	}
//
//	// Backward compatibility wrapper
//	func processToolsFromMap(tools map[string]any) error {
//	    config, err := ParseToolsConfig(tools)
//	    if err != nil {
//	        return err
//	    }
//	    return processTools(config)
//	}
//
// # Design Notes
//
//   - Built-in tool fields use pointers to distinguish between "not configured" (nil)
//     and "configured with defaults" (non-nil but empty struct)
//   - The Custom map stores MCP server configurations that aren't built-in tools
//   - The raw map is preserved for perfect round-trip conversion when needed
//   - Type alias Tools = ToolsConfig provides backward compatibility for existing code
type ToolsConfig struct {
	// Built-in tools - using pointers to distinguish between "not set" and "set to nil/empty"
	GitHub           *GitHubToolConfig           `yaml:"github,omitempty"`
	Bash             *BashToolConfig             `yaml:"bash,omitempty"`
	WebFetch         *WebFetchToolConfig         `yaml:"web-fetch,omitempty"`
	WebSearch        *WebSearchToolConfig        `yaml:"web-search,omitempty"`
	Edit             *EditToolConfig             `yaml:"edit,omitempty"`
	Playwright       *PlaywrightToolConfig       `yaml:"playwright,omitempty"`
	Serena           *SerenaToolConfig           `yaml:"serena,omitempty"`
	AgenticWorkflows *AgenticWorkflowsToolConfig `yaml:"agentic-workflows,omitempty"`
	CacheMemory      *CacheMemoryToolConfig      `yaml:"cache-memory,omitempty"`
	RepoMemory       *RepoMemoryToolConfig       `yaml:"repo-memory,omitempty"`
	Timeout          *int                        `yaml:"timeout,omitempty"`
	StartupTimeout   *int                        `yaml:"startup-timeout,omitempty"`

	// Custom MCP tools (anything not in the above list)
	Custom map[string]MCPServerConfig `yaml:",inline"`

	// Raw map for backwards compatibility
	raw map[string]any
}

// Tools is a type alias for ToolsConfig for backward compatibility.
// New code should prefer using ToolsConfig to be explicit about the unified configuration pattern.
type Tools = ToolsConfig

// ParseToolsConfig creates a ToolsConfig from a map[string]any.
// This function provides backward compatibility for code that uses map[string]any.
// It parses all known tool types into their strongly-typed equivalents and stores
// unknown tools in the Custom map.
func ParseToolsConfig(toolsMap map[string]any) (*ToolsConfig, error) {
	toolsTypesLog.Printf("Parsing tools configuration: tool_count=%d", len(toolsMap))
	config := NewTools(toolsMap)
	toolNames := config.GetToolNames()
	toolsTypesLog.Printf("Parsed tools configuration: result_count=%d, tools=%v", len(toolNames), toolNames)
	return config, nil
}

// mcpServerConfigToMap converts an MCPServerConfig to map[string]any for backward compatibility
func mcpServerConfigToMap(config MCPServerConfig) map[string]any {
	result := make(map[string]any)

	// Add common fields if they're set
	if config.Command != "" {
		result["command"] = config.Command
	}
	if len(config.Args) > 0 {
		result["args"] = config.Args
	}
	if len(config.Env) > 0 {
		result["env"] = config.Env
	}
	if config.Mode != "" {
		result["mode"] = config.Mode
	}
	if config.Type != "" {
		result["type"] = config.Type
	}
	if config.Version != "" {
		result["version"] = config.Version
	}
	if len(config.Toolsets) > 0 {
		result["toolsets"] = config.Toolsets
	}

	// Add HTTP-specific fields
	if config.URL != "" {
		result["url"] = config.URL
	}
	if len(config.Headers) > 0 {
		result["headers"] = config.Headers
	}

	// Add container-specific fields
	if config.Container != "" {
		result["container"] = config.Container
	}
	if config.Entrypoint != "" {
		result["entrypoint"] = config.Entrypoint
	}
	if len(config.EntrypointArgs) > 0 {
		result["entrypointArgs"] = config.EntrypointArgs
	}
	if len(config.Mounts) > 0 {
		result["mounts"] = config.Mounts
	}

	// Add custom fields (these override standard fields if there are conflicts)
	for key, value := range config.CustomFields {
		result[key] = value
	}

	return result
}

// ToMap converts the ToolsConfig back to a map[string]any for backward compatibility.
// This is useful when interfacing with legacy code that expects map[string]any.
func (t *ToolsConfig) ToMap() map[string]any {
	if t == nil {
		toolsTypesLog.Print("Converting nil ToolsConfig to empty map")
		return make(map[string]any)
	}

	// Return the raw map if it exists
	if t.raw != nil {
		toolsTypesLog.Printf("Returning cached raw map with %d entries", len(t.raw))
		return t.raw
	}

	// Otherwise construct a new map from the fields
	toolsTypesLog.Print("Constructing map from ToolsConfig fields")
	result := make(map[string]any)

	if t.GitHub != nil {
		result["github"] = t.GitHub
	}
	if t.Bash != nil {
		result["bash"] = t.Bash.AllowedCommands
	}
	if t.WebFetch != nil {
		result["web-fetch"] = t.WebFetch
	}
	if t.WebSearch != nil {
		result["web-search"] = t.WebSearch
	}
	if t.Edit != nil {
		result["edit"] = t.Edit
	}
	if t.Playwright != nil {
		result["playwright"] = t.Playwright
	}
	if t.Serena != nil {
		// Convert back based on whether it was short syntax or object
		if len(t.Serena.ShortSyntax) > 0 {
			result["serena"] = t.Serena.ShortSyntax
		} else {
			result["serena"] = t.Serena
		}
	}
	if t.AgenticWorkflows != nil {
		result["agentic-workflows"] = t.AgenticWorkflows.Enabled
	}
	if t.CacheMemory != nil {
		result["cache-memory"] = t.CacheMemory.Raw
	}
	if t.RepoMemory != nil {
		result["repo-memory"] = t.RepoMemory.Raw
	}
	if t.Timeout != nil {
		result["timeout"] = *t.Timeout
	}
	if t.StartupTimeout != nil {
		result["startup-timeout"] = *t.StartupTimeout
	}

	// Add custom tools - convert MCPServerConfig to map[string]any
	for name, config := range t.Custom {
		result[name] = mcpServerConfigToMap(config)
	}

	toolsTypesLog.Printf("Constructed map with %d entries from ToolsConfig", len(result))
	return result
}

// GitHubToolName represents a GitHub tool name (e.g., "issue_read", "create_issue")
type GitHubToolName string

// GitHubAllowedTools is a slice of GitHub tool names
type GitHubAllowedTools []GitHubToolName

// ToStringSlice converts GitHubAllowedTools to []string
func (g GitHubAllowedTools) ToStringSlice() []string {
	result := make([]string, len(g))
	for i, tool := range g {
		result[i] = string(tool)
	}
	return result
}

// GitHubToolset represents a GitHub toolset name (e.g., "default", "repos", "issues")
type GitHubToolset string

// GitHubToolsets is a slice of GitHub toolset names
type GitHubToolsets []GitHubToolset

// ToStringSlice converts GitHubToolsets to []string
func (g GitHubToolsets) ToStringSlice() []string {
	result := make([]string, len(g))
	for i, toolset := range g {
		result[i] = string(toolset)
	}
	return result
}

// GitHubToolConfig represents the configuration for the GitHub tool
// Can be nil (enabled with defaults), string, or an object with specific settings
type GitHubToolConfig struct {
	Allowed     GitHubAllowedTools `yaml:"allowed,omitempty"`
	Mode        string             `yaml:"mode,omitempty"`
	Version     string             `yaml:"version,omitempty"`
	Args        []string           `yaml:"args,omitempty"`
	ReadOnly    bool               `yaml:"read-only,omitempty"`
	GitHubToken string             `yaml:"github-token,omitempty"`
	Toolset     GitHubToolsets     `yaml:"toolsets,omitempty"`
	Lockdown    bool               `yaml:"lockdown,omitempty"`
	App         *GitHubAppConfig   `yaml:"app,omitempty"` // GitHub App configuration for token minting
}

// PlaywrightDomain represents a domain name allowed for Playwright browser automation
type PlaywrightDomain string

// PlaywrightAllowedDomains is a slice of allowed domain names for Playwright
type PlaywrightAllowedDomains []PlaywrightDomain

// ToStringSlice converts PlaywrightAllowedDomains to []string
func (p PlaywrightAllowedDomains) ToStringSlice() []string {
	result := make([]string, len(p))
	for i, domain := range p {
		result[i] = string(domain)
	}
	return result
}

// PlaywrightToolConfig represents the configuration for the Playwright tool
type PlaywrightToolConfig struct {
	Version        string                   `yaml:"version,omitempty"`
	AllowedDomains PlaywrightAllowedDomains `yaml:"allowed_domains,omitempty"`
	Args           []string                 `yaml:"args,omitempty"`
}

// SerenaToolConfig represents the configuration for the Serena MCP tool
type SerenaToolConfig struct {
	Version   string                       `yaml:"version,omitempty"`
	Args      []string                     `yaml:"args,omitempty"`
	Mode      string                       `yaml:"mode,omitempty"` // "docker" (default) or "local" (uses uvx)
	Languages map[string]*SerenaLangConfig `yaml:"languages,omitempty"`
	// ShortSyntax stores the array of language names when using short syntax (e.g., ["go", "typescript"])
	ShortSyntax []string `yaml:"-"`
}

// SerenaLangConfig represents per-language configuration for Serena
type SerenaLangConfig struct {
	Version      string `yaml:"version,omitempty"`
	GoModFile    string `yaml:"go-mod-file,omitempty"`   // Path to go.mod file (Go only)
	GoplsVersion string `yaml:"gopls-version,omitempty"` // Version of gopls to install (Go only)
}

// BashToolConfig represents the configuration for the Bash tool
// Can be nil (all commands allowed) or an array of allowed commands
type BashToolConfig struct {
	AllowedCommands []string `yaml:"-"` // List of allowed bash commands
}

// WebFetchToolConfig represents the configuration for the web-fetch tool
type WebFetchToolConfig struct {
	// Currently an empty object or nil
}

// WebSearchToolConfig represents the configuration for the web-search tool
type WebSearchToolConfig struct {
	// Currently an empty object or nil
}

// EditToolConfig represents the configuration for the edit tool
type EditToolConfig struct {
	// Currently an empty object or nil
}

// AgenticWorkflowsToolConfig represents the configuration for the agentic-workflows tool
type AgenticWorkflowsToolConfig struct {
	// Can be boolean or nil
	Enabled bool `yaml:"-"`
}

// CacheMemoryToolConfig represents the configuration for cache-memory
// This is handled separately by the existing CacheMemoryConfig in cache.go
type CacheMemoryToolConfig struct {
	// Can be boolean, object, or array - handled by cache.go
	Raw any `yaml:"-"`
}

// MCPServerConfig represents the configuration for a custom MCP server.
// It embeds BaseMCPServerConfig for common fields and adds workflow-specific fields.
// This provides partial type safety for common MCP configuration fields
// while maintaining flexibility for truly dynamic configurations.
type MCPServerConfig struct {
	types.BaseMCPServerConfig

	// Workflow-specific fields
	Mode     string   `yaml:"mode,omitempty"`     // MCP server mode (stdio, http, remote, local)
	Toolsets []string `yaml:"toolsets,omitempty"` // Toolsets to enable

	// For truly dynamic configuration (server-specific fields not covered above)
	CustomFields map[string]any `yaml:",inline"`
}

// MCPGatewayRuntimeConfig represents the configuration for the MCP gateway runtime execution
// The gateway routes MCP server calls through a unified HTTP endpoint
// Per MCP Gateway Specification v1.0.0: All stdio-based MCP servers MUST be containerized.
// Direct command execution is not supported.
type MCPGatewayRuntimeConfig struct {
	Container      string            `yaml:"container,omitempty"`      // Container image for the gateway (required)
	Version        string            `yaml:"version,omitempty"`        // Optional version/tag for the container
	Entrypoint     string            `yaml:"entrypoint,omitempty"`     // Optional entrypoint override for the container
	Args           []string          `yaml:"args,omitempty"`           // Arguments for docker run
	EntrypointArgs []string          `yaml:"entrypointArgs,omitempty"` // Arguments passed to container entrypoint
	Env            map[string]string `yaml:"env,omitempty"`            // Environment variables for the gateway
	Port           int               `yaml:"port,omitempty"`           // Port for the gateway HTTP server (default: 8080)
	APIKey         string            `yaml:"api-key,omitempty"`        // API key for gateway authentication
	Domain         string            `yaml:"domain,omitempty"`         // Domain for gateway URL (localhost or host.docker.internal)
	Mounts         []string          `yaml:"mounts,omitempty"`         // Volume mounts for the gateway container (format: "source:dest:mode")
	PayloadDir     string            `yaml:"payload-dir,omitempty"`    // Directory path for storing large payload JSON files (must be absolute path)
}

// HasTool checks if a tool is present in the configuration
func (t *Tools) HasTool(name string) bool {
	if t == nil {
		return false
	}

	toolsTypesLog.Printf("Checking if tool exists: name=%s", name)

	switch name {
	case "github":
		return t.GitHub != nil
	case "bash":
		return t.Bash != nil
	case "web-fetch":
		return t.WebFetch != nil
	case "web-search":
		return t.WebSearch != nil
	case "edit":
		return t.Edit != nil
	case "playwright":
		return t.Playwright != nil
	case "serena":
		return t.Serena != nil
	case "agentic-workflows":
		return t.AgenticWorkflows != nil
	case "cache-memory":
		return t.CacheMemory != nil
	case "repo-memory":
		return t.RepoMemory != nil
	case "timeout":
		return t.Timeout != nil
	case "startup-timeout":
		return t.StartupTimeout != nil
	default:
		_, exists := t.Custom[name]
		return exists
	}
}

// GetToolNames returns a list of all tool names configured
func (t *Tools) GetToolNames() []string {
	if t == nil {
		return []string{}
	}

	toolsTypesLog.Print("Collecting configured tool names")
	names := []string{}

	if t.GitHub != nil {
		names = append(names, "github")
	}
	if t.Bash != nil {
		names = append(names, "bash")
	}
	if t.WebFetch != nil {
		names = append(names, "web-fetch")
	}
	if t.WebSearch != nil {
		names = append(names, "web-search")
	}
	if t.Edit != nil {
		names = append(names, "edit")
	}
	if t.Playwright != nil {
		names = append(names, "playwright")
	}
	if t.Serena != nil {
		names = append(names, "serena")
	}
	if t.AgenticWorkflows != nil {
		names = append(names, "agentic-workflows")
	}
	if t.CacheMemory != nil {
		names = append(names, "cache-memory")
	}
	if t.RepoMemory != nil {
		names = append(names, "repo-memory")
	}
	if t.Timeout != nil {
		names = append(names, "timeout")
	}
	if t.StartupTimeout != nil {
		names = append(names, "startup-timeout")
	}

	// Add custom tools
	for name := range t.Custom {
		names = append(names, name)
	}

	toolsTypesLog.Printf("Found %d configured tools: %v", len(names), names)
	return names
}
