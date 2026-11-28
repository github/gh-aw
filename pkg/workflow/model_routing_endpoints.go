package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// modelRoutingEngineEndpoints returns the AWF routing endpoints supported by the
// engine's runtime harness (matching AWF_MODEL_ROUTING_POLICIES in awf_model_routing.cjs).
func modelRoutingEngineEndpoints(engineName string) []string {
	switch strings.ToLower(engineName) {
	case "copilot":
		return []string{"/responses", "/chat/completions"}
	case "claude":
		return []string{"/v1/messages"}
	case "codex":
		return []string{"/responses"}
	case "pi":
		return []string{"/v1/messages", "/responses", "/chat/completions"}
	default:
		return nil
	}
}

// apiProxySupportsRoutingFallback reports whether the workflow's effective AWF
// release and apiProxy image support router fallback (AWF v0.29.21+). Unlike
// apiProxySupportsRoutingEndpoints, it can be called before ModelRouting is
// enabled during implicit default and authored model: auto eligibility checks.
func apiProxySupportsRoutingFallback(data *WorkflowData) bool {
	firewall := getFirewallConfig(data)
	if (firewall != nil && strings.EqualFold(strings.TrimSpace(firewall.Version), "latest")) ||
		!awfVersionAtLeast(firewall, constants.AWFModelRoutingFallbackMinVersion) {
		return false
	}
	if agentConfig := getAgentConfig(data); agentConfig != nil {
		if image := agentConfig.Images[awfImageRoleAPIProxy]; image != "" {
			tag, found := imageReferenceTag(image)
			return found && !strings.EqualFold(tag, "latest") &&
				versionAtLeast(tag, "", string(constants.AWFModelRoutingFallbackMinVersion))
		}
	}
	return true
}

// apiProxySupportsRoutingEndpoints reports whether both the effective AWF release
// and its apiProxy image support apiProxy.routing.endpoints (AWF v0.29.21+).
func apiProxySupportsRoutingEndpoints(data *WorkflowData) bool {
	if !isModelRoutingEnabled(data) {
		return false
	}
	firewall := getFirewallConfig(data)
	if (firewall != nil && strings.EqualFold(strings.TrimSpace(firewall.Version), "latest")) ||
		!awfVersionAtLeast(firewall, constants.AWFModelRoutingFallbackMinVersion) {
		return false
	}
	image := getSandboxAgentImages(data)[awfImageRoleAPIProxy]
	tag, found := imageReferenceTag(image)
	return found && !strings.EqualFold(tag, "latest") &&
		versionAtLeast(tag, "", string(constants.AWFModelRoutingFallbackMinVersion))
}
