// This file provides sandbox validation functions for agentic workflow compilation.
//
// This file contains domain-specific validation functions for sandbox configuration:
//   - validateMountsSyntax() - Validates container mount syntax
//   - validateSandboxConfig() - Validates complete sandbox configuration
//
// These validation functions are organized in a dedicated file following the validation
// architecture pattern where domain-specific validation belongs in domain validation files.
// See validation.go for the complete validation architecture documentation.

package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var sandboxValidationLog = logger.New("workflow:sandbox_validation")

var githubActionsExpressionPattern = regexp.MustCompile(`\$\{\{[\s\S]*\}\}`)
var mcpGatewayEnvNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// validateMountsSyntax validates that mount strings follow the correct syntax
// Expected format: "source:destination:mode" where mode is either "ro" or "rw"
func validateMountsSyntax(mounts []string) error {
	return validateMountEntries(mounts, func(i int, parts mountParts) {
		sandboxValidationLog.Printf("Validated mount %d: source=%s, dest=%s, mode=%s", i, parts.source, parts.dest, parts.mode)
	}, func(i int, mount string, parts mountParts, kind mountValidationKind) error {
		switch kind {
		case mountValidationFormatError:
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d]", i),
				mount,
				"mount syntax must follow 'source:destination:mode' format with exactly 3 colon-separated parts",
				fmt.Sprintf("Use the format 'source:destination:mode'.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"\n\nSee: %s", constants.DocsSandboxURL),
			)
		case mountValidationModeError:
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d].mode", i),
				parts.mode,
				"mount mode must be 'ro' (read-only) or 'rw' (read-write)",
				fmt.Sprintf("Change the mount mode to either 'ro' or 'rw'.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"  # read-only\n    - \"/host/path:/container/path:rw\"  # read-write\n\nSee: %s", constants.DocsSandboxURL),
			)
		case mountValidationEmptySource:
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d].source", i),
				mount,
				"source path cannot be empty",
				fmt.Sprintf("Provide a valid source path.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"\n\nSee: %s", constants.DocsSandboxURL),
			)
		case mountValidationEmptyDestination:
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d].destination", i),
				mount,
				"destination path cannot be empty",
				fmt.Sprintf("Provide a valid destination path.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"\n\nSee: %s", constants.DocsSandboxURL),
			)
		default:
			return NewValidationError(
				fmt.Sprintf("sandbox.mounts[%d]", i),
				mount,
				fmt.Sprintf("internal error: sandbox mount validation kind %d is not supported", kind),
				fmt.Sprintf("Expected one of: invalid-format, too-few-parts, too-many-parts, empty-host-path, empty-destination.\n\nExample:\nsandbox:\n  mounts:\n    - \"/host/path:/container/path:ro\"\n\nSee: %s", constants.DocsSandboxURL),
			)
		}
	})
}

