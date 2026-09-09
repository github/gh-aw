package workflow

import (
	"fmt"
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
)

func hasCodexFeaturesTable(config string) bool {
	for line := range strings.SplitSeq(config, "\n") {
		if strings.TrimSpace(line) == "[features]" {
			return true
		}
	}
	return false
}

func addCodexPluginConfig(config string) string {
	lines := strings.Split(config, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "[features]" {
			lines = append(lines[:i+1], append([]string{"plugins = false"}, lines[i+1:]...)...)
			return strings.Join(lines, "\n")
		}
	}
	return config
}

// RenderMCPConfig generates MCP server configuration for Codex
func (e *CodexEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error { //nolint:largefunc // Legacy Codex config rendering remains to be migrated.
	if codexMCPLog.Enabled() {
		codexMCPLog.Printf("Rendering MCP config for Codex: mcp_tools=%v, tool_count=%d", mcpTools, len(tools))
	}

	// Create unified renderer with Codex-specific options
	// Codex uses TOML format without Copilot-specific fields and multi-line args
	createRenderer := func(isLast bool) *MCPConfigRendererUnified {
		return NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields:   false, // Codex doesn't use "type" and "tools" fields
			InlineArgs:             false, // Codex uses multi-line args format
			Format:                 "toml",
			IsLast:                 isLast,
			ActionMode:             GetActionModeFromWorkflowData(workflowData),
			WriteSinkGuardPolicies: deriveWriteSinkGuardPolicyFromWorkflow(workflowData),
			ContainerPinMappings:   workflowData.getContainerPinMappings(),
		})
	}

	// Build the heredoc content into a temporary buffer so we can derive the
	// delimiter from a SHA-256 hash of the content before writing it to the YAML output.
	var mcpConfigContent strings.Builder

	// Add history configuration to disable persistence
	mcpConfigContent.WriteString("          [history]\n")
	mcpConfigContent.WriteString("          persistence = \"none\"\n")

	// Codex defaults metrics_exporter to a built-in Statsig OTLP exporter that phones
	// home to https://ab.chatgpt.com regardless of model-provider (see
	// codex-rs/config/src/types.rs OtelConfig::default). This is unrelated to model
	// inference routing (which correctly targets the AWF gateway for BYOK/GitHub
	// providers) and would otherwise require allow-listing an OpenAI telemetry domain
	// even for workflows that never talk to OpenAI directly. Disable it explicitly.
	mcpConfigContent.WriteString("          [otel]\n")
	mcpConfigContent.WriteString("          metrics_exporter = \"none\"\n")

	// Add shell environment policy to control which environment variables are passed through
	// This is a security feature to prevent accidental exposure of secrets
	e.renderShellEnvironmentPolicy(&mcpConfigContent, tools, mcpTools)

	// Expand neutral tools (like playwright: null) to include the copilot agent tools
	expandedTools := e.expandNeutralToolsToCodexToolsFromMap(tools)

	// Generate [mcp_servers] section
	for _, toolName := range mcpTools {
		renderer := createRenderer(false) // isLast is always false in TOML format
		switch toolName {
		case "github":
			githubTool, _ := expandedTools["github"].(map[string]any)
			renderer.RenderGitHubMCP(&mcpConfigContent, githubTool, workflowData)
		case "agentic-workflows":
			renderer.RenderAgenticWorkflowsMCP(&mcpConfigContent)
		case "safe-outputs":
			// Add safe-outputs MCP server if safe-outputs are configured
			hasSafeOutputs := workflowData != nil && workflowData.SafeOutputs != nil && HasSafeOutputsEnabled(workflowData.SafeOutputs)
			if hasSafeOutputs {
				renderer.RenderSafeOutputsMCP(&mcpConfigContent, workflowData)
			}
		case "mcp-scripts":
			// Add mcp-scripts MCP server if mcp-scripts are configured and feature flag is enabled
			hasMCPScripts := workflowData != nil && IsMCPScriptsEnabled(workflowData.MCPScripts)
			if hasMCPScripts {
				renderer.RenderMCPScriptsMCP(&mcpConfigContent, workflowData.MCPScripts, workflowData)
			}
		case enclaveMCPServerName:
			writeEnclaveMCPTOML(&mcpConfigContent, workflowData)
		default:
			// Handle custom MCP tools using shared helper (with adapter for isLast parameter)
			HandleCustomMCPToolInSwitch(&mcpConfigContent, toolName, expandedTools, false, func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
				return e.renderCodexMCPConfigWithContext(yaml, toolName, toolConfig, workflowData)
			})
		}
	}

	// Append custom config if provided
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Config != "" {
		mcpConfigContent.WriteString("          \n")
		mcpConfigContent.WriteString("          # Custom configuration\n")
		// Write the custom config line by line with proper indentation
		configLines := strings.SplitSeq(workflowData.EngineConfig.Config, "\n")
		for line := range configLines {
			if strings.TrimSpace(line) != "" {
				mcpConfigContent.WriteString("          " + line + "\n")
			} else {
				mcpConfigContent.WriteString("          \n")
			}
		}
	}

	// Derive the delimiter from the content so it is stable across builds.
	delimiter := GenerateHeredocDelimiterFromContent("MCP_CONFIG", mcpConfigContent.String())
	yaml.WriteString("          cat > \"${RUNNER_TEMP}/gh-aw/mcp-config/config.toml\" << " + delimiter + "\n") //nolint:generatedyamlheredoc // Legacy Codex config rendering remains to be migrated.
	yaml.WriteString(mcpConfigContent.String())

	// End the heredoc for config.toml
	yaml.WriteString("          " + delimiter + "\n")

	// Also generate JSON config for MCP gateway
	// Per MCP Gateway Specification v1.0.0 section 4.1, the gateway requires JSON input
	// This JSON config is used by the gateway, while the TOML config above is used by Codex
	yaml.WriteString("          \n")
	yaml.WriteString("          # Generate JSON config for MCP gateway\n")

	// Gateway uses JSON format without Copilot-specific fields and multi-line args
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

	// start_mcp_gateway.cjs converts the gateway output and writes Codex config to
	// ${RUNNER_TEMP}/gh-aw/mcp-config/config.toml. Codex reads config from
	// $CODEX_HOME/config.toml, so copy the converted config into writable CODEX_HOME
	// and prepend shell policy (converter output does not include this section).
	yaml.WriteString("          \n")
	yaml.WriteString("          # Sync converter output to writable CODEX_HOME for Codex\n")
	yaml.WriteString("          mkdir -p /tmp/gh-aw/mcp-config\n")

	// Build the shell-policy heredoc content into a temp buffer so the delimiter
	// can be derived from a SHA-256 hash of the content for build stability.
	var shellPolicyContent strings.Builder
	if isFirewallEnabled(workflowData) {
		e.renderOpenAIProxyProviderToml(&shellPolicyContent, "          ", workflowData)
	}
	hasCustomFeatures := workflowData != nil && workflowData.EngineConfig != nil && hasCodexFeaturesTable(workflowData.EngineConfig.Config)
	if (workflowData == nil || len(workflowData.Plugins) == 0) && !hasCustomFeatures {
		shellPolicyContent.WriteString("          [features]\n")
		shellPolicyContent.WriteString("          plugins = false\n")
	}
	e.renderShellEnvironmentPolicyToml(&shellPolicyContent, tools, mcpTools, "          ")
	shellPolicyDelimiter := GenerateHeredocDelimiterFromContent("CODEX_SHELL_POLICY", shellPolicyContent.String())
	yaml.WriteString("          cat > \"/tmp/gh-aw/mcp-config/config.toml\" << " + shellPolicyDelimiter + "\n") //nolint:generatedyamlheredoc // Legacy Codex policy rendering remains to be migrated.
	yaml.WriteString(shellPolicyContent.String())
	yaml.WriteString("          " + shellPolicyDelimiter + "\n")
	if isFirewallEnabled(workflowData) {
		e.renderAppendConvertedConfigWithoutOpenAIProxy(yaml)
	} else {
		yaml.WriteString("          cat \"${RUNNER_TEMP}/gh-aw/mcp-config/config.toml\" >> \"/tmp/gh-aw/mcp-config/config.toml\"\n")
	}
	if workflowData != nil && workflowData.EngineConfig != nil && strings.TrimSpace(workflowData.EngineConfig.Config) != "" {
		customConfig := workflowData.EngineConfig.Config
		if len(workflowData.Plugins) == 0 && hasCustomFeatures {
			customConfig = addCodexPluginConfig(customConfig)
		}
		customConfigDelimiter := GenerateHeredocDelimiterFromContent("CODEX_CUSTOM_CONFIG", customConfig)
		yaml.WriteString("          \n")
		yaml.WriteString("          # Append engine-level custom Codex config\n")
		yaml.WriteString("          cat >> \"/tmp/gh-aw/mcp-config/config.toml\" << " + customConfigDelimiter + "\n") //nolint:generatedyamlheredoc // Legacy custom config rendering remains to be migrated.
		yaml.WriteString(customConfig)
		if !strings.HasSuffix(customConfig, "\n") {
			yaml.WriteString("\n")
		}
		yaml.WriteString("          " + customConfigDelimiter + "\n")
	}
	yaml.WriteString("          chmod 600 \"/tmp/gh-aw/mcp-config/config.toml\"\n")
	yaml.WriteString("          mkdir -p \"${CODEX_HOME}\"\n")
	yaml.WriteString("          if [ \"/tmp/gh-aw/mcp-config/config.toml\" != \"${CODEX_HOME}/config.toml\" ]; then cp \"/tmp/gh-aw/mcp-config/config.toml\" \"${CODEX_HOME}/config.toml\"; fi\n")
	yaml.WriteString("          chmod 600 \"${CODEX_HOME}/config.toml\"\n")

	return nil
}

