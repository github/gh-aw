package workflow

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

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
	AgenticWorkflows *AgenticWorkflowsToolConfig `yaml:"agentic-workflows,omitempty"`
	CacheMemory      *CacheMemoryToolConfig      `yaml:"cache-memory,omitempty"`
	DriveMemory      *DriveMemoryToolConfig      `yaml:"drive-memory,omitempty"`
	CommentMemory    *CommentMemoryToolConfig    `yaml:"comment-memory,omitempty"`
	RepoMemory       *RepoMemoryToolConfig       `yaml:"repo-memory,omitempty"`
	Timeout          *TemplatableInt32           `yaml:"timeout,omitempty"`
	StartupTimeout   *TemplatableInt32           `yaml:"startup-timeout,omitempty"`

	// Custom MCP tools (anything not in the above list)
	Custom map[string]MCPServerConfig `yaml:",inline"`

	// CLIProxy enables mounting MCP servers as standalone CLI tools on PATH.
	// When true, each user-facing MCP server gets a bash wrapper script placed in
	// a read-only directory added to PATH. The servers remain in the MCP gateway
	// config, but are filtered out of the agent's final MCP config so the agent
	// uses the CLI instead of the MCP protocol.
	// Default is false.
	CLIProxy bool `yaml:"cli-proxy,omitempty"`

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
	if config.GitHub != nil && config.GitHub.reposParseErr != nil {
		return nil, config.GitHub.reposParseErr
	}
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
	if config.Auth != nil {
		result["auth"] = config.Auth
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

	// Add guard policies if set
	if len(config.GuardPolicies) > 0 {
		result["guard-policies"] = config.GuardPolicies
	}

	// Add custom fields (these override standard fields if there are conflicts)
	maps.Copy(result, config.CustomFields)

	return result
}