// validateSandboxConfig validates the sandbox configuration
// Returns an error if the configuration is invalid
func validateSandboxConfig(workflowData *WorkflowData) error { //nolint:largefunc // Existing sandbox validation remains centralized.
	if workflowData == nil {
		return nil
	}

	if workflowData.SandboxConfig == nil {
		return nil // No sandbox config is valid
	}

	sandboxConfig := workflowData.SandboxConfig

	// Check if sandbox.agent: false was specified
	// This requires the "dangerously-disable-sandbox-agent" feature to be explicitly enabled.
	if sandboxConfig.Agent != nil && sandboxConfig.Agent.Disabled {
		flag := string(constants.DangerouslyDisableSandboxAgentFeatureFlag)
		value, found := getFeatureValueCaseInsensitive(workflowData.Features, flag)
		enabled, isBoolean := value.(bool)
		if !found || !isBoolean || !enabled {
			return NewValidationError(
				"sandbox.agent",
				"false",
				fmt.Sprintf("disabling the agent sandbox removes a trust boundary and requires 'features.%s: true'", flag),
				fmt.Sprintf("Explicitly enable the dangerous sandbox opt-out:\n\nfeatures:\n  %s: true\nsandbox:\n  agent: false\n\nSee: %s", flag, constants.DocsSandboxURL),
			)
		}
		sandboxValidationLog.Printf("sandbox.agent: false permitted by features.%s: true", flag)

		if workflowData.EngineConfig != nil &&
			workflowData.EngineConfig.ID == string(constants.CodexEngine) &&
			NewCodexEngine().ResolveLLMProvider(workflowData) == LLMProviderGitHub {
			return NewValidationError(
				"sandbox.agent",
				"false",
				"Codex with a copilot/* model requires the agent sandbox for BYOK inference routing",
				"Enable the agent sandbox or select a non-Copilot model.",
			)
		}
	}

	// Validate mounts syntax if specified in agent config
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil && len(agentConfig.Mounts) > 0 {
		if err := validateMountsSyntax(agentConfig.Mounts); err != nil {
			return err
		}
	}

	// Validate memory format if specified in agent config
	if agentConfig != nil && agentConfig.Memory != "" {
		if err := validateAgentMemoryLimit(agentConfig.Memory); err != nil {
			return err
		}
	}

	if agentConfig != nil && len(agentConfig.AllowHostPorts) > 0 {
		if err := validateAllowHostPorts(agentConfig.AllowHostPorts); err != nil {
			return err
		}
	}

	// Validate the digest-pinned AWF infrastructure image manifest if configured.
	if err := validateSandboxAgentImages(workflowData); err != nil {
		return err
	}

	// Validate the runtime profile and the properties that depend on it.
	if err := validateSandboxRuntimeProfile(workflowData, agentConfig); err != nil {
		return err
	}

	// Validate gVisor runtime compatibility
	if agentConfig != nil && agentConfig.Runtime == AgentRuntimeGVisor {
		// gVisor is incompatible with ARC/DinD topology: the runner has no access to the
		// DinD sidecar's daemon config or systemd, so runsc install + systemctl restart
		// cannot succeed.
		if isArcDindTopology(workflowData) {
			return NewValidationError(
				"sandbox.agent.runtime",
				string(AgentRuntimeGVisor),
				"gvisor is incompatible with runner.topology: arc-dind",
				"gVisor requires registering the runsc runtime with Docker via systemctl, which "+
					"is not possible on ARC DinD runners where the Docker daemon runs in a sidecar. "+
					"Remove sandbox.agent.runtime: gvisor or change runner.topology.",
			)
		}

		sandboxValidationLog.Print("gVisor runtime configured -- topology check passed")
	}

	// Validate docker-sbx runtime compatibility
	if agentConfig != nil && agentConfig.Runtime == AgentRuntimeDockerSbx {
		// docker-sbx is incompatible with ARC/DinD topology: sbx requires KVM which is
		// not available on ARC DinD runners that typically lack nested virtualisation.
		if isArcDindTopology(workflowData) {
			return NewValidationError(
				"sandbox.agent.runtime",
				string(AgentRuntimeDockerSbx),
				"docker-sbx is incompatible with runner.topology: arc-dind",
				"docker-sbx requires KVM (nested virtualisation) which is typically unavailable "+
					"on ARC DinD runners. Remove sandbox.agent.runtime: docker-sbx or change runner.topology.",
			)
		}

		firewallConfig := getFirewallConfig(workflowData)
		var configuredVersion string
		if firewallConfig != nil {
			configuredVersion = firewallConfig.Version
		}
		if !versionAtLeast(configuredVersion, string(constants.DefaultFirewallVersion), string(constants.AWFContainerRuntimeMinVersion)) {
			effectiveVersion := configuredVersion
			if effectiveVersion == "" {
				effectiveVersion = string(constants.DefaultFirewallVersion)
			}
			return NewValidationError(
				"sandbox.agent.runtime",
				string(AgentRuntimeDockerSbx),
				fmt.Sprintf("docker-sbx requires AWF %s or newer", constants.AWFContainerRuntimeMinVersion),
				fmt.Sprintf("docker-sbx emits 'awf --container-runtime sbx', which is only supported in AWF %s+.\n\nThe effective AWF version is %s. Set firewall.version or sandbox.agent.version to %s or newer.", constants.AWFContainerRuntimeMinVersion, effectiveVersion, constants.AWFContainerRuntimeMinVersion),
			)
		}

		sandboxValidationLog.Print("docker-sbx runtime configured -- topology and AWF version checks passed")
	}

	// Validate cloud-hypervisor runtime compatibility
	if agentConfig != nil && agentConfig.Runtime == AgentRuntimeCloudHypervisor {
		if isArcDindTopology(workflowData) {
			return NewValidationError(
				"sandbox.agent.runtime",
				string(AgentRuntimeCloudHypervisor),
				"cloud-hypervisor is incompatible with runner.topology: arc-dind",
				"cloud-hypervisor requires KVM and is only supported on GitHub-hosted Ubuntu x86_64 runners. "+
					"ARC DinD runners do not provide that runtime environment. Remove sandbox.agent.runtime: cloud-hypervisor or change runner.topology.",
			)
		}

		firewallConfig := getFirewallConfig(workflowData)
		var configuredVersion string
		if firewallConfig != nil {
			configuredVersion = firewallConfig.Version
		}
		if !versionAtLeast(configuredVersion, string(constants.DefaultFirewallVersion), string(constants.AWFCloudHypervisorMinVersion)) {
			effectiveVersion := configuredVersion
			if effectiveVersion == "" {
				effectiveVersion = string(constants.DefaultFirewallVersion)
			}
			return NewValidationError(
				"sandbox.agent.runtime",
				string(AgentRuntimeCloudHypervisor),
				fmt.Sprintf("cloud-hypervisor requires AWF %s or newer", constants.AWFCloudHypervisorMinVersion),
				fmt.Sprintf("cloud-hypervisor preview support is only available in AWF %s+.\n\nThe effective AWF version is %s. Set firewall.version or sandbox.agent.version to %s or newer.", constants.AWFCloudHypervisorMinVersion, effectiveVersion, constants.AWFCloudHypervisorMinVersion),
			)
		}

		// cloud-hypervisor rejects --difc-proxy-host: the CLI proxy sidecar (awmg-cli-proxy)
		// is intentionally not attached to the isolated topology, so gh-proxy mode
		// (and integrity-reactions, which implicitly enables it) has no route to the host.
		if isCliProxyNeeded(workflowData) {
			return NewValidationError(
				"sandbox.agent.runtime",
				string(AgentRuntimeCloudHypervisor),
				"cloud-hypervisor is incompatible with tools.github.mode: gh-proxy",
				"cloud-hypervisor does not attach the CLI proxy sidecar, and the AWF runtime rejects "+
					"--difc-proxy-host for this runtime. Remove tools.github.mode: gh-proxy and the "+
					"integrity-reactions feature, or change sandbox.agent.runtime.",
			)
		}

		// cloud-hypervisor rejects enclaves configuration outright.
		if len(workflowData.Enclaves) > 0 {
			return NewValidationError(
				"sandbox.agent.runtime",
				string(AgentRuntimeCloudHypervisor),
				"cloud-hypervisor is incompatible with enclaves",
				"cloud-hypervisor does not support the enclaves subsystem. Remove the enclaves "+
					"configuration, or change sandbox.agent.runtime.",
			)
		}

		sandboxValidationLog.Print("cloud-hypervisor runtime configured -- topology, AWF version, and feature compatibility checks passed")
	}

	// Validate config structure if provided (deprecated - was only for SRT)
	if sandboxConfig.Config != nil {
		// Config is no longer used - SRT removed
		return NewConfigurationError(
			"sandbox.config",
			"deprecated",
			"custom sandbox config is deprecated (was only for Sandbox Runtime which has been removed)",
			"Remove sandbox.config from your workflow. AWF (Agent Workflow Firewall) is the only supported sandbox and does not use this configuration.",
		)
	}

	// Validate MCP gateway port if configured
	if sandboxConfig.MCP != nil && sandboxConfig.MCP.Port != 0 {
		if err := validateIntRange(sandboxConfig.MCP.Port, constants.MinNetworkPort, constants.MaxNetworkPort, "sandbox.mcp.port"); err != nil {
			return err
		}
		sandboxValidationLog.Printf("Validated MCP gateway port: %d", sandboxConfig.MCP.Port)
		// Dynamic enclave delegation derives a private control-plane port by adding
		// enclaveDelegationControlPortOffset to this data-plane port (see
		// buildMCPGatewayContainerCommand/writeMCPGatewayExports). Reject configured
		// ports that would push the derived control port past the valid TCP range,
		// rather than silently emitting an unusable MCP_GATEWAY_DELEGATION_CONTROL_LISTEN
		// value at runtime.
		if enclaveDynamicRepositoryPolicyEnabled(workflowData) &&
			sandboxConfig.MCP.Port+enclaveDelegationControlPortOffset > constants.MaxNetworkPort {
			return NewConfigurationError(
				"sandbox.mcp.port",
				strconv.Itoa(sandboxConfig.MCP.Port),
				fmt.Sprintf("port must leave room for the dynamic enclave delegation control port (port+%d) within the valid TCP range (%d-%d)",
					enclaveDelegationControlPortOffset, constants.MinNetworkPort, constants.MaxNetworkPort),
				fmt.Sprintf("Choose a sandbox.mcp.port no greater than %d, or remove the custom port to use the default.",
					constants.MaxNetworkPort-enclaveDelegationControlPortOffset),
			)
		}
	}

	if sandboxConfig.MCP != nil {
		for _, name := range sliceutil.SortedKeys(sandboxConfig.MCP.Env) {
			if isReservedMCPGatewayEnvVar(name) {
				return NewValidationError(
					"sandbox.mcp.env."+name,
					name,
					"environment variable names in the GH_AW_MCP_GATEWAY_ namespace are reserved for internal transport",
					"Choose a different name that does not start with GH_AW_MCP_GATEWAY_. Example:\n\nsandbox:\n  mcp:\n    env:\n      API_TOKEN: value",
				)
			}
			if !mcpGatewayEnvNamePattern.MatchString(name) {
				return NewValidationError(
					"sandbox.mcp.env."+name,
					name,
					fmt.Sprintf("environment variable names should match %s", mcpGatewayEnvNamePattern),
					"Use uppercase letters, digits, and underscores, starting with a letter or underscore. Example:\n\nsandbox:\n  mcp:\n    env:\n      API_TOKEN: value",
				)
			}
		}
	}

	// Validate that if agent sandbox is enabled, MCP gateway is always enabled.
	// The MCP gateway is enabled when MCP servers are configured (tools that use MCP).
	// Note: Even if agent sandbox is disabled (sandbox.agent: false), the MCP gateway
	// must still be enabled. Agent sandbox and MCP gateway are now independent.
	if sandboxConfig.Agent != nil && !sandboxConfig.Agent.Disabled {
		if !HasMCPServers(workflowData) {
			return NewConfigurationError(
				"sandbox",
				"enabled without MCP servers",
				"agent sandbox requires MCP servers to be configured",
				"Add MCP tools to your workflow:\n\ntools:\n  github:\n    mode: remote\n  playwright: null\n\nOr disable the agent sandbox:\nsandbox:\n  agent: false",
			)
		}
		sandboxValidationLog.Print("Agent sandbox enabled with MCP gateway - validation passed")
	}

	return nil
}

