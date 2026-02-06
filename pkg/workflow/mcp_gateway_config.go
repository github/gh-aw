// Package workflow provides MCP gateway configuration management for agentic workflows.
//
// # MCP Gateway Configuration
//
// The MCP gateway acts as a proxy between AI engines and MCP servers, providing
// protocol translation, connection management, and security features. This file
// handles the configuration and setup of the MCP gateway for workflow execution.
//
// Key responsibilities:
//   - Setting default MCP gateway container and version
//   - Ensuring gateway configuration exists with sensible defaults
//   - Building gateway configuration for MCP config files
//   - Managing gateway port, domain, and API key settings
//
// The gateway configuration includes:
//   - Container image and version (defaults to github/gh-aw-mcpg)
//   - Network port (default: 80)
//   - Domain for gateway access (localhost or host.docker.internal)
//   - API key for authentication
//   - Volume mounts for workspace and temporary directories
//
// Configuration flow:
//  1. ensureDefaultMCPGatewayConfig: Sets defaults if not provided
//  2. buildMCPGatewayConfig: Builds gateway config for MCP files
//  3. isSandboxDisabled: Checks if sandbox features are disabled
//
// When sandbox is disabled (sandbox: false), the gateway is skipped entirely
// and MCP servers communicate directly without the gateway proxy.
//
// Related files:
//   - mcp_gateway_constants.go: Gateway version and container constants
//   - mcp_setup_generator.go: Setup step generation with gateway startup
//   - mcp_renderer.go: YAML rendering for MCP configurations
//
// Example gateway configuration:
//
//	sandbox:
//	  mcp:
//	    container: github/gh-aw-mcpg
//	    version: v0.0.12
//	    port: 80
//	    domain: host.docker.internal
//	    mounts:
//	      - /opt:/opt:ro
//	      - /tmp:/tmp:rw
package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpGatewayConfigLog = logger.New("workflow:mcp_gateway_config")

// ensureDefaultMCPGatewayConfig ensures MCP gateway has default configuration if not provided
// The MCP gateway is mandatory and defaults to github/gh-aw-mcpg
func ensureDefaultMCPGatewayConfig(workflowData *WorkflowData) {
	if workflowData == nil {
		return
	}

	// Ensure SandboxConfig exists
	if workflowData.SandboxConfig == nil {
		workflowData.SandboxConfig = &SandboxConfig{}
	}

	// Ensure MCP gateway config exists with defaults
	if workflowData.SandboxConfig.MCP == nil {
		mcpGatewayConfigLog.Print("No MCP gateway configuration found, setting default configuration")
		workflowData.SandboxConfig.MCP = &MCPGatewayRuntimeConfig{
			Container: constants.DefaultMCPGatewayContainer,
			Version:   string(constants.DefaultMCPGatewayVersion),
			Port:      int(DefaultMCPGatewayPort),
		}
	} else {
		// Fill in defaults for missing fields
		if workflowData.SandboxConfig.MCP.Container == "" {
			workflowData.SandboxConfig.MCP.Container = constants.DefaultMCPGatewayContainer
		}
		// Only replace empty version with default - preserve user-specified versions including "latest"
		if workflowData.SandboxConfig.MCP.Version == "" {
			workflowData.SandboxConfig.MCP.Version = string(constants.DefaultMCPGatewayVersion)
		}
		if workflowData.SandboxConfig.MCP.Port == 0 {
			workflowData.SandboxConfig.MCP.Port = int(DefaultMCPGatewayPort)
		}
	}

	// Ensure default mounts are set if not provided
	if len(workflowData.SandboxConfig.MCP.Mounts) == 0 {
		mcpGatewayConfigLog.Print("Setting default gateway mounts")
		workflowData.SandboxConfig.MCP.Mounts = []string{
			"/opt:/opt:ro",
			"/tmp:/tmp:rw",
			"${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:rw",
		}
	}

	// Ensure default payloadDir is set if not provided
	if workflowData.SandboxConfig.MCP.PayloadDir == "" {
		mcpGatewayConfigLog.Print("Setting default gateway payloadDir")
		workflowData.SandboxConfig.MCP.PayloadDir = constants.DefaultMCPGatewayPayloadDir
	}
}

// buildMCPGatewayConfig builds the gateway configuration for inclusion in MCP config files
// Per MCP Gateway Specification v1.0.0 section 4.1.3, the gateway section is required with port and domain
// Returns nil if sandbox is disabled (sandbox: false) to skip gateway completely
func buildMCPGatewayConfig(workflowData *WorkflowData) *MCPGatewayRuntimeConfig {
	if workflowData == nil {
		return nil
	}

	// If sandbox is disabled, skip gateway configuration entirely
	if isSandboxDisabled(workflowData) {
		return nil
	}

	// Ensure default configuration is set
	ensureDefaultMCPGatewayConfig(workflowData)

	// Return gateway config with required fields populated
	// Use ${...} syntax for environment variable references that will be resolved by the gateway at runtime
	// Per MCP Gateway Specification v1.0.0 section 4.2, variable expressions use "${VARIABLE_NAME}" syntax
	return &MCPGatewayRuntimeConfig{
		Port:       int(DefaultMCPGatewayPort),   // Will be formatted as "${MCP_GATEWAY_PORT}" in renderer
		Domain:     "${MCP_GATEWAY_DOMAIN}",      // Gateway variable expression
		APIKey:     "${MCP_GATEWAY_API_KEY}",     // Gateway variable expression
		PayloadDir: "${MCP_GATEWAY_PAYLOAD_DIR}", // Gateway variable expression for payload directory
	}
}

// isSandboxDisabled checks if sandbox features are completely disabled (sandbox: false)
func isSandboxDisabled(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.SandboxConfig == nil {
		return false
	}
	// Check if sandbox was explicitly disabled via sandbox: false
	return workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled
}
