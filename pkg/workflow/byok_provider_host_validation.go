package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var byokProviderHostLog = logger.New("workflow:byok_provider_host_validation")

// byokProviderHostAllowlistGap reports whether an expression-valued
// engine.env.COPILOT_PROVIDER_BASE_URL will be blocked by the firewall.
// The compiler can only allow-list literal hosts. AWF v0.25.41+ resolves the runtime value
// and allows exactly that host, so only older pinned AWF versions without a custom
// network.allowed domain are affected.
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
	if workflowData.NetworkPermissions != nil {
		for _, entry := range workflowData.NetworkPermissions.Allowed {
			entry = strings.TrimSpace(entry)
			if entry != "" && !strings.Contains(entry, "${{") && !isEcosystemIdentifier(entry) {
				byokProviderHostLog.Printf("Custom network.allowed entry %q may cover the provider host", entry)
				return false
			}
		}
	}
	return true
}

// validateBYOKProviderHostAllowlist reports an expression-valued Copilot provider base URL
// whose host the pinned AWF version cannot add to the firewall allow-list. Strict mode
// fails compilation; otherwise a warning is emitted to preserve existing workflows.
func (c *Compiler) validateBYOKProviderHostAllowlist(workflowData *WorkflowData, markdownPath string) error {
	if !byokProviderHostAllowlistGap(workflowData) {
		return nil
	}
	msg := fmt.Sprintf("engine.env.%s is a GitHub Actions expression, so its host is resolved only at runtime. "+
		"AWF %s and later add exactly that host to the firewall allow-list, but the pinned AWF version is older and the agent's model requests will be blocked. "+
		"Remove the sandbox.agent.version pin (or set it to %s or later), or add the exact provider host to network.allowed.\n"+
		"✓ Example:\n"+
		"network:\n"+
		"  allowed:\n"+
		"    - defaults\n"+
		"    - my-gateway.openai.azure.com",
		constants.CopilotProviderBaseURL,
		constants.AWFCopilotProviderHostAllowlistMinVersion,
		constants.AWFCopilotProviderHostAllowlistMinVersion)
	if c.strictMode {
		err := errors.New("strict mode: " + msg)
		return formatCompilerError(markdownPath, "error", err.Error(), err)
	}
	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", msg))
	c.IncrementWarningCount()
	return nil
}