// memoryLimitPattern matches valid memory limit strings.
// The value must start with a non-zero digit, optionally followed by more digits,
// and end with one of: b, k, m, g (case-insensitive). Leading zeros and bare-zero
// values (e.g. "0m") are rejected because AWF rejects them at startup.
// Examples of valid values: "512m", "2g", "1024k", "1b", "128M".
var memoryLimitPattern = regexp.MustCompile(`^[1-9][0-9]*[bkmgBKMG]$`)

// validateAgentMemoryLimit checks that a sandbox.agent.memory string has the correct format.
func validateAgentMemoryLimit(memory string) error {
	if !memoryLimitPattern.MatchString(memory) {
		return NewValidationError(
			"sandbox.agent.memory",
			memory,
			"memory value is not a valid limit. Expected a positive integer without leading zeros followed by a unit: b, k, m, or g (e.g. \"4g\", \"512m\")",
			fmt.Sprintf("Use a valid memory limit format:\n\nsandbox:\n  agent:\n    memory: 4g  # examples: 512m, 4g, 8g\n\nSee: %s", constants.DocsSandboxURL),
		)
	}
	return nil
}

func validateAllowHostPorts(ports []int) error {
	for _, port := range ports {
		if port < minPort || port > maxPort {
			return NewValidationError(
				"sandbox.agent.allow-host-ports",
				strconv.Itoa(port),
				"allow-host-ports value "+strconv.Itoa(port)+" is out of range",
				"Expected a TCP port between 1 and 65535.\n\nExample: allow-host-ports: [9000]",
			)
		}
		if service, dangerous := awfDangerousHostPorts[port]; dangerous {
			return NewValidationError(
				"sandbox.agent.allow-host-ports",
				strconv.Itoa(port),
				"allow-host-ports value "+strconv.Itoa(port)+" maps to blocked service "+service,
				fmt.Sprintf("Blocked service ports remain unreachable under allow-host-ports even with the %s runtime; expose the service via GitHub Actions services: with sandbox.agent.runtime: %s instead.\n\nExample:\n# Do not list blocked service ports under allow-host-ports\nsandbox:\n  agent:\n    runtime: %s\nservices:\n  db:\n    image: postgres\n    ports: [\"5432:5432\"]", AgentRuntimeDockerSudoIptables, AgentRuntimeDockerSudoIptables, AgentRuntimeDockerSudoIptables),
			)
		}
	}
	return nil
}

