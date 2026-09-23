// This file defines the sandbox runtime profiles.
//
// sandbox.agent.runtime is the single selector for the supported sandbox security
// and topology profiles. Each runtime resolves to exactly one profile that encodes
// the AWF security behavior (privileged vs rootless AWF, legacy iptables networking,
// host access) instead of exposing those implementation details as separate
// frontmatter fields.

package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
)

// sandboxRuntimeProfile describes the effective sandbox behavior for one runtime.
type sandboxRuntimeProfile struct {
	// Runtime is the canonical frontmatter value for this profile.
	Runtime AgentRuntime

	// NetworkIsolation enables AWF's topology-based egress isolation
	// (--network-isolation): MCP sidecars run as bridge containers attached to
	// AWF's internal network.
	NetworkIsolation bool

	// LegacySecurity selects the privileged AWF command together with the
	// --legacy-security / --enable-host-access flags (iptables-based networking).
	LegacySecurity bool

	// Rootless is true when AWF itself runs as the runner user (no sudo). It
	// controls whether the AWF binary is installed rootless and whether the
	// firewall log helper uses non-interactive sudo.
	Rootless bool

	// AWFCommand is the command prefix used to invoke AWF.
	AWFCommand string

	// SupportsHostAccess is true when sandbox.agent.allow-host-ports and
	// automatic connectivity to GitHub Actions services: are available.
	SupportsHostAccess bool
}

// sandboxRuntimeProfiles maps each supported runtime to its profile.
var sandboxRuntimeProfiles = map[AgentRuntime]sandboxRuntimeProfile{
	AgentRuntimeDocker: {
		Runtime:          AgentRuntimeDocker,
		NetworkIsolation: true,
		Rootless:         true,
		AWFCommand:       constants.AWFDefaultCommand.String(),
	},
	AgentRuntimeDockerSudoIptables: {
		Runtime:            AgentRuntimeDockerSudoIptables,
		NetworkIsolation:   true,
		LegacySecurity:     true,
		AWFCommand:         constants.AWFLegacySecurityCommand,
		SupportsHostAccess: true,
	},
	AgentRuntimeCloudHypervisor: {
		Runtime:          AgentRuntimeCloudHypervisor,
		NetworkIsolation: true,
		// Cloud Hypervisor needs host privileges to access KVM and configure the VM.
		// This is still strict security: the guest remains network-isolated and no
		// legacy-security or host-access flags are implied by the sudo prefix.
		AWFCommand: constants.AWFCloudHypervisorCommand,
	},
}

// supportedAgentRuntimes lists the runtime values accepted in frontmatter, in
// documentation order.
var supportedAgentRuntimes = []AgentRuntime{
	AgentRuntimeDocker,
	AgentRuntimeDockerSudoIptables,
	AgentRuntimeCloudHypervisor,
}

// supportedAgentRuntimeNames returns the supported runtime values as strings.
func supportedAgentRuntimeNames() []string {
	return sliceutil.Map(supportedAgentRuntimes, func(runtime AgentRuntime) string {
		return string(runtime)
	})
}

// isSupportedAgentRuntime reports whether the given runtime value is supported.
// An empty value is supported and resolves to the default docker profile.
func isSupportedAgentRuntime(runtime AgentRuntime) bool {
	if runtime == "" {
		return true
	}
	_, ok := sandboxRuntimeProfiles[runtime]
	return ok
}

// resolveSandboxRuntimeProfile returns the profile for an agent configuration.
// An unset (or unknown) runtime resolves to the default docker profile, which
// keeps the secure Docker default when runtime is omitted.
func resolveSandboxRuntimeProfile(agentConfig *AgentSandboxConfig) sandboxRuntimeProfile {
	if agentConfig == nil {
		return sandboxRuntimeProfiles[AgentRuntimeDocker]
	}
	if profile, ok := sandboxRuntimeProfiles[agentConfig.Runtime]; ok {
		return profile
	}
	return sandboxRuntimeProfiles[AgentRuntimeDocker]
}

// getSandboxRuntimeProfile returns the effective sandbox runtime profile for a workflow.
func getSandboxRuntimeProfile(workflowData *WorkflowData) sandboxRuntimeProfile {
	return resolveSandboxRuntimeProfile(getAgentConfig(workflowData))
}

// isLegacySecurityRuntime returns true when the workflow uses the
// docker-sudo-iptables profile (privileged AWF, iptables networking, host access).
func isLegacySecurityRuntime(workflowData *WorkflowData) bool {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || agentConfig.Disabled {
		return false
	}
	return resolveSandboxRuntimeProfile(agentConfig).LegacySecurity
}
