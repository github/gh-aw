package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var firewallLog = logger.New("workflow:firewall")

// FirewallConfig represents AWF (gh-aw-firewall) configuration for network egress control.
// These settings are specific to the AWF sandbox and do not apply to Sandbox Runtime (SRT).
type FirewallConfig struct {
	Enabled       bool     `yaml:"enabled,omitempty"`        // Enable/disable AWF (default: true for copilot when network restrictions present)
	Version       string   `yaml:"version,omitempty"`        // AWF version (empty = latest)
	Args          []string `yaml:"args,omitempty"`           // Additional arguments to pass to AWF
	LogLevel      string   `yaml:"log_level,omitempty"`      // AWF log level (default: "info")
	CleanupScript string   `yaml:"cleanup_script,omitempty"` // Cleanup script path (default: "./scripts/ci/cleanup.sh")
	SSLBump       bool     `yaml:"ssl_bump,omitempty"`       // AWF-only: Enable SSL Bump for HTTPS content inspection (allows URL path filtering)
	AllowURLs     []string `yaml:"allow_urls,omitempty"`     // AWF-only: URL patterns to allow for HTTPS (requires SSLBump), e.g., "https://github.com/githubnext/*"
}

// isFirewallDisabledBySandboxAgent checks if the firewall is disabled via sandbox.agent: false
func isFirewallDisabledBySandboxAgent(workflowData *WorkflowData) bool {
	return workflowData != nil &&
		workflowData.SandboxConfig != nil &&
		workflowData.SandboxConfig.Agent != nil &&
		workflowData.SandboxConfig.Agent.Disabled
}

// isFirewallEnabled checks if AWF firewall is enabled for the workflow
// Firewall is enabled if network.firewall is explicitly set to true or an object
// Firewall is disabled if sandbox.agent is explicitly set to false
func isFirewallEnabled(workflowData *WorkflowData) bool {
	// Check if sandbox.agent: false (new way to disable firewall)
	if isFirewallDisabledBySandboxAgent(workflowData) {
		firewallLog.Print("Firewall disabled via sandbox.agent: false")
		return false
	}

	// Check network.firewall configuration (deprecated)
	if workflowData != nil && workflowData.NetworkPermissions != nil && workflowData.NetworkPermissions.Firewall != nil {
		enabled := workflowData.NetworkPermissions.Firewall.Enabled
		firewallLog.Printf("Firewall enabled check: %v", enabled)
		return enabled
	}

	firewallLog.Print("Firewall not configured, returning false")
	return false
}

// getFirewallConfig returns the firewall configuration from network permissions
func getFirewallConfig(workflowData *WorkflowData) *FirewallConfig {
	if workflowData == nil {
		return nil
	}

	// Check network.firewall configuration
	if workflowData.NetworkPermissions != nil && workflowData.NetworkPermissions.Firewall != nil {
		config := workflowData.NetworkPermissions.Firewall
		if firewallLog.Enabled() {
			firewallLog.Printf("Retrieved firewall config: enabled=%v, version=%s, logLevel=%s",
				config.Enabled, config.Version, config.LogLevel)
		}
		return config
	}

	return nil
}

// getAgentConfig returns the agent sandbox configuration from sandbox config
func getAgentConfig(workflowData *WorkflowData) *AgentSandboxConfig {
	if workflowData == nil || workflowData.SandboxConfig == nil {
		return nil
	}

	return workflowData.SandboxConfig.Agent
}

// enableFirewallByDefaultForCopilot enables firewall by default for copilot and codex engines
// when network restrictions are present but no explicit firewall configuration exists
// and no SRT sandbox is configured (SRT and AWF are mutually exclusive)
// and sandbox.agent is not explicitly set to false
//
// The firewall is enabled by default for copilot and codex UNLESS:
// - allowed contains "*" (unrestricted network access)
// - sandbox.agent is explicitly set to false
// - SRT sandbox is configured
func enableFirewallByDefaultForCopilot(engineID string, networkPermissions *NetworkPermissions, sandboxConfig *SandboxConfig) {
	// Only apply to copilot and codex engines
	if engineID != "copilot" && engineID != "codex" {
		return
	}

	enableFirewallByDefaultForEngine(engineID, networkPermissions, sandboxConfig)
}

// enableFirewallByDefaultForClaude enables firewall by default for Claude engine
// when network restrictions are present but no explicit firewall configuration exists
// and sandbox.agent is not explicitly set to false
//
// The firewall is enabled by default for Claude UNLESS:
// - allowed contains "*" (unrestricted network access)
// - sandbox.agent is explicitly set to false
func enableFirewallByDefaultForClaude(engineID string, networkPermissions *NetworkPermissions, sandboxConfig *SandboxConfig) {
	// Only apply to claude engine
	if engineID != "claude" {
		return
	}

	enableFirewallByDefaultForEngine(engineID, networkPermissions, sandboxConfig)
}

