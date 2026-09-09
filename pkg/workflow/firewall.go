package workflow

import (
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var firewallLog = logger.New("workflow:firewall")

// FirewallConfig represents AWF (gh-aw-firewall) configuration for network egress control.
// These settings are specific to the AWF sandbox and do not apply to Sandbox Runtime (SRT).
type FirewallConfig struct {
	Enabled   bool     `yaml:"enabled,omitempty"`    // Enable/disable AWF (default: true for copilot when network restrictions present)
	Version   string   `yaml:"version,omitempty"`    // AWF version (empty = latest)
	Args      []string `yaml:"args,omitempty"`       // Additional arguments to pass to AWF
	LogLevel  string   `yaml:"log_level,omitempty"`  // AWF log level (default: "info")
	SSLBump   bool     `yaml:"ssl_bump,omitempty"`   // AWF-only: Enable SSL Bump for HTTPS content inspection (allows URL path filtering)
	AllowURLs []string `yaml:"allow_urls,omitempty"` // AWF-only: URL patterns to allow for HTTPS (requires SSLBump), e.g., "https://github.com/githubnext/*"
}

// isFirewallDisabledBySandboxAgent checks if the firewall is disabled via sandbox.agent: false
func isFirewallDisabledBySandboxAgent(workflowData *WorkflowData) bool {
	return workflowData != nil &&
		workflowData.SandboxConfig != nil &&
		workflowData.SandboxConfig.Agent != nil &&
		workflowData.SandboxConfig.Agent.Disabled
}

// isFirewallEnabled checks if AWF firewall is enabled for the workflow
// Firewall is enabled if:
// - network.firewall is explicitly set to true or an object
// - sandbox is enabled (via sandbox.agent or legacy sandbox.type field)
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

	// Check if sandbox is enabled (via sandbox.agent or legacy sandbox.type field)
	// When sandbox is enabled, the firewall is implicitly enabled
	if workflowData != nil && isSandboxEnabled(workflowData.SandboxConfig, workflowData.NetworkPermissions) {
		firewallLog.Print("Firewall implicitly enabled via sandbox configuration")
		return true
	}

	firewallLog.Print("Firewall not configured, returning false")
	return false
}

