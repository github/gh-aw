package workflow

import "strings"

// MCPRendererOptions contains configuration options for the unified MCP renderer
type MCPRendererOptions struct {
	// IncludeCopilotFields indicates if the engine requires "type" and "tools" fields (true for copilot engine)
	IncludeCopilotFields bool
	// InlineArgs indicates if args should be rendered inline (true for copilot) or multi-line (false for claude/custom)
	InlineArgs bool
	// Format specifies the output format ("json" for JSON-like, "toml" for TOML-like)
	Format string
	// IsLast indicates if this is the last server in the configuration (affects trailing comma)
	IsLast bool
	// ActionMode indicates the action mode for workflow compilation (dev, release, script)
	ActionMode ActionMode
	// WriteSinkGuardPolicies contains the write-sink guard policies to apply to non-GitHub MCP servers.
	// These are derived from the GitHub guard-policy configuration and applied as a default write-sink
	// to ensure that as guard policies are rolled out, only GitHub inputs are filtered while outputs
	// to non-GitHub servers are not restricted. Nil when no GitHub guard policies are configured.
	WriteSinkGuardPolicies map[string]any
	// ContainerPinMappings maps source container image references to their SHA-pinned
	// replacements from aw.json container_pins. When set, built-in MCP server container
	// references are redirected to the mapped private registry mirror (digest stripped for
	// MCP Gateway compatibility). Nil when no container_pins are configured.
	ContainerPinMappings map[string]string
}

// MCPConfigRendererUnified provides unified rendering methods for MCP configurations
// across different engines (Claude, Copilot, Codex, Custom)
type MCPConfigRendererUnified struct {
	options MCPRendererOptions
}

// RenderCustomMCPToolConfigHandler is a function type for rendering custom MCP tool configurations
type RenderCustomMCPToolConfigHandler func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error

// MCPToolRenderers holds engine-specific rendering functions for each MCP tool type
type MCPToolRenderers struct {
	RenderGitHub           func(yaml *strings.Builder, githubTool map[string]any, isLast bool, workflowData *WorkflowData)
	RenderCacheMemory      func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData)
	RenderAgenticWorkflows func(yaml *strings.Builder, isLast bool)
	RenderSafeOutputs      func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData)
	RenderMCPScripts       func(yaml *strings.Builder, mcpScripts *MCPScriptsConfig, isLast bool)
	RenderEnclave          func(yaml *strings.Builder, workflowData *WorkflowData, isLast bool)
	RenderCustomMCPConfig  RenderCustomMCPToolConfigHandler
}

// JSONMCPConfigOptions defines configuration for JSON-based MCP config rendering
type JSONMCPConfigOptions struct {
	// ConfigPath is the file path for the MCP config (e.g., "${RUNNER_TEMP}/gh-aw/mcp-config/mcp-servers.json")
	ConfigPath string
	// Renderers contains engine-specific rendering functions for each tool
	Renderers MCPToolRenderers
	// FilterTool is an optional function to filter out tools before processing
	// Returns true if the tool should be included, false to skip it
	FilterTool func(toolName string) bool
	// PostEOFCommands is an optional function to add commands after the EOF (e.g., debug output)
	PostEOFCommands func(yaml *strings.Builder)
	// GatewayConfig is an optional gateway configuration to include in the MCP config
	// When set, adds a "gateway" section with port and apiKey for awmg to use
	GatewayConfig *MCPGatewayRuntimeConfig
}

// GitHubMCPDockerOptions defines configuration for GitHub MCP Docker rendering
type GitHubMCPDockerOptions struct {
	// ReadOnly enables read-only mode for GitHub API operations
	ReadOnly bool
	// Lockdown enables lockdown mode for GitHub MCP server (limits content from public repos)
	Lockdown bool
	// LockdownFromStep indicates if lockdown value should be read from step output
	LockdownFromStep bool
	// GuardPoliciesFromStep indicates if guard policy values should be read from step outputs
	// (GITHUB_MCP_GUARD_MIN_INTEGRITY and GITHUB_MCP_GUARD_REPOS env vars)
	GuardPoliciesFromStep bool
	// Toolsets specifies the GitHub toolsets to enable
	Toolsets string
	// Features is a comma-separated list of GitHub MCP feature flags to enable (e.g. "fields_param").
	// Emitted as GITHUB_FEATURES env var for the Docker container.
	Features string
	// DockerImageVersion specifies the GitHub MCP server Docker image version
	DockerImageVersion string
	// CustomArgs are additional arguments to append to the Docker command
	CustomArgs []string
	// IncludeTypeField indicates whether to include the "type": "stdio" field (Copilot needs it, Claude doesn't)
	IncludeTypeField bool
	// AllowedTools specifies the list of allowed tools (Copilot uses this, Claude doesn't)
	AllowedTools []string
	// EffectiveToken is the GitHub token to use (Claude uses this, Copilot uses env passthrough)
	EffectiveToken string
	// GuardPolicies specifies access control policies for the MCP gateway (e.g., allow-only repos/integrity)
	GuardPolicies map[string]any
	// EmitRequiredFalse writes `"required": false` for delegation-only backends whose startup
	// failures should degrade to warnings rather than fail the entire primary agent run.
	EmitRequiredFalse bool
	// ContainerPinMappings maps source container image references to their SHA-pinned replacements.
	// When set, the GitHub MCP server container reference is redirected to the mapped private
	// registry mirror (digest stripped for MCP Gateway compatibility). Nil → no redirect.
	ContainerPinMappings map[string]string
}

// GitHubMCPRemoteOptions defines configuration for GitHub MCP remote mode rendering
type GitHubMCPRemoteOptions struct {
	// ReadOnly enables read-only mode for GitHub API operations
	ReadOnly bool
	// Lockdown enables lockdown mode for GitHub MCP server (limits content from public repos)
	Lockdown bool
	// LockdownFromStep indicates if lockdown value should be read from step output
	LockdownFromStep bool
	// GuardPoliciesFromStep indicates if guard policy values should be read from step outputs
	// (GITHUB_MCP_GUARD_MIN_INTEGRITY and GITHUB_MCP_GUARD_REPOS env vars)
	GuardPoliciesFromStep bool
	// Toolsets specifies the GitHub toolsets to enable
	Toolsets string
	// Features is a comma-separated list of GitHub MCP feature flags to enable (e.g. "fields_param").
	// Emitted as X-MCP-Features header for the hosted endpoint.
	Features string
	// AuthorizationValue is the value for the Authorization header
	// For Claude: "Bearer {effectiveToken}"
	// For Copilot: "Bearer \\${GITHUB_PERSONAL_ACCESS_TOKEN}"
	AuthorizationValue string
	// IncludeToolsField indicates whether to include the "tools" field (Copilot needs it, Claude doesn't)
	IncludeToolsField bool
	// AllowedTools specifies the list of allowed tools (Copilot uses this, Claude doesn't)
	AllowedTools []string
	// IncludeEnvSection indicates whether to include the env section (Copilot needs it, Claude doesn't)
	IncludeEnvSection bool
	// GuardPolicies specifies access control policies for the MCP gateway (e.g., allow-only repos/integrity)
	GuardPolicies map[string]any
	// EmitRequiredFalse writes `"required": false` for delegation-only backends whose startup
	// failures should degrade to warnings rather than fail the entire primary agent run.
	EmitRequiredFalse bool
}
