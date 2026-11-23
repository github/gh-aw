package types

// BaseMCPServerConfig contains the shared fields common to all MCP server configurations.
// This base type is embedded by both parser.MCPServerConfig and workflow.MCPServerConfig
// to eliminate duplication while allowing each to have domain-specific fields and struct tags.
type BaseMCPServerConfig struct {
	// Common execution fields
	Command string            `json:"command,omitempty" yaml:"command,omitempty"` // Command to execute (for stdio mode)
	Args    []string          `json:"args,omitempty" yaml:"args,omitempty"`       // Arguments for the command
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`         // Environment variables

	// Type and version
	Type    string `json:"type,omitempty" yaml:"type,omitempty"`       // MCP server type (stdio, http, local, remote)
	Version string `json:"version,omitempty" yaml:"version,omitempty"` // Optional version/tag

	// HTTP-specific fields
	URL        string            `json:"url,omitempty" yaml:"url,omitempty"`                   // URL for HTTP mode MCP servers
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`           // HTTP headers for HTTP mode
	Auth       *MCPAuthConfig    `json:"auth,omitempty" yaml:"auth,omitempty"`                 // Upstream authentication config (HTTP mode only)
	CACertFile string            `json:"ca-cert-file,omitempty" yaml:"ca-cert-file,omitempty"` // Host PEM file trusted only for this HTTP server

	// Container-specific fields
	Container      string   `json:"container,omitempty" yaml:"container,omitempty"`           // Container image for the MCP server
	Entrypoint     string   `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`         // Optional entrypoint override for container
	EntrypointArgs []string `json:"entrypointArgs,omitempty" yaml:"entrypointArgs,omitempty"` // Arguments passed to container entrypoint
	Mounts         []string `json:"mounts,omitempty" yaml:"mounts,omitempty"`                 // Volume mounts for container (format: "source:dest:mode")
}

// MCPAuthConfig represents upstream authentication configuration for an HTTP MCP server.
// When configured, the gateway dynamically acquires tokens and injects them as Authorization
// headers on every outgoing request.
type MCPAuthConfig struct {
	// Type is the authentication type. Currently only "github-oidc" is supported.
	Type string `json:"type" yaml:"type"`
	// Audience defaults to the server URL for direct OIDC or Azure's exchange audience.
	Audience      string `json:"audience,omitempty" yaml:"audience,omitempty"`
	Provider      string `json:"provider,omitempty" yaml:"provider,omitempty"`
	AzureTenantID string `json:"azure-tenant-id,omitempty" yaml:"azure-tenant-id,omitempty"`
	AzureClientID string `json:"azure-client-id,omitempty" yaml:"azure-client-id,omitempty"`
	AzureScope    string `json:"azure-scope,omitempty" yaml:"azure-scope,omitempty"`
	AzureCloud    string `json:"azure-cloud,omitempty" yaml:"azure-cloud,omitempty"`
}
