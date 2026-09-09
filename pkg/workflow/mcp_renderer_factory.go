// Package workflow provides utility functions for MCP configuration processing
// and rendering.
//
// # MCP Rendering
//
// This file consolidates MCP infrastructure helpers: URL rewriting for Docker
// networking and shared rendering functions used across multiple engines
// (Claude, Gemini, Copilot, Codex).
//
// URL rewriting:
// When HTTP MCP servers run on the host machine but need to be accessed from
// within a Docker container (like the firewall container running the AI agent),
// localhost URLs must
// be rewritten to use host.docker.internal.
//
// Supported URL patterns:
//   - http://localhost:port → http://host.docker.internal:port
//   - https://localhost:port → https://host.docker.internal:port
//   - http://127.0.0.1:port → http://host.docker.internal:port
//   - https://127.0.0.1:port → https://host.docker.internal:port
//
// Related files:
//   - mcp_renderer.go: Uses URL rewriting for HTTP MCP servers
//   - safe_outputs.go: Safe outputs HTTP server configuration
//   - mcp_scripts.go: MCP Scripts HTTP server configuration
package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpRenderingLog = logger.New("workflow:mcp-rendering")

// rewriteLocalhostToDockerHost rewrites localhost URLs to use host.docker.internal
// This is necessary when MCP servers run on the host machine but are accessed from within
// a Docker container (e.g., when firewall/sandbox is enabled)
func rewriteLocalhostToDockerHost(url string) string {
	// Define the localhost patterns to replace and their docker equivalents
	// Each pattern is a (prefix, replacement) pair
	replacements := []struct {
		prefix      string
		replacement string
	}{
		{"http://localhost", "http://host.docker.internal"},
		{"https://localhost", "https://host.docker.internal"},
		{"http://127.0.0.1", "http://host.docker.internal"},
		{"https://127.0.0.1", "https://host.docker.internal"},
	}

	for _, r := range replacements {
		if strings.HasPrefix(url, r.prefix) {
			newURL := r.replacement + url[len(r.prefix):]
			mcpRenderingLog.Printf("Rewriting localhost URL for Docker access: %s -> %s", url, newURL)
			return newURL
		}
	}

	return url
}

// shouldRewriteLocalhostToDocker returns true when MCP server localhost URLs should be
// rewritten to host.docker.internal so that containerised AI agents can reach servers
// running on the host. Rewriting is enabled whenever the agent sandbox is active
// (i.e. sandbox.agent is not explicitly disabled).
func shouldRewriteLocalhostToDocker(workflowData *WorkflowData) bool {
	result := workflowData != nil && (workflowData.SandboxConfig == nil ||
		workflowData.SandboxConfig.Agent == nil ||
		!workflowData.SandboxConfig.Agent.Disabled)
	mcpRenderingLog.Printf("shouldRewriteLocalhostToDocker: %v (agent sandbox active)", result)
	return result
}

// noOpCacheMemoryRenderer is a no-op MCPToolRenderers.RenderCacheMemory function for engines
// that do not need an MCP server entry for cache-memory. Cache-memory is a simple file share
// accessible at /tmp/gh-aw/cache-memory/ and requires no MCP configuration.
func noOpCacheMemoryRenderer(_ *strings.Builder, _ bool, _ *WorkflowData) {}

// renderStandardJSONMCPConfigOptions holds configuration for renderStandardJSONMCPConfig.
type renderStandardJSONMCPConfigOptions struct {
	tools                map[string]any
	mcpTools             []string
	workflowData         *WorkflowData
	configPath           string
	includeCopilotFields bool
	inlineArgs           bool
	renderCustom         RenderCustomMCPToolConfigHandler
	filterTool           func(string) bool
}

// renderDefaultJSONMCPConfig is a convenience wrapper for renderStandardJSONMCPConfig used by
// simple JSON engines (Claude, Gemini) that share the standard
// renderCustomMCPConfigWrapperWithContext callback and differ only in their config path.
func renderDefaultJSONMCPConfig(
	sb *strings.Builder,
	tools map[string]any,
	mcpTools []string,
	workflowData *WorkflowData,
	configPath string,
) error {
	return renderStandardJSONMCPConfig(sb, renderStandardJSONMCPConfigOptions{
		tools:        tools,
		mcpTools:     mcpTools,
		workflowData: workflowData,
		configPath:   configPath,
		renderCustom: func(builder *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
			return renderCustomMCPConfigWrapperWithContext(builder, toolName, toolConfig, isLast, workflowData)
		},
	})
}

