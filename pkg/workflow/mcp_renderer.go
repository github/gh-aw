// Package workflow provides YAML rendering for MCP server configurations.
//
// # MCP Configuration Renderer Module
//
// The renderer subsystem is split across focused files for maintainability:
//
//   - mcp_renderer.go         — Factory (NewMCPConfigRenderer), custom-tool switch handler
//     (HandleCustomMCPToolInSwitch), top-level JSON orchestrator (RenderJSONMCPConfig).
//   - mcp_renderer_types.go   — All struct and func-type definitions (MCPRendererOptions,
//     MCPConfigRendererUnified, RenderCustomMCPToolConfigHandler, MCPToolRenderers,
//     JSONMCPConfigOptions, GitHubMCPDockerOptions, GitHubMCPRemoteOptions).
//   - mcp_renderer_github.go  — GitHub MCP rendering: RenderGitHubMCP, renderGitHubTOML,
//     RenderGitHubMCPDockerConfig, RenderGitHubMCPRemoteConfig.
//   - mcp_renderer_builtin.go — Built-in MCP server renderers: SafeOutputs,
//     MCPScripts, AgenticWorkflows (JSON + TOML for each).
//   - mcp_renderer_guard.go   — Guard / access-control policy rendering:
//     renderGuardPoliciesJSON, renderGuardPoliciesToml.
//
// All files belong to package workflow — no import path changes required.
//
// Renderer architecture:
// The renderer uses the MCPConfigRendererUnified struct with MCPRendererOptions
// to configure engine-specific behaviors:
//   - IncludeCopilotFields: Add "type" and "tools" fields for Copilot
//   - InlineArgs: Render args inline (Copilot) vs multi-line (Claude/Custom)
//   - Format: "json" for JSON-like or "toml" for TOML-like output
//   - IsLast: Control trailing commas in rendered configuration
//
// Engine-specific rendering:
//   - Copilot: JSON format with "type" and "tools" fields, inline args
//   - Claude: JSON format without Copilot fields, multi-line args
//   - Codex: TOML format for MCP configuration
//   - Custom: Same as Claude (JSON, multi-line args)
//
// Example usage:
//
//	renderer := NewMCPConfigRenderer(MCPRendererOptions{
//	   IncludeCopilotFields: true,
//	   InlineArgs: true,
//	   Format: "json",
//	   IsLast: false,
//	})
//
// renderer.RenderGitHubMCP(yaml, githubTool, workflowData)
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpRendererLog = logger.New("workflow:mcp_renderer")

// safeMCPServerIDRE matches MCP server IDs that are safe to embed inside an unquoted bash heredoc.
// The pattern allows alphanumeric characters, hyphens, and underscores — the same characters used
// for built-in tool names and typical custom mcp-server keys.
var safeMCPServerIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// isSafeMCPServerID reports whether id can be safely embedded inside an unquoted bash heredoc
// without triggering shell expansion.
func isSafeMCPServerID(id string) bool {
	return id != "" && safeMCPServerIDRE.MatchString(id)
}

func durationStringToSeconds(durationValue string) (int, error) {
	parsedDuration, err := time.ParseDuration(durationValue)
	if err != nil {
		return 0, err
	}
	return int(parsedDuration.Round(time.Second) / time.Second), nil
}

// NewMCPConfigRenderer creates a new unified MCP config renderer with the specified options
func NewMCPConfigRenderer(opts MCPRendererOptions) *MCPConfigRendererUnified {
	mcpRendererLog.Printf("Creating MCP renderer: format=%s, copilot_fields=%t, inline_args=%t, is_last=%t",
		opts.Format, opts.IncludeCopilotFields, opts.InlineArgs, opts.IsLast)
	return &MCPConfigRendererUnified{
		options: opts,
	}
}

