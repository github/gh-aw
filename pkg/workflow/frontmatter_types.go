package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var frontmatterTypesLog = logger.New("workflow:frontmatter_types")

// RuntimeConfig represents the configuration for a single runtime
type RuntimeConfig struct {
	Version string `json:"version,omitempty"` // Version of the runtime (e.g., "20" for Node, "3.11" for Python)
}

// RuntimesConfig represents the configuration for all runtime environments
// This provides type-safe access to runtime version overrides
type RuntimesConfig struct {
	Node   *RuntimeConfig `json:"node,omitempty"`   // Node.js runtime
	Python *RuntimeConfig `json:"python,omitempty"` // Python runtime
	Go     *RuntimeConfig `json:"go,omitempty"`     // Go runtime
	UV     *RuntimeConfig `json:"uv,omitempty"`     // uv package installer
	Bun    *RuntimeConfig `json:"bun,omitempty"`    // Bun runtime
	Deno   *RuntimeConfig `json:"deno,omitempty"`   // Deno runtime
}

// PermissionsConfig represents GitHub Actions permissions configuration
// Supports both shorthand (read-all, write-all) and detailed scope-based permissions
type PermissionsConfig struct {
	// Shorthand permission (read-all, write-all, read, write, none)
	Shorthand string `json:"-"` // Not in JSON, set when parsing shorthand format

	// Detailed permissions by scope
	Actions              string `json:"actions,omitempty"`
	Checks               string `json:"checks,omitempty"`
	Contents             string `json:"contents,omitempty"`
	Deployments          string `json:"deployments,omitempty"`
	IDToken              string `json:"id-token,omitempty"`
	Issues               string `json:"issues,omitempty"`
	Discussions          string `json:"discussions,omitempty"`
	Packages             string `json:"packages,omitempty"`
	Pages                string `json:"pages,omitempty"`
	PullRequests         string `json:"pull-requests,omitempty"`
	RepositoryProjects   string `json:"repository-projects,omitempty"`
	SecurityEvents       string `json:"security-events,omitempty"`
	Statuses             string `json:"statuses,omitempty"`
	OrganizationProjects string `json:"organization-projects,omitempty"`
	OrganizationPackages string `json:"organization-packages,omitempty"`
}

// PluginMCPConfig represents MCP configuration for a plugin
type PluginMCPConfig struct {
	Env map[string]string `json:"env,omitempty"` // Environment variables for MCP server instantiation
}

// PluginItem represents configuration for a single plugin
// Supports both simple string format and object format with MCP configuration
type PluginItem struct {
	ID  string           `json:"id"`            // Plugin identifier/repository slug (e.g., "org/repo")
	MCP *PluginMCPConfig `json:"mcp,omitempty"` // Optional MCP configuration
}

// PluginInfo encapsulates all plugin-related configuration
// This consolidates plugins list, custom token, and per-plugin MCP configs
type PluginInfo struct {
	Plugins     []string                    // Plugin repository slugs to install
	CustomToken string                      // Custom github-token for plugin installation
	MCPConfigs  map[string]*PluginMCPConfig // Per-plugin MCP configurations (keyed by plugin ID)
}

// PluginsConfig represents plugin configuration for installation (for parsing only)
// Supports object format with repos list, optional custom github-token
type PluginsConfig struct {
	Repos       []string `json:"repos"`                  // List of plugin repository slugs (required)
	GitHubToken string   `json:"github-token,omitempty"` // Custom GitHub token for plugin installation
}

