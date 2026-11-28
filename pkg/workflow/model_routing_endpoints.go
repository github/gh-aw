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
