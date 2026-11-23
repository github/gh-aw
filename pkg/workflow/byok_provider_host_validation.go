package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var byokProviderHostLog = logger.New("workflow:byok_provider_host_validation")

// byokProviderHostAllowlistGap reports whether an expression-valued
// engine.env.COPILOT_PROVIDER_BASE_URL will be blocked by the firewall.
// The compiler can only allow-list literal hosts. AWF v0.25.41+ resolves the runtime value
// and allows exactly that host. Older pinned AWF versions are always reported because the
// compiler cannot verify that any network.allowed entry covers a host known only at runtime.
func byokProviderHostAllowlistGap(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.EngineConfig == nil || !isFirewallEnabled(workflowData) {
		return false
	}
	baseURL := strings.TrimSpace(workflowData.EngineConfig.Env[constants.CopilotProviderBaseURL])
	if !strings.Contains(baseURL, "${{") {
		return false
	}
	if awfVersionAtLeast(getFirewallConfig(workflowData), constants.AWFCopilotProviderHostAllowlistMinVersion) {
		return false
	}
	byokProviderHostLog.Printf("Expression-valued %s with AWF older than %s; provider host cannot be verified",
		constants.CopilotProviderBaseURL, constants.AWFCopilotProviderHostAllowlistMinVersion)
	return true
}

// validateBYOKProviderHostAllowlist reports an expression-valued Copilot provider base URL
// whose host the pinned AWF version cannot add to the firewall allow-list.
func (c *Compiler) validateBYOKProviderHostAllowlist(workflowData *WorkflowData, markdownPath string) error {
	if !byokProviderHostAllowlistGap(workflowData) {
		return nil
	}
	msg := fmt.Sprintf("engine.env.%s is a GitHub Actions expression, so its host is resolved only at runtime. "+
		"AWF %s and later add exactly that host to the firewall allow-list, but the pinned AWF version is older, so the compiler cannot verify the host is allowed and the agent's model requests may be blocked. "+
		"Remove the sandbox.agent.version pin or set it to %s or later (recommended). "+
		"If you keep the older pin, use a literal URL for engine.env.%s so the compiler can allow its host; list only the exact provider host in network.allowed if needed.\n"+
		"✓ Example:\n"+
		"network:\n"+
		"  allowed:\n"+
		"    - defaults\n"+
		"    - my-gateway.openai.azure.com",
		constants.CopilotProviderBaseURL,
		constants.AWFCopilotProviderHostAllowlistMinVersion,
		constants.AWFCopilotProviderHostAllowlistMinVersion,
		constants.CopilotProviderBaseURL)
	err := errors.New(msg)
	return formatCompilerError(markdownPath, "error", err.Error(), err)
}