func (e *CodexEngine) renderOpenAIProxyProviderToml(yaml *strings.Builder, indent string, workflowData *WorkflowData) {
	yaml.WriteString("\n")
	yaml.WriteString(indent + "model_provider = \"" + codexOpenAIProxyProviderID + "\"\n")
	yaml.WriteString("\n")
	yaml.WriteString(indent + "[model_providers." + codexOpenAIProxyProviderID + "]\n")
	yaml.WriteString(indent + "name = \"" + codexOpenAIProxyProviderName + "\"\n")
	yaml.WriteString(indent + "base_url = \"" + e.getOpenAIProxyProviderBaseURL(workflowData) + "\"\n")
	yaml.WriteString(indent + "env_key = \"CODEX_API_KEY\"\n")
	yaml.WriteString(indent + "wire_api = \"responses\"\n")
	yaml.WriteString(indent + "requires_openai_auth = false\n")
	yaml.WriteString(indent + "supports_websockets = false\n")
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

func (e *CodexEngine) renderAppendConvertedConfigWithoutOpenAIProxy(yaml *strings.Builder) {
	yaml.WriteString("          awk '\n")
	yaml.WriteString("            BEGIN { skip_openai_proxy = 0 }\n")
	yaml.WriteString("            /^[[:space:]]*model_provider[[:space:]]*=/ { next }\n")
	yaml.WriteString("            /^\\[model_providers\\.openai-proxy\\][[:space:]]*$/ { skip_openai_proxy = 1; next }\n")
	yaml.WriteString("            /^\\[/ { skip_openai_proxy = 0 }\n")
	yaml.WriteString("            !skip_openai_proxy { print }\n")
	yaml.WriteString("          ' \"${RUNNER_TEMP}/gh-aw/mcp-config/config.toml\" >> \"/tmp/gh-aw/mcp-config/config.toml\"\n")
}

// renderCodexMCPConfigWithContext generates custom MCP server configuration for a single tool in codex workflow config.toml
// This version includes workflowData to determine if localhost URLs should be rewritten
func (e *CodexEngine) renderCodexMCPConfigWithContext(yaml *strings.Builder, toolName string, toolConfig map[string]any, workflowData *WorkflowData) error {
	// Determine if localhost URLs should be rewritten to host.docker.internal
	// This is needed when firewall is enabled (agent is not disabled)
	rewriteLocalhost := shouldRewriteLocalhostToDocker(workflowData)
	codexMCPLog.Printf("Rendering TOML MCP config for custom tool: %s (rewrite_localhost=%v)", toolName, rewriteLocalhost)

	yaml.WriteString("          \n")
	fmt.Fprintf(yaml, "          [mcp_servers.%s]\n", toolName)

	// Use the shared MCP config renderer with TOML format
	renderer := MCPConfigRenderer{
		IndentLevel:              "          ",
		Format:                   "toml",
		RewriteLocalhostToDocker: rewriteLocalhost,
		GuardPolicies:            deriveWriteSinkGuardPolicyFromWorkflow(workflowData),
		ContainerPinMappings:     workflowData.getContainerPinMappings(),
	}

	err := renderSharedMCPConfig(yaml, toolName, toolConfig, renderer)
	if err != nil {
		codexMCPLog.Printf("Failed to render TOML MCP config for tool %s: %v", toolName, err)
		return err
	}

	return nil
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