// getFirewallConfig returns the firewall configuration from network permissions
func getFirewallConfig(workflowData *WorkflowData) *FirewallConfig {
	if workflowData == nil {
		return nil
	}

	agentConfig := getAgentConfig(workflowData)
	agentVersion := ""
	if agentConfig != nil {
		agentVersion = agentConfig.Version
	}

	// Check resolved firewall configuration from network permissions
	if workflowData.NetworkPermissions != nil && workflowData.NetworkPermissions.Firewall != nil {
		config := workflowData.NetworkPermissions.Firewall
		if agentVersion != "" {
			if config.Version == agentVersion {
				return config
			}

			overrideConfig := *config
			overrideConfig.Version = agentVersion
			if config.Version == "" {
				firewallLog.Printf("Using sandbox.agent.version %s for firewall version", agentVersion)
			} else {
				firewallLog.Printf("Overriding firewall version %s with sandbox.agent.version %s", config.Version, agentVersion)
			}
			return &overrideConfig
		}
		if firewallLog.Enabled() {
			firewallLog.Printf("Retrieved firewall config: enabled=%v, version=%s, logLevel=%s",
				config.Enabled, config.Version, config.LogLevel)
		}
		return config
	}

	if agentVersion != "" {
		firewallLog.Printf("Using sandbox.agent.version override: %s", agentVersion)
		return &FirewallConfig{
			Enabled: isFirewallEnabled(workflowData),
			Version: agentVersion,
		}
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

// getAgentContainerRuntime returns the container runtime string for the AWF config,
// or an empty string if no custom runtime is configured.
// docker-sbx and cloud-hypervisor are excluded because they are not OCI runtimes;
// they pass --container-runtime via CLI flags in BuildAWFArgs instead.
func getAgentContainerRuntime(workflowData *WorkflowData) string {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Disabled {
		return ""
	}
	// Only gVisor is an OCI runtime that AWF passes through as
	// container.containerRuntime. The docker/docker-sudo-iptables profiles use the
	// default Docker runtime, and docker-sbx/cloud-hypervisor pass
	// --container-runtime via CLI flags in BuildAWFArgs instead.
	if agentConfig.Runtime != AgentRuntimeGVisor {
		return ""
	}
	return string(AgentRuntimeGVisor)
}

// isGVisorRuntime returns true when the agent container should use gVisor (runsc).
func isGVisorRuntime(workflowData *WorkflowData) bool {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Disabled {
		return false
	}
	return agentConfig.Runtime == AgentRuntimeGVisor
}

// isDockerSbxRuntime returns true when the agent should run inside a Docker sbx
// microVM (KVM-based hypervisor isolation).
func isDockerSbxRuntime(workflowData *WorkflowData) bool {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Disabled {
		return false
	}
	return agentConfig.Runtime == AgentRuntimeDockerSbx
}

// isCloudHypervisorRuntime returns true when the agent should run inside a Cloud
// Hypervisor microVM (preview).
func isCloudHypervisorRuntime(workflowData *WorkflowData) bool {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Disabled {
		return false
	}
	return agentConfig.Runtime == AgentRuntimeCloudHypervisor
}

// declaresIgnoredFilesystemAllowWrite returns true when a workflow declares
// sandbox.agent.config.filesystem.allowWrite on a runtime where the compiler drops
// it (see awfEmitsFilesystemAllowWrite). Only explicit opt-ins are reported: the
// implicit defaults are seeded for the Cloud Hypervisor runtime alone.
func declaresIgnoredFilesystemAllowWrite(workflowData *WorkflowData) bool {
	if isCloudHypervisorRuntime(workflowData) {
		return false
	}
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Disabled || agentConfig.Config == nil || agentConfig.Config.Filesystem == nil {
		return false
	}
	return agentConfig.Config.Filesystem.AllowWrite != nil
}

// isRuntimeInstallEnabled returns true when runtime installation steps should be
// generated (the default). Returns false only when sandbox.agent.runtime-install is
// explicitly set to false AND a runtime (gVisor or docker-sbx) is configured.
// When no runtime is set, the field has no effect and true is returned.
func isRuntimeInstallEnabled(workflowData *WorkflowData) bool {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Disabled {
		return true
	}
	// Noop when the runtime does not provision anything on the runner.
	if !resolveSandboxRuntimeProfile(agentConfig).SupportsRuntimeInstall {
		return true
	}
	if agentConfig.RuntimeInstall != nil && !*agentConfig.RuntimeInstall {
		return false
	}
	return true
}

func isAWFNetworkIsolationEnabled(workflowData *WorkflowData) bool {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Disabled {
		return false
	}
	// Inline threat detection and evals run a standalone AWF with no MCP sidecars, so
	// there is nothing to attach to the isolated topology.
	if workflowData != nil && (workflowData.IsDetectionRun || workflowData.IsEvalsRun) {
		return false
	}
	profile := resolveSandboxRuntimeProfile(agentConfig)
	return profile.NetworkIsolation && !profile.SupportsHostAccess
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
	if engineID != string(constants.CopilotEngine) && engineID != string(constants.CodexEngine) {
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
	if engineID != string(constants.ClaudeEngine) {
		return
	}

	enableFirewallByDefaultForEngine(engineID, networkPermissions, sandboxConfig)
}

// enableFirewallByDefaultForPi enables firewall by default for Pi engine
// when network restrictions are present but no explicit firewall configuration exists
// and sandbox.agent is not explicitly set to false
//
// The firewall is enabled by default for Pi UNLESS:
// - allowed contains "*" (unrestricted network access)
// - sandbox.agent is explicitly set to false
func enableFirewallByDefaultForPi(engineID string, networkPermissions *NetworkPermissions, sandboxConfig *SandboxConfig) {
	// Only apply to pi engine
	if engineID != string(constants.PiEngine) {
		return
	}

	enableFirewallByDefaultForEngine(engineID, networkPermissions, sandboxConfig)
}

// enableFirewallByDefaultForGemini enables firewall by default for Gemini engine
// when network restrictions are present but no explicit firewall configuration exists
// and sandbox.agent is not explicitly set to false
//
// The firewall is enabled by default for Gemini UNLESS:
// - allowed contains "*" (unrestricted network access)
// - sandbox.agent is explicitly set to false
func enableFirewallByDefaultForGemini(engineID string, networkPermissions *NetworkPermissions, sandboxConfig *SandboxConfig) {
	// Only apply to gemini engine
	if engineID != string(constants.GeminiEngine) {
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

	// SRT has been removed, all sandboxes should use AWF now
	// This section is no longer needed

	// Check if firewall is already configured
	if networkPermissions.Firewall != nil {
		firewallLog.Print("Firewall already configured, skipping default enablement")
		return
	}

	// Check if allowed contains "*" (unrestricted network access)
	// If it does, do NOT enable the firewall by default
	if slices.Contains(networkPermissions.Allowed, "*") {
		firewallLog.Print("Wildcard '*' in allowed domains, skipping AWF auto-enablement")
		return
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