// HandleCustomMCPToolInSwitch processes custom MCP tools in the default case of a switch statement.
// This shared function extracts the common pattern used across all workflow engines.
//
// Parameters:
//   - yaml: The string builder for YAML output
//   - toolName: The name of the tool being processed
//   - tools: The tools map containing tool configurations (supports both expanded and non-expanded tools)
//   - isLast: Whether this is the last tool in the list
//   - renderFunc: Engine-specific function to render the MCP configuration
//
// Returns:
//   - bool: true if a custom MCP tool was handled, false otherwise
func HandleCustomMCPToolInSwitch(
	yaml *strings.Builder,
	toolName string,
	tools map[string]any,
	isLast bool,
	renderFunc RenderCustomMCPToolConfigHandler,
) bool {
	// Handle custom MCP tools (those with MCP-compatible type)
	if toolConfig, ok := tools[toolName].(map[string]any); ok {
		if hasMcp, _ := hasMCPConfig(toolConfig); hasMcp {
			if err := renderFunc(yaml, toolName, toolConfig, isLast); err != nil {
				fmt.Fprintf(os.Stderr, "Error generating custom MCP configuration for %s: %v\n", toolName, err)
			}
			return true
		}
	}
	return false
}

// RenderJSONMCPConfig renders MCP configuration in JSON format with the common mcpServers structure.
// This shared function extracts the duplicate pattern from Claude, Copilot, and Custom engines.
//
// Parameters:
//   - yaml: The string builder for YAML output
//   - tools: Map of tool configurations
//   - mcpTools: Ordered list of MCP tool names to render
//   - workflowData: Workflow configuration data
//   - options: JSON MCP config rendering options
func RenderJSONMCPConfig( //nolint:largefunc // Existing renderer keeps MCP JSON emission order stable for golden tests.
	yaml *strings.Builder,
	tools map[string]any,
	mcpTools []string,
	workflowData *WorkflowData,
	options JSONMCPConfigOptions,
) error {
	mcpRendererLog.Printf("Rendering JSON MCP config: %d tools", len(mcpTools))

	// Build the JSON configuration in a separate builder for validation
	var configBuilder strings.Builder
	configBuilder.WriteString("          {\n")
	configBuilder.WriteString("            \"mcpServers\": {\n")

	// Filter tools if needed (e.g., Copilot filters out cache-memory)
	var filteredTools []string
	for _, toolName := range mcpTools {
		if options.FilterTool != nil && !options.FilterTool(toolName) {
			mcpRendererLog.Printf("Filtering out MCP tool: %s", toolName)
			continue
		}
		filteredTools = append(filteredTools, toolName)
	}

	mcpRendererLog.Printf("Rendering %d MCP tools after filtering", len(filteredTools))

	// Process each MCP tool
	totalServers := len(filteredTools)
	serverCount := 0

	for _, toolName := range filteredTools {
		serverCount++
		isLast := serverCount == totalServers

		switch toolName {
		case "github":
			githubTool, _ := tools["github"].(map[string]any)
			options.Renderers.RenderGitHub(&configBuilder, githubTool, isLast, workflowData)
		case "cache-memory":
			options.Renderers.RenderCacheMemory(&configBuilder, isLast, workflowData)
		case "agentic-workflows":
			options.Renderers.RenderAgenticWorkflows(&configBuilder, isLast)
		case "safe-outputs":
			options.Renderers.RenderSafeOutputs(&configBuilder, isLast, workflowData)
		case "mcp-scripts":
			if options.Renderers.RenderMCPScripts != nil {
				options.Renderers.RenderMCPScripts(&configBuilder, workflowData.MCPScripts, isLast)
			}
		case enclaveMCPServerName:
			if options.Renderers.RenderEnclave != nil {
				options.Renderers.RenderEnclave(&configBuilder, workflowData, isLast)
			}
		default:
			// Handle custom MCP tools using shared helper
			HandleCustomMCPToolInSwitch(&configBuilder, toolName, tools, isLast, options.Renderers.RenderCustomMCPConfig)
		}
	}

	// Write config file footer - but don't add newline yet if we need to add gateway
	if options.GatewayConfig != nil {
		configBuilder.WriteString("            },\n")
		// Add gateway section (needed for gateway to process)
		// Per MCP Gateway Specification v1.0.0 section 4.2, use "${VARIABLE_NAME}" syntax for variable expressions
		configBuilder.WriteString("            \"gateway\": {\n")
		// Port as unquoted variable - shell expands to integer (e.g., 8080) for valid JSON
		fmt.Fprintf(&configBuilder, "              \"port\": $MCP_GATEWAY_PORT,\n")
		fmt.Fprintf(&configBuilder, "              \"domain\": \"%s\",\n", options.GatewayConfig.Domain)
		if len(options.GatewayConfig.AgentIDs) > 0 {
			agentIDs, err := json.Marshal(options.GatewayConfig.AgentIDs)
			if err != nil {
				return fmt.Errorf("failed to marshal gateway agent IDs: %w", err)
			}
			fmt.Fprintf(&configBuilder, "              \"agentIds\": %s", agentIDs)
			if len(options.GatewayConfig.AgentPolicies) > 0 {
				agentPolicies, err := json.Marshal(options.GatewayConfig.AgentPolicies)
				if err != nil {
					return fmt.Errorf("failed to marshal gateway agent policies: %w", err)
				}
				fmt.Fprintf(&configBuilder, ",\n              \"agentPolicies\": %s", agentPolicies)
			}
		} else {
			fmt.Fprintf(&configBuilder, "              \"agentId\": \"%s\"", options.GatewayConfig.AgentID)
		}

		// Add optional fields if specified (agentId always precedes them without a trailing comma)
		if options.GatewayConfig.PayloadDir != "" {
			fmt.Fprintf(&configBuilder, ",\n              \"payloadDir\": \"%s\"", options.GatewayConfig.PayloadDir)
		}
		if len(options.GatewayConfig.TrustedBots) > 0 {
			configBuilder.WriteString(",\n              \"trustedBots\": [")
			for i, bot := range options.GatewayConfig.TrustedBots {
				if i > 0 {
					configBuilder.WriteString(", ")
				}
				fmt.Fprintf(&configBuilder, "%q", bot)
			}
			configBuilder.WriteString("]")
		}
		if options.GatewayConfig.KeepaliveInterval != 0 {
			fmt.Fprintf(&configBuilder, ",\n              \"keepaliveInterval\": %d", options.GatewayConfig.KeepaliveInterval)
		}
		if options.GatewayConfig.SessionTimeout != "" {
			fmt.Fprintf(&configBuilder, ",\n              \"sessionTimeout\": %q", options.GatewayConfig.SessionTimeout)
		}
		if options.GatewayConfig.ToolTimeout != "" {
			toolTimeoutSeconds, err := durationStringToSeconds(options.GatewayConfig.ToolTimeout)
			if err != nil {
				return fmt.Errorf("failed to parse engine.mcp.tool-timeout %q for gateway.toolTimeout: %w", options.GatewayConfig.ToolTimeout, err)
			}
			fmt.Fprintf(&configBuilder, ",\n              \"toolTimeout\": %d", toolTimeoutSeconds)
		}
		// Always emit startupTimeout to override MCP Gateway's built-in 30-second default.
		// Without this field, the gateway evicts safeoutputs backends that start after 30s
		// and permanently caches zero tools, causing silent data loss. The gh-aw default is
		// 120 seconds (constants.DefaultMCPStartupTimeout); users can override via tools.startup-timeout.
		if options.GatewayConfig.StartupTimeout > 0 {
			fmt.Fprintf(&configBuilder, ",\n              \"startupTimeout\": %d", options.GatewayConfig.StartupTimeout)
		}
		// Emit forcePublicRepos: false when private-to-public-flows: allow is declared.
		// Only emitted when explicitly set to false; omitting the field lets the gateway default (true).
		// See MCP Gateway Specification Section 4.1.3.8.
		if options.GatewayConfig.ForcePublicRepos != nil && !*options.GatewayConfig.ForcePublicRepos {
			configBuilder.WriteString(",\n              \"forcePublicRepos\": false")
		}
		// Emit sinkVisibilityExemptServers when private-to-public-flows lists specific server IDs.
		// See MCP Gateway Specification Section 10.9.
		if len(options.GatewayConfig.SinkVisibilityExemptServers) > 0 {
			configBuilder.WriteString(",\n              \"sinkVisibilityExemptServers\": [")
			for i, serverID := range options.GatewayConfig.SinkVisibilityExemptServers {
				if i > 0 {
					configBuilder.WriteString(", ")
				}
				// Validate against safe-identifier pattern before writing into the heredoc.
				// The config is rendered inside an unquoted bash heredoc; unvalidated IDs
				// containing shell metacharacters (e.g. $(cmd), `cmd`) would be expanded.
				if !isSafeMCPServerID(serverID) {
					return fmt.Errorf("private-to-public-flows: server ID %q contains characters that are unsafe for shell heredoc emission; IDs must match [A-Za-z0-9_-]+. Example:\n\nfirewall:\n  private-to-public-flows:\n    allowed-server-ids:\n      - my-safe-server", serverID)
				}
				fmt.Fprintf(&configBuilder, "%q", serverID)
			}
			configBuilder.WriteString("]")
		}
		// When OTLP tracing is configured, add the opentelemetry section directly to the
		// gateway config. The endpoint is passed via the OTEL_EXPORTER_OTLP_ENDPOINT env var
		// (injected by injectOTLPConfig) so that secrets are never interpolated directly into
		// the run block (RGS-008 compliance). All four fields use ${VARIABLE_NAME} expressions
		// expanded by bash from workflow-level env vars.
		// Per MCP Gateway Specification §4.1.3.7 and the opentelemetryConfig schema.
		// Note: OTEL_EXPORTER_OTLP_HEADERS is passed as a container env var (not in the JSON
		// config) so that auth credentials are not embedded in the stdin JSON config pipe.
		if options.GatewayConfig.OTLPEndpoint != "" {
			configBuilder.WriteString(",\n              \"opentelemetry\": {\n")
			configBuilder.WriteString("                \"endpoint\": \"${OTEL_EXPORTER_OTLP_ENDPOINT}\",\n")
			configBuilder.WriteString("                \"traceId\": \"${GITHUB_AW_OTEL_TRACE_ID}\",\n")
			configBuilder.WriteString("                \"spanId\": \"${GITHUB_AW_OTEL_PARENT_SPAN_ID}\"\n")
			configBuilder.WriteString("              }")
		}
		configBuilder.WriteString("\n")
		configBuilder.WriteString("            }\n")
	} else {
		configBuilder.WriteString("            }\n")
	}

	configBuilder.WriteString("          }\n")

	// Get the generated configuration
	generatedConfig := configBuilder.String()

	delimiter := GenerateHeredocDelimiterFromContent("MCP_CONFIG", generatedConfig)
	// Resolve the node binary to its absolute path so the command is robust
	// against PATH modifications that may occur later in the workflow.
	yaml.WriteString("          GH_AW_NODE=$(which node 2>/dev/null || command -v node 2>/dev/null || echo node)\n")
	// Write the configuration to the YAML output
	yaml.WriteString("          cat << " + delimiter + " | \"$GH_AW_NODE\" \"${RUNNER_TEMP}/gh-aw/actions/start_mcp_gateway.cjs\"\n") //nolint:generatedyamlheredoc // Gateway stdin rendering remains to be migrated.
	yaml.WriteString(generatedConfig)
	yaml.WriteString("          " + delimiter + "\n")

	// Note: Post-EOF commands are no longer needed since we pipe directly to the gateway script
	return nil
}
