package workflow

import (
	"net"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var codexMCPLog = logger.New("workflow:codex_mcp")

const (
	codexOpenAIProxyProviderID   = "openai-proxy"
	codexOpenAIProxyProviderName = "OpenAI AWF proxy"
	codexRunBlockIndent          = "          "
)

// writeIndentedCodexConfig adds the YAML run-block indentation to each custom
// config line. YAML block scalar parsing strips this common indentation before
// the shell runs, so the heredoc receives the original TOML content. Lines that
// contain only whitespace are normalized to empty lines.
func writeIndentedCodexConfig(yaml *strings.Builder, config string) {
	outputEndsWithNewline := false
	for _, line := range strings.SplitAfter(config, "\n") {
		if line == "" {
			continue
		}
		if strings.TrimSpace(line) == "" {
			yaml.WriteByte('\n')
			outputEndsWithNewline = true
			continue
		}
		yaml.WriteString(codexRunBlockIndent)
		yaml.WriteString(line)
		outputEndsWithNewline = strings.HasSuffix(line, "\n")
	}
	if config != "" && !outputEndsWithNewline {
		yaml.WriteByte('\n')
	}
}

// RenderMCPConfig generates MCP server configuration for Codex
func (e *CodexEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error {
	if codexMCPLog.Enabled() {
		codexMCPLog.Printf("Rendering MCP config for Codex: mcp_tools=%v, tool_count=%d", mcpTools, len(tools))
	}

	yaml.WriteString(codexRunBlockIndent + "export GH_AW_CODEX_CONFIG=\"${RUNNER_TEMP}/gh-aw/mcp-config/codex-config.json\"\n")
	yaml.WriteString(codexRunBlockIndent + "\n")
	yaml.WriteString(codexRunBlockIndent + "# Generate JSON config for MCP gateway\n")
	if err := renderStandardJSONMCPConfig(yaml, renderStandardJSONMCPConfigOptions{
		tools:        tools,
		mcpTools:     mcpTools,
		workflowData: workflowData,
		configPath:   constants.ShellMcpServersJsonPath,
		renderCustom: func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
			return e.renderCodexJSONMCPConfigWithContext(yaml, toolName, toolConfig, isLast, workflowData)
		},
	}); err != nil {
		return err
	}

	yaml.WriteString(codexRunBlockIndent + "\n")
	yaml.WriteString(codexRunBlockIndent + "# Sync converter output to writable CODEX_HOME for Codex\n")
	yaml.WriteString(codexRunBlockIndent + "mkdir -p \"${CODEX_HOME}\"\n")
	yaml.WriteString(codexRunBlockIndent + "if [ \"${RUNNER_TEMP}/gh-aw/mcp-config/config.toml\" != \"${CODEX_HOME}/config.toml\" ]; then cp \"${RUNNER_TEMP}/gh-aw/mcp-config/config.toml\" \"${CODEX_HOME}/config.toml\"; fi\n")
	yaml.WriteString(codexRunBlockIndent + "chmod 600 \"${CODEX_HOME}/config.toml\"\n")

	return nil
}

func (e *CodexEngine) getOpenAIProxyProviderBaseURL(workflowData *WorkflowData) string {
	// AWF exposes each provider on its own gateway port. GitHub-hosted inference
	// (copilot/ models) is only served on the Copilot port (10002); the shared
	// OpenAI/Responses gateway port (10000) answers with a 404 "OpenAI proxy not
	// configured" error when no OpenAI credentials are present.
	port := constants.CodexLLMGatewayPort
	if e.ResolveLLMProvider(workflowData) == LLMProviderGitHub {
		port = constants.CopilotLLMGatewayPort
	}
	return "http://" + net.JoinHostPort(constants.AWFAPIProxyContainerIP, strconv.Itoa(port))
}

// renderCodexJSONMCPConfigWithContext generates custom MCP server configuration in JSON format for gateway
// This is used to generate the JSON config file that the MCP gateway reads
func (e *CodexEngine) renderCodexJSONMCPConfigWithContext(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool, workflowData *WorkflowData) error {
	// Determine if localhost URLs should be rewritten to host.docker.internal
	rewriteLocalhost := shouldRewriteLocalhostToDocker(workflowData)
	codexMCPLog.Printf("Rendering JSON MCP config for gateway tool: %s (isLast=%v, rewrite_localhost=%v)", toolName, isLast, rewriteLocalhost)

	// Use the shared renderer with JSON format for gateway
	renderer := MCPConfigRenderer{
		Format:                   "json",
		IndentLevel:              "              ",
		RewriteLocalhostToDocker: rewriteLocalhost,
		GuardPolicies:            deriveWriteSinkGuardPolicyFromWorkflow(workflowData),
		ContainerPinMappings:     workflowData.getContainerPinMappings(),
	}

	yaml.WriteString("              \"" + toolName + "\": {\n")

	err := renderSharedMCPConfig(yaml, toolName, toolConfig, renderer)
	if err != nil {
		codexMCPLog.Printf("Failed to render JSON MCP config for tool %s: %v", toolName, err)
		return err
	}

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}

	return nil
}