// renderStandardJSONMCPConfig is a shared helper for JSON MCP config rendering used by
// Claude, Gemini, Copilot, and Codex engines. It consolidates the repeated sequence of:
// buildMCPRendererFactory → buildMCPGatewayConfig → buildStandardJSONMCPRenderers → RenderJSONMCPConfig.
func renderStandardJSONMCPConfig(
	yaml *strings.Builder,
	opts renderStandardJSONMCPConfigOptions,
) error {
	tools := opts.tools
	mcpTools := opts.mcpTools
	workflowData := opts.workflowData
	configPath := opts.configPath
	includeCopilotFields := opts.includeCopilotFields
	inlineArgs := opts.inlineArgs
	renderCustom := opts.renderCustom
	filterTool := opts.filterTool
	mcpRenderingLog.Printf("Rendering standard JSON MCP config: config_path=%s tools=%d mcp_tools=%d", configPath, len(tools), len(mcpTools))
	createRenderer := buildMCPRendererFactory(workflowData, "json", includeCopilotFields, inlineArgs)

	// CLI-mounted servers are NOT excluded from the gateway config.
	// The gateway must start their Docker containers so that:
	//   1. The CLI manifest (saved by start_mcp_gateway.cjs) includes them.
	//   2. mount_mcp_as_cli.cjs can query their tool lists and create wrappers.
	// Exclusion from the agent's final MCP config happens inside each
	// convert_gateway_config_*.cjs script via GH_AW_MCP_CLI_SERVERS.

	return RenderJSONMCPConfig(yaml, tools, mcpTools, workflowData, JSONMCPConfigOptions{
		ConfigPath:    configPath,
		GatewayConfig: buildMCPGatewayConfig(workflowData),
		Renderers:     buildStandardJSONMCPRenderers(workflowData, createRenderer, renderCustom),
		FilterTool:    filterTool,
	})
}

// buildMCPRendererFactory creates a factory function for MCPConfigRendererUnified instances.
// The returned function accepts isLast as a parameter and creates a renderer with engine-specific
// options derived from the provided parameters and workflowData at call time.
func buildMCPRendererFactory(workflowData *WorkflowData, format string, includeCopilotFields, inlineArgs bool) func(bool) *MCPConfigRendererUnified {
	mcpRenderingLog.Printf("Building MCP renderer factory: format=%s, copilotFields=%t, inlineArgs=%t", format, includeCopilotFields, inlineArgs)
	return func(isLast bool) *MCPConfigRendererUnified {
		return NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields:   includeCopilotFields,
			InlineArgs:             inlineArgs,
			Format:                 format,
			IsLast:                 isLast,
			ActionMode:             GetActionModeFromWorkflowData(workflowData),
			WriteSinkGuardPolicies: deriveWriteSinkGuardPolicyFromWorkflow(workflowData),
			ContainerPinMappings:   workflowData.getContainerPinMappings(),
		})
	}
}

// buildStandardJSONMCPRenderers constructs MCPToolRenderers with the standard rendering callbacks
// shared across JSON-format engines (Claude, Gemini, Copilot, Codex gateway).
//
// All standard tool callbacks (GitHub, CacheMemory, AgenticWorkflows,
// SafeOutputs, MCPScripts) are wired to the corresponding unified renderer methods
// via createRenderer. Cache-memory is always a no-op for these engines.
//
// renderCustom is the engine-specific handler for custom MCP tool configuration entries.
func buildStandardJSONMCPRenderers(
	workflowData *WorkflowData,
	createRenderer func(bool) *MCPConfigRendererUnified,
	renderCustom RenderCustomMCPToolConfigHandler,
) MCPToolRenderers {
	mcpRenderingLog.Printf("Building standard JSON MCP renderers")
	return MCPToolRenderers{
		RenderGitHub: func(yaml *strings.Builder, githubTool map[string]any, isLast bool, workflowData *WorkflowData) {
			createRenderer(isLast).RenderGitHubMCP(yaml, githubTool, workflowData)
		},
		RenderCacheMemory: noOpCacheMemoryRenderer,
		RenderAgenticWorkflows: func(yaml *strings.Builder, isLast bool) {
			createRenderer(isLast).RenderAgenticWorkflowsMCP(yaml)
		},
		RenderSafeOutputs: func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {
			createRenderer(isLast).RenderSafeOutputsMCP(yaml, workflowData)
		},
		RenderMCPScripts: func(yaml *strings.Builder, mcpScripts *MCPScriptsConfig, isLast bool) {
			createRenderer(isLast).RenderMCPScriptsMCP(yaml, mcpScripts, workflowData)
		},
		RenderEnclave: func(yaml *strings.Builder, workflowData *WorkflowData, isLast bool) {
			writeEnclaveMCPJSON(yaml, workflowData, isLast)
		},
		RenderCustomMCPConfig: renderCustom,
	}
}
