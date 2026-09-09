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
//   - Managing gateway port, domain, and agent ID settings
//
// The gateway configuration includes:
//   - Container image and version (defaults to github/gh-aw-mcpg)
//   - Network port (default: 8080)
//   - Domain for gateway access (localhost or host.docker.internal)
//   - Agent/session identifier for authentication
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
//	    port: 8080
//	    domain: host.docker.internal
//	    mounts:
//	      - /opt:/opt:ro
//	      - /tmp:/tmp:rw
package workflow

import (
	"slices"
	"sort"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpGatewayConfigLog = logger.New("workflow:mcp_gateway_config")

const safeOutputsMount = "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw"

func defaultMCPGatewayVersionForWorkflow(workflowData *WorkflowData) string {
	defaultVersion := string(constants.DefaultMCPGatewayVersion)
	if enclaveDynamicRepositoryPolicyEnabled(workflowData) &&
		!versionAtLeast(defaultVersion, defaultVersion, string(constants.MCPGDynamicRepositoryDelegationMinVersion)) {
		return string(constants.MCPGDynamicRepositoryDelegationMinVersion)
	}
	return defaultVersion
}

func effectiveMCPGatewayVersion(workflowData *WorkflowData) string {
	if workflowData != nil &&
		workflowData.SandboxConfig != nil &&
		workflowData.SandboxConfig.MCP != nil &&
		workflowData.SandboxConfig.MCP.Version != "" {
		return workflowData.SandboxConfig.MCP.Version
	}
	return defaultMCPGatewayVersionForWorkflow(workflowData)
}

func primaryGitHubMCPEnabled(workflowData *WorkflowData) bool {
	if workflowData == nil {
		return false
	}
	githubTool, hasGitHub := workflowData.Tools["github"]
	return hasGitHub && githubTool != false && !isGitHubCLIModeEnabled(workflowData)
}

func githubBackendIsDynamicDelegationOnly(workflowData *WorkflowData) bool {
	return enclaveDynamicRepositoryPolicyEnabled(workflowData) && !primaryGitHubMCPEnabled(workflowData)
}

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
			Version:   defaultMCPGatewayVersionForWorkflow(workflowData),
			Port:      int(DefaultMCPGatewayPort),
		}
	} else {
		// Fill in defaults for missing fields
		if workflowData.SandboxConfig.MCP.Container == "" {
			workflowData.SandboxConfig.MCP.Container = constants.DefaultMCPGatewayContainer
		}
		// Only replace empty version with default - preserve user-specified versions including "latest"
		if workflowData.SandboxConfig.MCP.Version == "" {
			workflowData.SandboxConfig.MCP.Version = defaultMCPGatewayVersionForWorkflow(workflowData)
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
			safeOutputsMount,
		}
	}

	// Ensure safeoutputs mount is present whenever upload assets is configured,
	// even when users provide custom mounts.
	if workflowData.SafeOutputs != nil &&
		workflowData.SafeOutputs.UploadAssets != nil &&
		!slices.Contains(workflowData.SandboxConfig.MCP.Mounts, safeOutputsMount) {
		workflowData.SandboxConfig.MCP.Mounts = append(workflowData.SandboxConfig.MCP.Mounts, safeOutputsMount)
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
func buildMCPGatewayConfig(workflowData *WorkflowData) *MCPGatewayRuntimeConfig { //nolint:largefunc // Existing gateway config assembly keeps related defaults in one place.
	if workflowData == nil {
		return nil
	}

	// If sandbox is disabled, skip gateway configuration entirely
	if isSandboxDisabled(workflowData) {
		mcpGatewayConfigLog.Print("Sandbox disabled, skipping MCP gateway configuration")
		return nil
	}
	mcpGatewayConfigLog.Print("Building MCP gateway configuration")

	// Ensure default configuration is set
	ensureDefaultMCPGatewayConfig(workflowData)

	// Get payload size threshold (use default if not configured)
	payloadSizeThreshold := workflowData.SandboxConfig.MCP.PayloadSizeThreshold
	if payloadSizeThreshold == 0 {
		payloadSizeThreshold = constants.DefaultMCPGatewayPayloadSizeThreshold
	}

	// Return gateway config with required fields populated
	// Use ${...} syntax for environment variable references that will be resolved by the gateway at runtime
	// Per MCP Gateway Specification v1.0.0 section 4.2, variable expressions use "${VARIABLE_NAME}" syntax
	//
	// OTLPEndpoint and OTLPHeaders are read from workflowData fields set by injectOTLPConfig.
	// These compile-time values (including GitHub Actions expressions such as ${{ secrets.X }})
	// are written directly into the gateway config JSON.
	//
	// SessionTimeout comes from engine.mcp.session-timeout in frontmatter (via EngineConfig).
	// ToolTimeout comes from engine.mcp.tool-timeout in frontmatter (via EngineConfig).
	var sessionTimeout, toolTimeout string
	if workflowData.EngineConfig != nil {
		sessionTimeout = workflowData.EngineConfig.MCPSessionTimeout
		toolTimeout = workflowData.EngineConfig.MCPToolTimeout
	}

	// Compute startupTimeout from tools.startup-timeout (integer seconds) or fall back to
	// constants.DefaultMCPStartupTimeout (120s). Always emitted so that MCP Gateway's
	// built-in 30-second default cannot silently evict a safeoutputs backend that starts
	// slightly late. GitHub Actions expressions (non-numeric strings) fall back to the default.
	startupTimeout := int(constants.DefaultMCPStartupTimeout / time.Second)
	if workflowData.ToolsStartupTimeout != "" {
		if n := templatableIntValue(&workflowData.ToolsStartupTimeout); n > 0 {
			startupTimeout = n
		}
	}

	// Derive ForcePublicRepos and SinkVisibilityExemptServers from tools.github.private-to-public-flows.
	var forcePublicRepos *bool
	var sinkVisibilityExemptServers []string
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.GitHub != nil {
		switch v := workflowData.ParsedTools.GitHub.PrivateToPublicFlows.(type) {
		case string:
			if v == "allow" {
				// Blanket opt-out: disable the runtime public-repos override.
				falseVal := false
				forcePublicRepos = &falseVal
			}
		case []string:
			if len(v) > 0 {
				sinkVisibilityExemptServers = v
			}
		}
	}

	config := &MCPGatewayRuntimeConfig{
		Port:                        int(DefaultMCPGatewayPort),                       // Will be formatted as "${MCP_GATEWAY_PORT}" in renderer
		Domain:                      "${MCP_GATEWAY_DOMAIN}",                          // Gateway variable expression
		AgentID:                     "${MCP_GATEWAY_AGENT_ID}",                        // Gateway variable expression
		PayloadDir:                  "${MCP_GATEWAY_PAYLOAD_DIR}",                     // Gateway variable expression for payload directory
		PayloadPathPrefix:           workflowData.SandboxConfig.MCP.PayloadPathPrefix, // Optional path prefix for agent containers
		PayloadSizeThreshold:        payloadSizeThreshold,                             // Size threshold in bytes
		TrustedBots:                 workflowData.SandboxConfig.MCP.TrustedBots,       // Additional trusted bot identities from frontmatter
		KeepaliveInterval:           workflowData.SandboxConfig.MCP.KeepaliveInterval, // Keepalive interval from frontmatter (0=default, -1=disabled, >0=custom)
		SessionTimeout:              sessionTimeout,                                   // Session timeout from engine.mcp.session-timeout (empty = gateway default 6h)
		ToolTimeout:                 toolTimeout,                                      // Tool timeout from engine.mcp.tool-timeout (empty = gateway built-in default 60s)
		StartupTimeout:              startupTimeout,                                   // Startup timeout in seconds; always set (default: 120s to override gateway's built-in 30s)
		ForcePublicRepos:            forcePublicRepos,                                 // nil = default (true); &false = disable runtime public-repos override
		SinkVisibilityExemptServers: sinkVisibilityExemptServers,                      // Server IDs exempt from default sink-visibility enforcement
		// OTLPEndpoint and OTLPHeaders are set from workflowData by injectOTLPConfig, which is
		// the fully resolved OTLP config (including imports). Using these fields ensures gateway
		// OTLP config honours observability defined in imported shared workflows.
		OTLPEndpoint: workflowData.OTLPEndpoint,
		OTLPHeaders:  workflowData.OTLPHeaders,
	}
	if enclaveGitHubDelegationEnabled(workflowData) {
		manifestServers := collectMCPServersForManifest(workflowData)
		primaryServers := make([]string, 0, len(manifestServers))
		for _, server := range manifestServers {
			primaryServers = append(primaryServers, server.Name)
		}
		primaryGitHubEnabled := primaryGitHubMCPEnabled(workflowData)
		// Dynamic repository delegation keeps the GitHub backend registered so
		// mcpg-issued delegated identities can reach it, but the primary agent
		// identity must not gain GitHub MCP access merely because a dynamic
		// enclave is configured; it requires a separate top-level tools.github.
		if !primaryGitHubEnabled {
			for i, server := range primaryServers {
				if server == "github" {
					primaryServers = append(primaryServers[:i], primaryServers[i+1:]...)
					break
				}
			}
		}
		sort.Strings(primaryServers)
		config.AgentID = ""
		config.AgentIDs = []string{"${MCP_GATEWAY_AGENT_ID}"}
		config.AgentPolicies = map[string]MCPGatewayAgentPolicy{
			"${MCP_GATEWAY_AGENT_ID}": {Servers: primaryServers},
		}
		if enclaveGitHubIssuesEnabled(workflowData) {
			config.AgentIDs = append(config.AgentIDs, "${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}")
			config.AgentPolicies["${AWF_ENCLAVE_GITHUB_MCP_AGENT_ID}"] = enclaveGitHubMCPAgentPolicy(workflowData)
		}
		if primaryGitHubEnabled {
			config.AgentPolicies["${MCP_GATEWAY_AGENT_ID}"] = MCPGatewayAgentPolicy{
				Servers: primaryServers,
				Tools:   map[string][]string{"github": collectGitHubMCPManifestTools(workflowData.Tools["github"])},
			}
		}
		// Dynamic repository delegation is bootstrapped entirely through the five
		// MCP_GATEWAY_DELEGATION_* environment variables set up in
		// generateMCPGatewaySetup and forwarded via appendMCPGatewayConditionalEnvFlags.
		// The gateway's strict-stdin config schema does not accept a
		// "delegationControllers" field, so no value is emitted here.
	}
	return config
}

// isSandboxDisabled checks if sandbox features are completely disabled (sandbox: false)
// This function is DEPRECATED and will return false now since top-level sandbox: false is no longer supported.
// Use isAgentSandboxDisabled() to check if the agent sandbox is disabled.
func isSandboxDisabled(workflowData *WorkflowData) bool {
	// Top-level sandbox: false is no longer supported, so this always returns false
	// The MCP gateway is always enabled
	return false
}

// isAgentSandboxDisabled checks if the agent sandbox (firewall) is explicitly disabled
// via sandbox.agent: false. This disables the agent firewall but keeps the MCP gateway enabled.
func isAgentSandboxDisabled(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.SandboxConfig == nil {
		return false
	}
	// Check if agent sandbox was explicitly disabled via sandbox.agent: false
	disabled := workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled
	if disabled {
		mcpGatewayConfigLog.Print("Agent sandbox (firewall) is explicitly disabled via sandbox.agent: false")
	}
	return disabled
}
