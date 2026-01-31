// Package workflow provides constants for MCP gateway configuration.
//
// # MCP Gateway Constants
//
// This file defines default values and constants used by the MCP gateway
// throughout the workflow compilation process. These constants ensure
// consistent configuration across different components.
//
// Gateway default values:
//   - Port: 80 (HTTP standard port)
//
// The MCP gateway port is used when:
//   - No custom port is specified in sandbox.mcp.port
//   - Building gateway configuration in mcp_gateway_config.go
//   - Generating gateway startup commands in mcp_setup_generator.go
//
// Historical note:
// This constant was originally used with the awmg gateway binary.
// The binary has been removed but the constant is retained for
// backwards compatibility with existing workflow configurations.
//
// Related files:
//   - mcp_gateway_config.go: Uses DefaultMCPGatewayPort for configuration
//   - mcp_setup_generator.go: Uses port for gateway startup
//   - constants/constants.go: Other MCP-related constants (versions, containers)
//
// Related constants in pkg/constants:
//   - DefaultMCPGatewayVersion: Gateway container version
//   - DefaultMCPGatewayContainer: Gateway container image
//   - DefaultGitHubMCPServerVersion: GitHub MCP server version
package workflow

const (
	// DefaultMCPGatewayPort is the default port for the MCP gateway
	// This constant is kept for backwards compatibility with existing configurations
	// even though the awmg gateway binary has been removed.
	DefaultMCPGatewayPort = 80
)