// enableFirewallByDefaultForEngine enables firewall by default for a given engine
// when network restrictions are present but no explicit firewall configuration exists
// and no SRT sandbox is configured (SRT and AWF are mutually exclusive)
// and sandbox.agent is not explicitly set to false
//
// The firewall is enabled by default for the engine UNLESS:
// - allowed contains "*" (unrestricted network access)
// - sandbox.agent is explicitly set to false
// - SRT sandbox is configured (Copilot only)
func enableFirewallByDefaultForEngine(engineID string, networkPermissions *NetworkPermissions, sandboxConfig *SandboxConfig) {
	// Check if network permissions exist
	if networkPermissions == nil {
		return
	}

	// Check if sandbox.agent: false is set (disables firewall)
	// Use a minimal check here since we don't have WorkflowData
	if sandboxConfig != nil && sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled {
		firewallLog.Print("sandbox.agent: false is set, skipping AWF auto-enablement")
		return
	}

	// Check if SRT is enabled - skip AWF auto-enablement if SRT is configured (Copilot only)
	if engineID == "copilot" && sandboxConfig != nil {
		// Check legacy Type field
		if sandboxConfig.Type == SandboxTypeRuntime {
			firewallLog.Print("SRT sandbox is enabled (via Type), skipping AWF auto-enablement")
			return
		}
		// Check new Agent field
		if sandboxConfig.Agent != nil {
			agentType := getAgentType(sandboxConfig.Agent)
			if agentType == SandboxTypeRuntime || agentType == SandboxTypeSRT {
				firewallLog.Print("SRT sandbox is enabled (via Agent), skipping AWF auto-enablement")
				return
			}
		}
	}

	// Check if firewall is already configured
	if networkPermissions.Firewall != nil {
		firewallLog.Print("Firewall already configured, skipping default enablement")
		return
	}

	// Check if allowed contains "*" (unrestricted network access)
	// If it does, do NOT enable the firewall by default
	for _, domain := range networkPermissions.Allowed {
		if domain == "*" {
			firewallLog.Print("Wildcard '*' in allowed domains, skipping AWF auto-enablement")
			return
		}
	}

	// Enable firewall by default for the engine (copilot, claude, codex)
	// This applies to all cases EXCEPT when allowed = "*"
	networkPermissions.Firewall = &FirewallConfig{
		Enabled: true,
	}
	firewallLog.Printf("Enabled firewall by default for %s engine", engineID)
}

// getAWFImageTag returns the AWF Docker image tag to use for the --image-tag flag.
// This ensures the AWF binary pulls its matching Docker image version instead of latest.
// Returns the version from firewall config if specified, otherwise returns the default version.
// The version is returned without the 'v' prefix (e.g., "0.7.0" instead of "v0.7.0").
func getAWFImageTag(firewallConfig *FirewallConfig) string {
	var version string
	if firewallConfig != nil && firewallConfig.Version != "" {
		version = firewallConfig.Version
		firewallLog.Printf("Using custom AWF image tag: %s", version)
	} else {
		version = string(constants.DefaultFirewallVersion)
		firewallLog.Printf("Using default AWF image tag: %s", version)
	}
	// Strip the 'v' prefix if present (AWF expects version without 'v' prefix)
	return strings.TrimPrefix(version, "v")
}

// getSSLBumpArgs returns the AWF arguments for SSL Bump configuration.
// Returns arguments for --ssl-bump and --allow-urls flags if SSL Bump is enabled.
// SSL Bump enables HTTPS content inspection (v0.9.0+), allowing URL path filtering
// instead of domain-only filtering.
//
// Note: These features are specific to AWF (Agent Workflow Firewall) and do not
// apply to Sandbox Runtime (SRT) or other sandbox configurations.
func getSSLBumpArgs(firewallConfig *FirewallConfig) []string {
	if firewallConfig == nil || !firewallConfig.SSLBump {
		return nil
	}

	var args []string
	args = append(args, "--ssl-bump")
	firewallLog.Print("Added --ssl-bump for HTTPS content inspection")

	// Add allow-urls if specified (requires SSL Bump)
	if len(firewallConfig.AllowURLs) > 0 {
		allowURLs := strings.Join(firewallConfig.AllowURLs, ",")
		args = append(args, "--allow-urls", allowURLs)
		firewallLog.Printf("Added --allow-urls: %s", allowURLs)
	}

	return args
}
