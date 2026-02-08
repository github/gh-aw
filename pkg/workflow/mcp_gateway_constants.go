// Package workflow provides constants for MCP gateway configuration.
//
// # MCP Gateway Constants
//
// This file provides access to MCP gateway configuration constants.
//
// Gateway default values:
//   - Port: 80 (HTTP standard port) - defined in pkg/constants
//
// The MCP gateway port is used when:
//   - No custom port is specified in sandbox.mcp.port
//   - Building gateway configuration in mcp_gateway_config.go
//   - Generating gateway startup commands in mcp_setup_generator.go
//
// Historical note:
// This constant was originally defined locally but has been moved to pkg/constants
// for centralization with other network port constants.
//
// Related files:
//   - mcp_gateway_config.go: Uses DefaultMCPGatewayPort for configuration
//   - mcp_setup_generator.go: Uses port for gateway startup
//   - constants/constants.go: Defines all MCP-related constants (versions, containers, ports)
//
// Related constants in pkg/constants:
//   - DefaultMCPGatewayPort: Gateway port (80)
//   - DefaultMCPGatewayVersion: Gateway container version
//   - DefaultMCPGatewayContainer: Gateway container image
//   - DefaultGitHubMCPServerVersion: GitHub MCP server version
package workflow

import "github.com/github/gh-aw/pkg/constants"

// DefaultMCPGatewayPort is the default port for the MCP gateway
// This is now an alias to the constant defined in pkg/constants
// for backwards compatibility with existing code.
const DefaultMCPGatewayPort = constants.DefaultMCPGatewayPort
