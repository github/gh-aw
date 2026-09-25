package workflow

import "strings"

// safe_outputs_transport_prompt.go renders the safe-output transport wording that is
// substituted into safe_outputs_prompt.md and mcp_cli_tools_with_safeoutputs_prompt.md.
//
// Engines with native MCP tool-calling (claude, codex, copilot, gemini, ...) can call
// safe-output tools directly, so the mounted `safeoutputs` CLI is an optional equivalent
// transport. Engines without MCP support (for example `pi`, EngineCapabilities.MCP:
// false) have no direct tool-call interface at all: for them the CLI is the only way to
// emit safe outputs, and describing it as optional leads agents to hunt for a command
// named after the tool (which never exists) and give up without emitting anything.

const (
	// safeOutputsTransportEnvVar is substituted into safe_outputs_prompt.md.
	safeOutputsTransportEnvVar = "GH_AW_SAFE_OUTPUTS_TRANSPORT"
	// safeOutputsCLITransportEnvVar is substituted into mcp_cli_tools_with_safeoutputs_prompt.md.
	safeOutputsCLITransportEnvVar = "GH_AW_SAFE_OUTPUTS_CLI_TRANSPORT"

	safeOutputsTransportMCPText = "Call the tool names listed in `<safe-output-tools>` directly; if a separate `<mcp-clis>` section says `safeoutputs` is available on `PATH`, you may use that CLI form instead."

	safeOutputsTransportCLIOnlyText = "This engine has no MCP tool-call interface, so the `safeoutputs` CLI on `PATH` is the ONLY way to invoke the tools listed in `<safe-output-tools>`: run `safeoutputs <tool_name> <json>` from bash (see the `<mcp-clis>` section). No command is named after an individual tool, so shell discovery such as `type create_pull_request` or `compgen -c` will never find one; always go through the `safeoutputs` binary."

	safeOutputsCLITransportMCPText = "For `safeoutputs`, call the tool names listed in `<safe-output-tools>` directly; the `safeoutputs` CLI commands above are an optional equivalent transport."

	safeOutputsCLITransportCLIOnlyText = "For `safeoutputs`, the CLI commands above are the ONLY transport: this engine has no MCP tool-call interface, so every tool listed in `<safe-output-tools>` must be invoked as `safeoutputs <tool_name> <json>` from bash. No command is named after an individual tool, so never conclude a safe-output tool is unavailable because shell discovery cannot find it."
)

// engineSupportsMCPToolCalls reports whether the workflow engine can call MCP tools
// directly. Unknown or unset engines are treated as MCP-capable, matching the default
// engine behaviour.
func engineSupportsMCPToolCalls(data *WorkflowData) bool {
	if data == nil || data.EngineConfig == nil || data.EngineConfig.ID == "" {
		return true
	}
	engine, err := GetGlobalEngineRegistry().GetEngine(strings.ToLower(data.EngineConfig.ID))
	if err != nil || engine == nil {
		return true
	}
	return engine.GetCapabilities().MCP
}

// safeOutputsTransportText returns the wording for safe_outputs_prompt.md.
func safeOutputsTransportText(data *WorkflowData) string {
	if engineSupportsMCPToolCalls(data) {
		return safeOutputsTransportMCPText
	}
	return safeOutputsTransportCLIOnlyText
}

// safeOutputsCLITransportText returns the wording for mcp_cli_tools_with_safeoutputs_prompt.md.
func safeOutputsCLITransportText(data *WorkflowData) string {
	if engineSupportsMCPToolCalls(data) {
		return safeOutputsCLITransportMCPText
	}
	return safeOutputsCLITransportCLIOnlyText
}
