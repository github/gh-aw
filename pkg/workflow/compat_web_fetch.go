package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// needsCompatWebFetch distinguishes URL fetching from hosted web search. The
// latter does not imply that an engine can fetch arbitrary URLs.
// Like native fetch and SDK fetch, compat is an engine-local capability governed
// by AWF network policy, not a gateway backend governed by MCP agent policies.
func needsCompatWebFetch(data *WorkflowData) bool {
	if data == nil || !isCopilotToolValueEnabled(data.Tools, "web-fetch") {
		return false
	}
	switch ResolveEngineID(data) {
	case "claude":
		return claudeUsesCopilotInference(data)
	case "copilot", "":
		return !isCopilotSDKMode(data)
	case "codex", "gemini", "agy", "opencode":
		return false
	default:
		return true
	}
}

func compatWebFetchEnabled(data *WorkflowData) bool {
	if !needsCompatWebFetch(data) || !isFirewallEnabled(data) || isNVXRuntime(data) {
		return false
	}
	switch ResolveEngineID(data) {
	case "copilot", "", "claude", "pi":
		return true
	default:
		return false
	}
}

func validateCompatWebFetch(data *WorkflowData) error {
	if !needsCompatWebFetch(data) {
		return nil
	}
	if !isFirewallEnabled(data) {
		return fmt.Errorf("tools.web-fetch requires the agent firewall for engine %q: enable sandbox.agent or use an engine with native URL fetching", ResolveEngineID(data))
	}
	if isNVXRuntime(data) {
		return errors.New("tools.web-fetch compatibility fallback is unavailable with sandbox.agent.runtime: nvx: use the docker or cloud-hypervisor runtime, or configure a fetch MCP server explicitly")
	}
	if !compatWebFetchEnabled(data) {
		return fmt.Errorf("tools.web-fetch is unavailable for engine %q: its MCP consumer does not support the firewall-local compat server; configure a fetch MCP server explicitly or use engine: copilot", ResolveEngineID(data))
	}
	if _, exists := data.Tools["compat"]; exists {
		return errors.New("tools.compat is reserved when tools.web-fetch uses the compiler-managed compat MCP server; rename the custom server")
	}
	return nil
}

func generateCompatWebFetchSetup(yaml *strings.Builder, data *WorkflowData) {
	if !compatWebFetchEnabled(data) {
		return
	}
	// Install outside the agent, but in the read-only gh-aw tree mounted into AWF.
	// The MCP entry supplies NODE_PATH explicitly rather than relying on host deps.
	yaml.WriteString("      - name: Install compat WebFetch dependencies\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          mkdir -p \"${RUNNER_TEMP}/gh-aw/sandbox/agent\"\n")
	yaml.WriteString("          npm install --prefix \"${RUNNER_TEMP}/gh-aw/compat\" --no-save --package-lock=false --ignore-scripts --no-audit --no-fund undici@6.29.1\n")
}

const claudeArcCompatMCPConfigPath = constants.GhAwRootDirShell + "/sandbox/agent/compat-mcp-config.json"

func claudeCompatMCPConfigPath(data *WorkflowData) string {
	if isArcDindTopology(data) {
		return claudeArcCompatMCPConfigPath
	}
	return constants.TmpGhAwAgentDir + "compat-mcp-config.json"
}

func compatMCPRuntimeSetup(source, destination string) string {
	return fmt.Sprintf(`(%s "%s/compat_mcp_config.cjs" "%s" "%s")`,
		nodeRuntimeResolutionCommand, SetupActionDestinationShell, source, destination)
}