// ToMap converts the ToolsConfig back to a map[string]any for backward compatibility.
// This is useful when interfacing with legacy code that expects map[string]any.
func (t *ToolsConfig) ToMap() map[string]any { //nolint:largefunc // Existing conversion preserves backwards-compatible map shape.
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
	if t.AgenticWorkflows != nil {
		result["agentic-workflows"] = t.AgenticWorkflows.Enabled
	}
	if t.CacheMemory != nil {
		result["cache-memory"] = t.CacheMemory.Raw
	}
	if t.DriveMemory != nil {
		result["drive-memory"] = t.DriveMemory.Raw
	}
	if t.CommentMemory != nil {
		result["comment-memory"] = t.CommentMemory.Raw
	}
	if t.RepoMemory != nil {
		result["repo-memory"] = t.RepoMemory.Raw
	}
	if t.Timeout != nil {
		result["timeout"] = t.Timeout.ToValue()
	}
	if t.StartupTimeout != nil {
		result["startup-timeout"] = t.StartupTimeout.ToValue()
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

// GitHubMCPMode represents the MCP transport/deployment mode for the GitHub tool.
type GitHubMCPMode string

const (
	// GitHubMCPModeLocal runs the GitHub MCP server as a Docker container on the runner.
	GitHubMCPModeLocal GitHubMCPMode = "local"
	// GitHubMCPModeRemote connects to the hosted GitHub MCP service.
	GitHubMCPModeRemote GitHubMCPMode = "remote"
	// GitHubMCPModeGHProxy routes GitHub operations through the gh CLI proxy.
	GitHubMCPModeGHProxy GitHubMCPMode = "gh-proxy"
	// GitHubMCPModeCLI is a legacy alias for GitHubMCPModeGHProxy.
	GitHubMCPModeCLI GitHubMCPMode = "cli"
)

// GitHubIntegrityLevel represents the minimum integrity level required for repository access
type GitHubIntegrityLevel string

const (
	// GitHubIntegrityNone allows access with no integrity requirements
	GitHubIntegrityNone GitHubIntegrityLevel = "none"
	// GitHubIntegrityUnapproved requires unapproved-level integrity
	GitHubIntegrityUnapproved GitHubIntegrityLevel = "unapproved"
	// GitHubIntegrityApproved requires approved-level integrity
	GitHubIntegrityApproved GitHubIntegrityLevel = "approved"
	// GitHubIntegrityMerged requires merged-level integrity
	GitHubIntegrityMerged GitHubIntegrityLevel = "merged"
)

// GitHubReposScope represents the repository scope for guard policy enforcement
// Can be one of: "all", "public", or an array of repository patterns
type GitHubReposScope []string

func parseGitHubReposScope(value any) (GitHubReposScope, error) {
	switch repos := value.(type) {
	case string:
		return GitHubReposScope{repos}, nil
	case []string:
		return GitHubReposScope(repos), nil
	case []any:
		result := make(GitHubReposScope, 0, len(repos))
		for _, repo := range repos {
			value, ok := repo.(string)
			if !ok {
				return nil, fmt.Errorf("repository scope entries must be strings, got %T", repo)
			}
			result = append(result, value)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("repository scope must be a string or array of strings, got %T", value)
	}
}

// UnmarshalYAML normalizes scalar and array repository scopes to a string slice.
func (s *GitHubReposScope) UnmarshalYAML(unmarshal func(any) error) error {
	var value any
	if err := unmarshal(&value); err != nil {
		return err
	}
	scope, err := parseGitHubReposScope(value)
	if err != nil {
		return err
	}
	*s = scope
	return nil
}

// UnmarshalJSON normalizes scalar and array repository scopes to a string slice.
func (s *GitHubReposScope) UnmarshalJSON(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	scope, err := parseGitHubReposScope(value)
	if err != nil {
		return err
	}
	*s = scope
	return nil
}

// GitHubToolConfig represents the configuration for the GitHub tool
// Can be nil (enabled with defaults), string, or an object with specific settings
type GitHubToolConfig struct {
	Allowed     GitHubAllowedTools `yaml:"allowed,omitempty"`
	Mode        GitHubMCPMode      `yaml:"mode,omitempty"`
	Type        string             `yaml:"type,omitempty"`
	Version     string             `yaml:"version,omitempty"`
	Args        []string           `yaml:"args,omitempty"`
	ReadOnly    bool               `yaml:"read-only,omitempty"`
	GitHubToken string             `yaml:"github-token,omitempty"`
	Toolset     GitHubToolsets     `yaml:"toolsets,omitempty"`
	Lockdown    bool               `yaml:"lockdown,omitempty"`
	GitHubApp   *GitHubAppConfig   `yaml:"github-app,omitempty"` // GitHub App configuration for token minting

	// Guard policy fields (flat syntax under github:)
	// AllowedRepos defines the access scope for policy enforcement.
	// Supports: "all", "public", or an array of patterns ["owner/repo", "owner/*"] (lowercase)
	AllowedRepos GitHubReposScope `yaml:"allowed-repos,omitempty"`
	// Repos is deprecated. Use AllowedRepos (yaml:"allowed-repos") instead.
	Repos         GitHubReposScope `yaml:"repos,omitempty"`
	reposParseErr error
	// MinIntegrity defines the minimum integrity level required: "none", "unapproved", "approved", "merged"
	MinIntegrity GitHubIntegrityLevel `yaml:"min-integrity,omitempty"`
	// BlockedUsers is an optional list of GitHub usernames whose content is unconditionally blocked.
	// Items from these users receive "blocked" integrity (below "none") and are always denied.
	BlockedUsers []string `yaml:"blocked-users,omitempty"`
	// BlockedUsersExpr holds a GitHub Actions expression (e.g. "${{ vars.BLOCKED_USERS }}") that
	// resolves at runtime to a comma- or newline-separated list of blocked usernames.
	// Set when the blocked-users field is a string expression rather than a literal array.
	BlockedUsersExpr string `yaml:"-"`
	// TrustedUsers is an optional list of GitHub usernames whose content is elevated to "approved"
	// integrity regardless of author_association. Takes precedence over min-integrity checks but
	// not over blocked-users. Requires min-integrity to be set.
	TrustedUsers []string `yaml:"trusted-users,omitempty"`
	// TrustedUsersExpr holds a GitHub Actions expression (e.g. "${{ vars.TRUSTED_USERS }}") that
	// resolves at runtime to a comma- or newline-separated list of trusted usernames.
	// Set when the trusted-users field is a string expression rather than a literal array.
	TrustedUsersExpr string `yaml:"-"`
	// ApprovalLabels is an optional list of GitHub label names that promote a content item's
	// effective integrity to "approved" when present. Does not override BlockedUsers.
	ApprovalLabels []string `yaml:"approval-labels,omitempty"`
	// ApprovalLabelsExpr holds a GitHub Actions expression (e.g. "${{ vars.APPROVAL_LABELS }}") that
	// resolves at runtime to a comma- or newline-separated list of approval label names.
	// Set when the approval-labels field is a string expression rather than a literal array.
	ApprovalLabelsExpr string `yaml:"-"`
	// EndorsementReactions is an optional list of GitHub reaction types that promote content
	// integrity to "approved" when added by maintainers. Only enforced in proxy mode (DIFC/CLI proxy);
	// ignored in MCP gateway mode. Requires integrity-reactions feature flag and MCPG >= v0.2.18.
	// When the feature flag is enabled and this field is not set, defaults to ["THUMBS_UP", "HEART"].
	// Valid values: THUMBS_UP, THUMBS_DOWN, HEART, HOORAY, CONFUSED, ROCKET, EYES, LAUGH
	EndorsementReactions []string `yaml:"endorsement-reactions,omitempty"`
	// DisapprovalReactions is an optional list of GitHub reaction types that demote content
	// integrity when added by maintainers. Only enforced in proxy mode (DIFC/CLI proxy);
	// ignored in MCP gateway mode. Requires integrity-reactions feature flag and MCPG >= v0.2.18.
	// When the feature flag is enabled and this field is not set, defaults to ["THUMBS_DOWN", "CONFUSED"].
	// Valid values: THUMBS_UP, THUMBS_DOWN, HEART, HOORAY, CONFUSED, ROCKET, EYES, LAUGH
	DisapprovalReactions []string `yaml:"disapproval-reactions,omitempty"`
	// DisapprovalIntegrity is the integrity level assigned when a disapproval reaction is present.
	// Optional, defaults to "none". Requires integrity-reactions feature flag and MCPG >= v0.2.18.
	// Valid values: "none", "unapproved", "approved", "merged"
	DisapprovalIntegrity GitHubIntegrityLevel `yaml:"disapproval-integrity,omitempty"`
	// EndorserMinIntegrity is the minimum integrity level required for an endorser (reactor) to
	// promote content. Optional, defaults to "approved". Requires integrity-reactions feature flag
	// and MCPG >= v0.2.18.
	// Valid values: "approved", "unapproved", "merged"
	EndorserMinIntegrity GitHubIntegrityLevel `yaml:"endorser-min-integrity,omitempty"`
	// PrivateToPublicFlows opts out of cross-visibility protections for private→public data flows.
	// Accepts either the string "allow" (blanket opt-out) or a []string of MCP server IDs
	// (selective exemption for those servers only).
	//   - "allow" → compiler emits gateway.forcePublicRepos: false; rejected in strict mode.
	//   - []string → compiler emits gateway.sinkVisibilityExemptServers with the listed IDs.
	// See MCP Gateway Specification Section 10.9.
	PrivateToPublicFlows any `yaml:"-"`
}

// PlaywrightToolConfig represents the configuration for the Playwright tool
type PlaywrightToolConfig struct {
	Version string `yaml:"version,omitempty"`
	// Mode accepts "cli" for backward compatibility with explicit configurations.
	// CLI mode is also used when this field is omitted.
	Mode     string   `yaml:"mode,omitempty"`
	Browsers []string `yaml:"browsers,omitempty"`
}

// IsCLIMode returns true when the Playwright tool uses the supported CLI mode.
func (p *PlaywrightToolConfig) IsCLIMode() bool {
	return p != nil && (p.Mode == "" || strings.EqualFold(p.Mode, "cli"))
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

// DriveMemoryToolConfig represents the configuration for drive-memory.
type DriveMemoryToolConfig struct {
	// Can be boolean, object, or array - handled by drive_memory_config.go.
	Raw any `yaml:"-"`
}

// CommentMemoryToolConfig represents the configuration for comment-memory.
// This is handled separately by comment_memory.go.
type CommentMemoryToolConfig struct {
	// Can be boolean, object, or null - handled by comment_memory.go
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

	// Guard policies for access control at the MCP gateway level
	// This is a general field that can hold server-specific policy configurations
	// For GitHub: policies are represented via GitHubAllowOnlyPolicy on GitHubToolConfig
	// For Jira/WorkIQ: define similar server-specific policy types
	GuardPolicies map[string]any `yaml:"guard-policies,omitempty"`

	// For truly dynamic configuration (server-specific fields not covered above)
	CustomFields map[string]any `yaml:",inline"`
}

// MCPGatewayRuntimeConfig represents the configuration for the MCP gateway runtime execution
// The gateway routes MCP server calls through a unified HTTP endpoint
// Per MCP Gateway Specification v1.0.0: All stdio-based MCP servers MUST be containerized.
// Direct command execution is not supported.
type MCPGatewayRuntimeConfig struct {
	Container            string                           `yaml:"container,omitempty"`              // Container image for the gateway (required)
	Version              string                           `yaml:"version,omitempty"`                // Optional version/tag for the container
	Entrypoint           string                           `yaml:"entrypoint,omitempty"`             // Optional entrypoint override for the container
	Args                 []string                         `yaml:"args,omitempty"`                   // Arguments for docker run
	EntrypointArgs       []string                         `yaml:"entrypointArgs,omitempty"`         // Arguments passed to container entrypoint
	Env                  map[string]string                `yaml:"env,omitempty"`                    // Environment variables for the gateway
	Port                 int                              `yaml:"port,omitempty"`                   // Port for the gateway HTTP server (default: 8080)
	AgentID              string                           `yaml:"agent-id,omitempty"`               // Agent/session identifier for gateway authentication
	AgentIDs             []string                         `yaml:"-"`                                // Multiple isolated gateway agent identities
	AgentPolicies        map[string]MCPGatewayAgentPolicy `yaml:"-"`                                // Per-agent gateway access policies
	Domain               string                           `yaml:"domain,omitempty"`                 // Domain for gateway URL (localhost or host.docker.internal)
	Mounts               []string                         `yaml:"mounts,omitempty"`                 // Volume mounts for the gateway container (format: "source:dest:mode")
	PayloadDir           string                           `yaml:"payload-dir,omitempty"`            // Directory path for storing large payload JSON files (must be absolute path)
	PayloadPathPrefix    string                           `yaml:"payload-path-prefix,omitempty"`    // Path prefix to remap payload paths for agent containers (e.g., /workspace/payloads)
	PayloadSizeThreshold int                              `yaml:"payload-size-threshold,omitempty"` // Size threshold in bytes for storing payloads to disk (default: 524288 = 512KB)
	TrustedBots          []string                         `yaml:"trusted-bots,omitempty"`           // Additional bot identity strings to pass to the gateway, merged with its built-in list
	KeepaliveInterval    int                              `yaml:"keepalive-interval,omitempty"`     // Keepalive ping interval in seconds for HTTP MCP backends (0=default 1500s, -1=disabled, >0=custom)
	SessionTimeout       string                           `yaml:"session-timeout,omitempty"`        // Session timeout for MCP gateway sessions as a Go duration string (e.g. "4h", "30m"); empty = gateway default (precedence: stdin config > MCP_GATEWAY_SESSION_TIMEOUT env var > built-in default 6h)
	ToolTimeout          string                           `yaml:"tool-timeout,omitempty"`           // Timeout for individual MCP tool calls as a Go duration string (e.g. "2m", "30s"); empty = gateway built-in default (60s)
	StartupTimeout       int                              `yaml:"-"`                                // Startup timeout in seconds for all MCP backends; always emitted to override gateway's built-in 30s default (gh-aw default: 120s, from tools.startup-timeout)
	OTLPEndpoint         string                           `yaml:"-"`                                // OTLP collector endpoint (derived from observability.otlp, not user-settable)
	OTLPHeaders          string                           `yaml:"-"`                                // Raw OTLP HTTP headers string (derived from observability.otlp, not user-settable)
	// ForcePublicRepos controls the gateway's runtime public-repo override.
	// When set to a pointer to false, the compiler emits "forcePublicRepos": false in the gateway
	// JSON config, disabling the runtime check that restricts repos to "public" when the
	// workflow runs in a public repository. Set from tools.github.private-to-public-flows: allow.
	ForcePublicRepos *bool `yaml:"-"`
	// SinkVisibilityExemptServers is the list of MCP server IDs exempt from default
	// sink-visibility="public" enforcement. Emitted as gateway.sinkVisibilityExemptServers.
	// Set from tools.github.private-to-public-flows: [server-ids...].
	SinkVisibilityExemptServers []string `yaml:"-"`
}

type MCPGatewayAgentPolicy struct {
	Servers   []string            `json:"servers"`
	Tools     map[string][]string `json:"tools,omitempty"`
	AllowOnly map[string]any      `json:"allow-only,omitempty"`
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
	case "agentic-workflows":
		return t.AgenticWorkflows != nil
	case "cache-memory":
		return t.CacheMemory != nil
	case "drive-memory":
		return t.DriveMemory != nil
	case "comment-memory":
		return t.CommentMemory != nil
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
	if t.AgenticWorkflows != nil {
		names = append(names, "agentic-workflows")
	}
	if t.CacheMemory != nil {
		names = append(names, "cache-memory")
	}
	if t.DriveMemory != nil {
		names = append(names, "drive-memory")
	}
	if t.CommentMemory != nil {
		names = append(names, "comment-memory")
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