// FrontmatterConfig represents the structured configuration from workflow frontmatter
// This provides compile-time type safety and clearer error messages compared to map[string]any
type FrontmatterConfig struct {
	// Core workflow fields
	Name           string   `json:"name,omitempty"`
	Description    string   `json:"description,omitempty"`
	Engine         string   `json:"engine,omitempty"`
	Source         string   `json:"source,omitempty"`
	TrackerID      string   `json:"tracker-id,omitempty"`
	Version        string   `json:"version,omitempty"`
	TimeoutMinutes int      `json:"timeout-minutes,omitempty"`
	Strict         *bool    `json:"strict,omitempty"` // Pointer to distinguish unset from false
	Labels         []string `json:"labels,omitempty"`

	// Configuration sections - using strongly-typed structs
	Tools            *ToolsConfig       `json:"tools,omitempty"`
	MCPServers       map[string]any     `json:"mcp-servers,omitempty"` // Legacy field, use Tools instead
	RuntimesTyped    *RuntimesConfig    `json:"-"`                     // New typed field (not in JSON to avoid conflict)
	Runtimes         map[string]any     `json:"runtimes,omitempty"`    // Deprecated: use RuntimesTyped
	Jobs             map[string]any     `json:"jobs,omitempty"`        // Custom workflow jobs (too dynamic to type)
	SafeOutputs      *SafeOutputsConfig `json:"safe-outputs,omitempty"`
	SafeInputs       *SafeInputsConfig  `json:"safe-inputs,omitempty"`
	PermissionsTyped *PermissionsConfig `json:"-"` // New typed field (not in JSON to avoid conflict)

	// Event and trigger configuration
	On          map[string]any `json:"on,omitempty"`          // Complex trigger config with many variants (too dynamic to type)
	Permissions map[string]any `json:"permissions,omitempty"` // Deprecated: use PermissionsTyped (can be string or map)
	Concurrency map[string]any `json:"concurrency,omitempty"`
	If          string         `json:"if,omitempty"`

	// Network and sandbox configuration
	Network *NetworkPermissions `json:"network,omitempty"`
	Sandbox *SandboxConfig      `json:"sandbox,omitempty"`

	// Plugin configuration
	// Supports both array format ([]string) and object format (PluginsConfig)
	// This field is handled specially in parsing to support both formats
	Plugins      any            `json:"plugins,omitempty"` // Can be []string or map[string]any
	PluginsTyped *PluginsConfig `json:"-"`                 // Typed plugin configuration (not in JSON)
	PluginsRepos []string       `json:"-"`                 // Extracted plugin repos (not in JSON)
	PluginsToken string         `json:"-"`                 // Extracted plugin token (not in JSON)

	// Feature flags and other settings
	Features map[string]any    `json:"features,omitempty"` // Dynamic feature flags
	Env      map[string]string `json:"env,omitempty"`
	Secrets  map[string]any    `json:"secrets,omitempty"`

	// Workflow execution settings
	RunsOn      string         `json:"runs-on,omitempty"`
	RunName     string         `json:"run-name,omitempty"`
	Steps       []any          `json:"steps,omitempty"`       // Custom workflow steps
	PostSteps   []any          `json:"post-steps,omitempty"`  // Post-workflow steps
	Environment map[string]any `json:"environment,omitempty"` // GitHub environment
	Container   map[string]any `json:"container,omitempty"`
	Services    map[string]any `json:"services,omitempty"`
	Cache       map[string]any `json:"cache,omitempty"`

	// Import and inclusion
	Imports any `json:"imports,omitempty"` // Can be string or array
	Include any `json:"include,omitempty"` // Can be string or array

	// Metadata
	Metadata      map[string]string    `json:"metadata,omitempty"` // Custom metadata key-value pairs
	SecretMasking *SecretMaskingConfig `json:"secret-masking,omitempty"`
	GithubToken   string               `json:"github-token,omitempty"`

	// Command/bot configuration
	Roles []string `json:"roles,omitempty"`
	Bots  []string `json:"bots,omitempty"`
}

// unmarshalFromMap converts a value from a map[string]any to a destination variable
// using JSON marshaling/unmarshaling for type conversion.
// This provides cleaner error messages than manual type assertions.
//
// Parameters:
//   - data: The map containing the configuration data
//   - key: The key to extract from the map
//   - dest: Pointer to the destination variable to unmarshal into (can be any type)
//
// Returns an error if:
//   - The key doesn't exist in the map
//   - The value cannot be marshaled to JSON
//   - The JSON cannot be unmarshaled into the destination type
//
// Example:
//
//	var name string
//	err := unmarshalFromMap(frontmatter, "name", &name)
//
//	var tools map[string]any
//	err := unmarshalFromMap(frontmatter, "tools", &tools)
func unmarshalFromMap(data map[string]any, key string, dest any) error {
	value, exists := data[key]
	if !exists {
		return fmt.Errorf("key '%s' not found in frontmatter", key)
	}

	// Use JSON as intermediate format for type conversion
	// This handles nested maps, arrays, and complex structures cleanly
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal '%s' to JSON: %w", key, err)
	}

	if err := json.Unmarshal(jsonBytes, dest); err != nil {
		return fmt.Errorf("failed to unmarshal '%s' into destination type: %w", key, err)
	}

	return nil
}

