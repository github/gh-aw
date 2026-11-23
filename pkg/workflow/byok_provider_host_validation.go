package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var byokProviderHostLog = logger.New("workflow:byok_provider_host_validation")

// validateBYOKProviderHostAllowlist reports an expression-valued Copilot provider base URL
// whose host the pinned AWF version cannot add to the firewall allow-list.
func (c *Compiler) validateBYOKProviderHostAllowlist(workflowData *WorkflowData, markdownPath string) error {
	if workflowData == nil || awfVersionAtLeast(getFirewallConfig(workflowData), constants.AWFCopilotProviderHostAllowlistMinVersion) {
		return nil
	}
	target := ""
	if ResolveEngineID(workflowData) == "copilot" && isFirewallEnabled(workflowData) {
		env := make(map[string]string)
		applyEngineAndAgentEnv(env, workflowData, nil)
		if strings.Contains(strings.TrimSpace(env[constants.CopilotProviderBaseURL]), "${{") {
			target = "main agent"
		}
	}
	if target == "" && IsDetectionJobEnabled(workflowData.SafeOutputs) && !workflowData.SafeOutputs.ThreatDetection.EngineDisabled &&
		c.getThreatDetectionEngineID(workflowData) == "copilot" {
		detector := buildThreatDetectionWorkflowData(workflowData, "copilot")
		detectionConfig := resolveExternalDetectorEngineConfig(workflowData, "copilot")
		env := mergeThreatDetectionEngineEnv(workflowData, "copilot", detectionConfig.Env)
		if isFirewallEnabled(detector) && strings.Contains(strings.TrimSpace(env[constants.CopilotProviderBaseURL]), "${{") {
			target = "threat detection"
		}
	}
	if target == "" {
		return nil
	}
	byokProviderHostLog.Printf("Expression-valued %s for %s with AWF older than %s; provider host cannot be verified",
		constants.CopilotProviderBaseURL, target, constants.AWFCopilotProviderHostAllowlistMinVersion)
	msg := fmt.Sprintf("The effective Copilot %s %s is a GitHub Actions expression, so its host is resolved only at runtime. "+
		"AWF %s and later add exactly that host to the firewall allow-list, but the pinned AWF version is older, so the compiler cannot verify the host is allowed and the agent's model requests may be blocked. "+
		"Remove both sandbox.agent.version and sandbox.agent.checksums-sha256 to use the bundled default, or upgrade to %s or later with a matching trusted checksum when required. "+
		"Alternatively, use a literal URL for %s. Adding network.allowed entries alone does not bypass this check.\n"+
		"✓ Example (engine.env):\n"+
		"engine:\n"+
		"  env:\n"+
		"    %s: https://my-gateway.openai.azure.com/v1",
		target,
		constants.CopilotProviderBaseURL,
		constants.AWFCopilotProviderHostAllowlistMinVersion,
		constants.AWFCopilotProviderHostAllowlistMinVersion,
		constants.CopilotProviderBaseURL,
		constants.CopilotProviderBaseURL)
	err := errors.New(msg)
	return formatCompilerError(markdownPath, "error", err.Error(), err)
}