// validateSandboxRuntimeProfile validates sandbox.agent.runtime and the properties
// whose behavior depends on the selected runtime profile.
func validateSandboxRuntimeProfile(workflowData *WorkflowData, agentConfig *AgentSandboxConfig) error {
	if agentConfig == nil || agentConfig.Disabled {
		return nil
	}

	if !isSupportedAgentRuntime(agentConfig.Runtime) {
		return NewValidationError(
			"sandbox.agent.runtime",
			string(agentConfig.Runtime),
			"unsupported sandbox runtime: must be one of "+strings.Join(supportedAgentRuntimeNames(), ", "),
			fmt.Sprintf("Choose one of the supported runtime profiles:\n\nsandbox:\n  agent:\n    runtime: %s\n\nSee: %s", AgentRuntimeDocker, constants.DocsSandboxURL),
		)
	}

	profile := resolveSandboxRuntimeProfile(agentConfig)

	// runtime-install controls runner provisioning and is only meaningful for the
	// runtimes that the compiler provisions (gvisor and docker-sbx).
	if agentConfig.RuntimeInstall != nil && !profile.SupportsRuntimeInstall {
		return NewValidationError(
			"sandbox.agent.runtime-install",
			strconv.FormatBool(*agentConfig.RuntimeInstall),
			fmt.Sprintf("sandbox.agent.runtime-install is only supported with sandbox.agent.runtime: %s or %s (current runtime: %s)", AgentRuntimeGVisor, AgentRuntimeDockerSbx, profile.Runtime),
			fmt.Sprintf("Remove sandbox.agent.runtime-install, or select a runtime that the compiler provisions:\n\nsandbox:\n  agent:\n    runtime: %s\n    runtime-install: false\n\nSee: %s", AgentRuntimeGVisor, constants.DocsSandboxURL),
		)
	}

	// Host access (explicit host ports and automatic GitHub Actions services:
	// connectivity) requires the privileged iptables profile.
	if profile.SupportsHostAccess {
		return nil
	}

	if len(agentConfig.AllowHostPorts) > 0 {
		return NewValidationError(
			"sandbox.agent.allow-host-ports",
			joinPorts(agentConfig.AllowHostPorts),
			fmt.Sprintf("sandbox.agent.allow-host-ports requires sandbox.agent.runtime: %s (current runtime: %s)", AgentRuntimeDockerSudoIptables, profile.Runtime),
			fmt.Sprintf("Host access is only available in the %s runtime profile:\n\nsandbox:\n  agent:\n    runtime: %s\n    allow-host-ports: [9000]\n\nSee: %s", AgentRuntimeDockerSudoIptables, AgentRuntimeDockerSudoIptables, constants.DocsSandboxURL),
		)
	}

	if workflowData != nil && workflowData.ServicePortExpressions != "" {
		return NewValidationError(
			"services",
			workflowData.ServicePortExpressions,
			fmt.Sprintf("GitHub Actions services: with published ports require sandbox.agent.runtime: %s (current runtime: %s)", AgentRuntimeDockerSudoIptables, profile.Runtime),
			fmt.Sprintf("The agent can only reach service containers in the %s runtime profile:\n\nsandbox:\n  agent:\n    runtime: %s\n\nOr remove the port mappings from services: if the agent does not need to reach them.\n\nSee: %s", AgentRuntimeDockerSudoIptables, AgentRuntimeDockerSudoIptables, constants.DocsSandboxURL),
		)
	}

	return nil
}

func getFeatureValueCaseInsensitive(features map[string]any, flagName string) (any, bool) {
	if value, exists := features[flagName]; exists {
		return value, true
	}
	for key, value := range features {
		if strings.EqualFold(key, flagName) {
			return value, true
		}
	}
	return nil, false
}