// ParseFrontmatterConfig creates a FrontmatterConfig from a raw frontmatter map
// This provides a single entry point for converting untyped frontmatter into
// a structured configuration with better error handling.
func ParseFrontmatterConfig(frontmatter map[string]any) (*FrontmatterConfig, error) {
	frontmatterTypesLog.Printf("Parsing frontmatter config with %d fields", len(frontmatter))
	var config FrontmatterConfig

	// Use JSON marshaling for the entire frontmatter conversion
	// This automatically handles all field mappings
	jsonBytes, err := json.Marshal(frontmatter)
	if err != nil {
		frontmatterTypesLog.Printf("Failed to marshal frontmatter: %v", err)
		return nil, fmt.Errorf("failed to marshal frontmatter to JSON: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, &config); err != nil {
		frontmatterTypesLog.Printf("Failed to unmarshal frontmatter: %v", err)
		return nil, fmt.Errorf("failed to unmarshal frontmatter into config: %w", err)
	}

	// Parse typed Runtimes field if runtimes exist
	if len(config.Runtimes) > 0 {
		runtimesTyped, err := parseRuntimesConfig(config.Runtimes)
		if err == nil {
			config.RuntimesTyped = runtimesTyped
			frontmatterTypesLog.Printf("Parsed typed runtimes config with %d runtimes", countRuntimes(runtimesTyped))
		}
	}

	// Parse typed Permissions field if permissions exist
	if len(config.Permissions) > 0 {
		permissionsTyped, err := parsePermissionsConfig(config.Permissions)
		if err == nil {
			config.PermissionsTyped = permissionsTyped
			frontmatterTypesLog.Print("Parsed typed permissions config")
		}
	}

	// Parse plugins field - supports both array and object formats
	if config.Plugins != nil {
		repos, token, err := parsePluginsConfig(config.Plugins)
		if err == nil {
			config.PluginsRepos = repos
			config.PluginsToken = token
			if len(repos) > 0 {
				frontmatterTypesLog.Printf("Parsed plugins config: %d repos, custom_token=%v", len(repos), token != "")
			}
		}
	}

	frontmatterTypesLog.Printf("Successfully parsed frontmatter config: name=%s, engine=%s", config.Name, config.Engine)
	return &config, nil
}

// parseRuntimesConfig converts a map[string]any to RuntimesConfig
func parseRuntimesConfig(runtimes map[string]any) (*RuntimesConfig, error) {
	config := &RuntimesConfig{}

	for runtimeID, configAny := range runtimes {
		configMap, ok := configAny.(map[string]any)
		if !ok {
			continue
		}

		versionAny, hasVersion := configMap["version"]
		if !hasVersion {
			continue
		}

		// Convert version to string
		var version string
		switch v := versionAny.(type) {
		case string:
			version = v
		case int:
			version = fmt.Sprintf("%d", v)
		case float64:
			if v == float64(int(v)) {
				version = fmt.Sprintf("%d", int(v))
			} else {
				version = fmt.Sprintf("%g", v)
			}
		default:
			continue
		}

		runtimeConfig := &RuntimeConfig{Version: version}

		// Map to specific runtime field
		switch runtimeID {
		case "node":
			config.Node = runtimeConfig
		case "python":
			config.Python = runtimeConfig
		case "go":
			config.Go = runtimeConfig
		case "uv":
			config.UV = runtimeConfig
		case "bun":
			config.Bun = runtimeConfig
		case "deno":
			config.Deno = runtimeConfig
		}
	}

	return config, nil
}

// parsePermissionsConfig converts a map[string]any to PermissionsConfig
func parsePermissionsConfig(permissions map[string]any) (*PermissionsConfig, error) {
	config := &PermissionsConfig{}

	// Check if it's a shorthand permission (single string value)
	if len(permissions) == 1 {
		for key, value := range permissions {
			if strValue, ok := value.(string); ok {
				shorthandPerms := []string{"read-all", "write-all", "read", "write", "none"}
				for _, shorthand := range shorthandPerms {
					if key == shorthand || strValue == shorthand {
						config.Shorthand = shorthand
						return config, nil
					}
				}
			}
		}
	}

	// Parse detailed permissions
	for scope, level := range permissions {
		if levelStr, ok := level.(string); ok {
			switch scope {
			case "actions":
				config.Actions = levelStr
			case "checks":
				config.Checks = levelStr
			case "contents":
				config.Contents = levelStr
			case "deployments":
				config.Deployments = levelStr
			case "id-token":
				config.IDToken = levelStr
			case "issues":
				config.Issues = levelStr
			case "discussions":
				config.Discussions = levelStr
			case "packages":
				config.Packages = levelStr
			case "pages":
				config.Pages = levelStr
			case "pull-requests":
				config.PullRequests = levelStr
			case "repository-projects":
				config.RepositoryProjects = levelStr
			case "security-events":
				config.SecurityEvents = levelStr
			case "statuses":
				config.Statuses = levelStr
			case "organization-projects":
				config.OrganizationProjects = levelStr
			case "organization-packages":
				config.OrganizationPackages = levelStr
			}
		}
	}

	return config, nil
}

// parsePluginsConfig parses the plugins field which can be either:
// 1. Array format: ["org/repo1", "org/repo2"]
// 2. Object format: { "repos": ["org/repo1"], "github-token": "${{ secrets.TOKEN }}" }
// Returns: (repos []string, customToken string, error)
func parsePluginsConfig(plugins any) ([]string, string, error) {
	// Try array format first
	if pluginsArray, ok := plugins.([]any); ok {
		var repos []string
		for _, p := range pluginsArray {
			if pluginStr, ok := p.(string); ok {
				repos = append(repos, pluginStr)
			}
		}
		return repos, "", nil
	}

	// Try object format
	if pluginsMap, ok := plugins.(map[string]any); ok {
		var repos []string
		var token string

		// Extract repos array (required)
		if reposAny, hasRepos := pluginsMap["repos"]; hasRepos {
			if reposArray, ok := reposAny.([]any); ok {
				for _, r := range reposArray {
					if repoStr, ok := r.(string); ok {
						repos = append(repos, repoStr)
					}
				}
			}
		}

		// Extract github-token (optional)
		if tokenAny, hasToken := pluginsMap["github-token"]; hasToken {
			if tokenStr, ok := tokenAny.(string); ok {
				token = tokenStr
			}
		}

		return repos, token, nil
	}

	return nil, "", fmt.Errorf("plugins must be either an array of strings or an object with 'repos' field")
}

// countRuntimes counts the number of non-nil runtimes in RuntimesConfig
func countRuntimes(config *RuntimesConfig) int {
	if config == nil {
		return 0
	}
	count := 0
	if config.Node != nil {
		count++
	}
	if config.Python != nil {
		count++
	}
	if config.Go != nil {
		count++
	}
	if config.UV != nil {
		count++
	}
	if config.Bun != nil {
		count++
	}
	if config.Deno != nil {
		count++
	}
	return count
}

// ExtractMapField is a convenience wrapper for extracting map[string]any fields
// from frontmatter. This maintains backward compatibility with existing extraction
// patterns while preserving original types (avoiding JSON conversion which would
// convert all numbers to float64).
//
// Returns an empty map if the key doesn't exist (for backward compatibility).
func ExtractMapField(frontmatter map[string]any, key string) map[string]any {
	// Check if key exists and value is not nil
	value, exists := frontmatter[key]
	if !exists || value == nil {
		frontmatterTypesLog.Printf("Field '%s' not found in frontmatter, returning empty map", key)
		return make(map[string]any)
	}

	// Direct type assertion to preserve original types (especially integers)
	// This avoids JSON marshaling which would convert integers to float64
	if valueMap, ok := value.(map[string]any); ok {
		frontmatterTypesLog.Printf("Extracted map field '%s' with %d entries", key, len(valueMap))
		return valueMap
	}

	// For backward compatibility, return empty map if not a map
	frontmatterTypesLog.Printf("Field '%s' is not a map type, returning empty map", key)
	return make(map[string]any)
}

// ExtractStringField is a convenience wrapper for extracting string fields.
// Returns empty string if the key doesn't exist or cannot be converted.
func ExtractStringField(frontmatter map[string]any, key string) string {
	var result string
	err := unmarshalFromMap(frontmatter, key, &result)
	if err != nil {
		return ""
	}
	return result
}

// ExtractIntField is a convenience wrapper for extracting integer fields.
// Returns 0 if the key doesn't exist or cannot be converted.
func ExtractIntField(frontmatter map[string]any, key string) int {
	var result int
	err := unmarshalFromMap(frontmatter, key, &result)
	if err != nil {
		return 0
	}
	return result
}

// ToMap converts FrontmatterConfig back to map[string]any for backward compatibility
// This allows gradual migration from map[string]any to strongly-typed config
func (fc *FrontmatterConfig) ToMap() map[string]any {
	result := make(map[string]any)

	// Core fields
	if fc.Name != "" {
		result["name"] = fc.Name
	}
	if fc.Description != "" {
		result["description"] = fc.Description
	}
	if fc.Engine != "" {
		result["engine"] = fc.Engine
	}
	if fc.Source != "" {
		result["source"] = fc.Source
	}
	if fc.TrackerID != "" {
		result["tracker-id"] = fc.TrackerID
	}
	if fc.Version != "" {
		result["version"] = fc.Version
	}
	if fc.TimeoutMinutes != 0 {
		result["timeout-minutes"] = fc.TimeoutMinutes
	}
	if fc.Strict != nil {
		result["strict"] = *fc.Strict
	}
	if len(fc.Labels) > 0 {
		result["labels"] = fc.Labels
	}

	// Configuration sections
	if fc.Tools != nil {
		result["tools"] = fc.Tools.ToMap()
	}
	if fc.MCPServers != nil {
		result["mcp-servers"] = fc.MCPServers
	}
	// Prefer RuntimesTyped over Runtimes for conversion
	if fc.RuntimesTyped != nil {
		result["runtimes"] = runtimesConfigToMap(fc.RuntimesTyped)
	} else if fc.Runtimes != nil {
		result["runtimes"] = fc.Runtimes
	}
	if fc.Jobs != nil {
		result["jobs"] = fc.Jobs
	}
	if fc.SafeOutputs != nil {
		// Convert SafeOutputsConfig to map - would need a ToMap method
		result["safe-outputs"] = fc.SafeOutputs
	}
	if fc.SafeInputs != nil {
		// Convert SafeInputsConfig to map - would need a ToMap method
		result["safe-inputs"] = fc.SafeInputs
	}

	// Event and trigger configuration
	if fc.On != nil {
		result["on"] = fc.On
	}
	// Prefer PermissionsTyped over Permissions for conversion
	if fc.PermissionsTyped != nil {
		result["permissions"] = permissionsConfigToMap(fc.PermissionsTyped)
	} else if fc.Permissions != nil {
		result["permissions"] = fc.Permissions
	}
	if fc.Concurrency != nil {
		result["concurrency"] = fc.Concurrency
	}
	if fc.If != "" {
		result["if"] = fc.If
	}

	// Network and sandbox
	if fc.Network != nil {
		// Convert NetworkPermissions to map format
		// If allowed list is just ["defaults"], convert to string format "defaults"
		if len(fc.Network.Allowed) == 1 && fc.Network.Allowed[0] == "defaults" && fc.Network.Firewall == nil && len(fc.Network.Blocked) == 0 {
			result["network"] = "defaults"
		} else {
			networkMap := make(map[string]any)
			if len(fc.Network.Allowed) > 0 {
				networkMap["allowed"] = fc.Network.Allowed
			}
			if len(fc.Network.Blocked) > 0 {
				networkMap["blocked"] = fc.Network.Blocked
			}
			if fc.Network.Firewall != nil {
				networkMap["firewall"] = fc.Network.Firewall
			}
			if len(networkMap) > 0 {
				result["network"] = networkMap
			}
		}
	}
	if fc.Sandbox != nil {
		result["sandbox"] = fc.Sandbox
	}

	// Plugins - use parsed repos and token if available
	if len(fc.PluginsRepos) > 0 {
		if fc.PluginsToken != "" {
			// Object format with custom token
			result["plugins"] = map[string]any{
				"repos":        fc.PluginsRepos,
				"github-token": fc.PluginsToken,
			}
		} else {
			// Array format
			result["plugins"] = fc.PluginsRepos
		}
	} else if fc.Plugins != nil {
		// Fallback to original value if parsing didn't populate PluginsRepos
		result["plugins"] = fc.Plugins
	}

	// Features and environment
	if fc.Features != nil {
		result["features"] = fc.Features
	}
	if fc.Env != nil {
		result["env"] = fc.Env
	}
	if fc.Secrets != nil {
		result["secrets"] = fc.Secrets
	}

	// Execution settings
	if fc.RunsOn != "" {
		result["runs-on"] = fc.RunsOn
	}
	if fc.RunName != "" {
		result["run-name"] = fc.RunName
	}
	if fc.Steps != nil {
		result["steps"] = fc.Steps
	}
	if fc.PostSteps != nil {
		result["post-steps"] = fc.PostSteps
	}
	if fc.Environment != nil {
		result["environment"] = fc.Environment
	}
	if fc.Container != nil {
		result["container"] = fc.Container
	}
	if fc.Services != nil {
		result["services"] = fc.Services
	}
	if fc.Cache != nil {
		result["cache"] = fc.Cache
	}

	// Import and inclusion
	if fc.Imports != nil {
		result["imports"] = fc.Imports
	}
	if fc.Include != nil {
		result["include"] = fc.Include
	}

	// Metadata
	if fc.Metadata != nil {
		result["metadata"] = fc.Metadata
	}
	if fc.SecretMasking != nil {
		result["secret-masking"] = fc.SecretMasking
	}
	if fc.GithubToken != "" {
		result["github-token"] = fc.GithubToken
	}
	if fc.Roles != nil {
		result["roles"] = fc.Roles
	}
	if fc.Bots != nil {
		result["bots"] = fc.Bots
	}

	return result
}

// runtimesConfigToMap converts RuntimesConfig back to map[string]any
func runtimesConfigToMap(config *RuntimesConfig) map[string]any {
	if config == nil {
		return nil
	}

	result := make(map[string]any)

	if config.Node != nil {
		result["node"] = map[string]any{"version": config.Node.Version}
	}
	if config.Python != nil {
		result["python"] = map[string]any{"version": config.Python.Version}
	}
	if config.Go != nil {
		result["go"] = map[string]any{"version": config.Go.Version}
	}
	if config.UV != nil {
		result["uv"] = map[string]any{"version": config.UV.Version}
	}
	if config.Bun != nil {
		result["bun"] = map[string]any{"version": config.Bun.Version}
	}
	if config.Deno != nil {
		result["deno"] = map[string]any{"version": config.Deno.Version}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

// permissionsConfigToMap converts PermissionsConfig back to map[string]any
func permissionsConfigToMap(config *PermissionsConfig) map[string]any {
	if config == nil {
		return nil
	}

	// If shorthand is set, return it directly
	if config.Shorthand != "" {
		return map[string]any{config.Shorthand: config.Shorthand}
	}

	result := make(map[string]any)

	if config.Actions != "" {
		result["actions"] = config.Actions
	}
	if config.Checks != "" {
		result["checks"] = config.Checks
	}
	if config.Contents != "" {
		result["contents"] = config.Contents
	}
	if config.Deployments != "" {
		result["deployments"] = config.Deployments
	}
	if config.IDToken != "" {
		result["id-token"] = config.IDToken
	}
	if config.Issues != "" {
		result["issues"] = config.Issues
	}
	if config.Discussions != "" {
		result["discussions"] = config.Discussions
	}
	if config.Packages != "" {
		result["packages"] = config.Packages
	}
	if config.Pages != "" {
		result["pages"] = config.Pages
	}
	if config.PullRequests != "" {
		result["pull-requests"] = config.PullRequests
	}
	if config.RepositoryProjects != "" {
		result["repository-projects"] = config.RepositoryProjects
	}
	if config.SecurityEvents != "" {
		result["security-events"] = config.SecurityEvents
	}
	if config.Statuses != "" {
		result["statuses"] = config.Statuses
	}
	if config.OrganizationProjects != "" {
		result["organization-projects"] = config.OrganizationProjects
	}
	if config.OrganizationPackages != "" {
		result["organization-packages"] = config.OrganizationPackages
	}

	if len(result) == 0 {
		return nil
	}

	return result
}
