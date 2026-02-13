// This file implements the GitHub Copilot SDK agentic engine.
//
// The Copilot SDK engine is an experimental alternative to the CLI-based CopilotEngine.
// Instead of constructing shell commands with --allow-tool flags, it generates a JSON
// config file and invokes a runner binary that uses the Copilot SDK (Go) programmatically.
//
// The SDK engine is organized into focused modules:
//   - copilot_sdk_engine.go: Core engine interface, constructor, and shared utilities
//   - copilot_sdk_engine_execution.go: Execution workflow generation (JSON config + runner invocation)
//   - copilot_sdk_engine_tools.go: Tool configuration mapping for SDK (AvailableTools/ExcludedTools)
//   - copilot_sdk_engine_installation.go: Installation steps (CLI + runner binary)
//
// Key differences from CopilotEngine:
//   - Generates JSON config instead of shell command flags
//   - Invokes copilot-runner binary instead of copilot CLI directly
//   - Tool permissions use AvailableTools/ExcludedTools arrays instead of --allow-tool flags
//   - MCP config is written to a config file for the runner to consume
//   - Structured JSON output from runner replaces log file parsing

package workflow

import (
	"encoding/json"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotSDKLog = logger.New("workflow:copilot_sdk_engine")

// CopilotSDKEngine represents the GitHub Copilot SDK agentic engine.
// It provides integration with GitHub Copilot via the Copilot SDK (Go),
// offering programmatic control over sessions, tools, and MCP servers.
type CopilotSDKEngine struct {
	BaseEngine
}

// NewCopilotSDKEngine creates a new CopilotSDKEngine instance.
// The engine is registered as experimental and uses the "copilot-sdk" identifier.
func NewCopilotSDKEngine() *CopilotSDKEngine {
	copilotSDKLog.Print("Creating new Copilot SDK engine instance")
	return &CopilotSDKEngine{
		BaseEngine: BaseEngine{
			id:                     "copilot-sdk",
			displayName:            "GitHub Copilot SDK",
			description:            "Uses GitHub Copilot SDK (Go) with programmatic control (experimental)",
			experimental:           true,
			supportsToolsAllowlist: true,
			supportsHTTPTransport:  true,  // SDK supports HTTP transport via MCP
			supportsMaxTurns:       true,  // SDK can control turns via session management
			supportsWebFetch:       true,  // Copilot CLI has built-in web-fetch support
			supportsWebSearch:      false, // Copilot CLI does not have built-in web-search support
			supportsFirewall:       true,  // Supports network firewalling via AWF
			supportsPlugins:        false, // Plugins work differently with SDK (not yet supported)
			supportsLLMGateway:     false, // Does not support LLM gateway
		},
	}
}

// GetDefaultDetectionModel returns the default model for threat detection.
// Uses the same detection model as the standard Copilot engine.
func (e *CopilotSDKEngine) GetDefaultDetectionModel() string {
	return string(constants.DefaultCopilotDetectionModel)
}

// GetRequiredSecretNames returns the list of secrets required by the Copilot SDK engine.
// This includes COPILOT_GITHUB_TOKEN and optionally MCP_GATEWAY_API_KEY.
func (e *CopilotSDKEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	copilotSDKLog.Print("Collecting required secrets for Copilot SDK engine")
	secrets := []string{"COPILOT_GITHUB_TOKEN"}

	// Add MCP gateway API key if MCP servers are present
	if HasMCPServers(workflowData) {
		copilotSDKLog.Print("Adding MCP_GATEWAY_API_KEY secret")
		secrets = append(secrets, "MCP_GATEWAY_API_KEY")
	}

	// Add GitHub token for GitHub MCP server if present
	if hasGitHubTool(workflowData.ParsedTools) {
		copilotSDKLog.Print("Adding GITHUB_MCP_SERVER_TOKEN secret")
		secrets = append(secrets, "GITHUB_MCP_SERVER_TOKEN")
	}

	// Add HTTP MCP header secret names
	headerSecrets := collectHTTPMCPHeaderSecrets(workflowData.Tools)
	for varName := range headerSecrets {
		secrets = append(secrets, varName)
	}
	if len(headerSecrets) > 0 {
		copilotSDKLog.Printf("Added %d HTTP MCP header secrets", len(headerSecrets))
	}

	// Add safe-inputs secret names
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		safeInputsSecrets := collectSafeInputsSecrets(workflowData.SafeInputs)
		for varName := range safeInputsSecrets {
			secrets = append(secrets, varName)
		}
		if len(safeInputsSecrets) > 0 {
			copilotSDKLog.Printf("Added %d safe-inputs secrets", len(safeInputsSecrets))
		}
	}

	copilotSDKLog.Printf("Total required secrets: %d", len(secrets))
	return secrets
}

// GetDeclaredOutputFiles returns the output files produced by the Copilot SDK engine.
// The runner binary writes structured output to the logs folder.
func (e *CopilotSDKEngine) GetDeclaredOutputFiles() []string {
	return []string{logsFolder}
}

// GetInstallationSteps is implemented in copilot_sdk_engine_installation.go

// GetExecutionSteps is implemented in copilot_sdk_engine_execution.go

// RenderMCPConfig generates MCP server configuration for the Copilot SDK engine.
// Like the CLI engine, the SDK engine writes MCP configuration to a JSON file on disk
// that the runner reads at startup. In a future iteration, the MCP config could be
// embedded directly in the runner's JSON config to avoid separate file I/O.
func (e *CopilotSDKEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) {
	copilotSDKLog.Printf("Rendering MCP config for Copilot SDK engine: mcpTools=%d", len(mcpTools))

	// For the SDK engine, we reuse the same MCP config rendering as the CLI engine.
	// The MCP config is still written to disk as JSON because the runner binary
	// reads it as part of its configuration. In a future iteration, this could be
	// embedded directly in the SDK runner config JSON.

	// Create the directory first
	yaml.WriteString("          mkdir -p /home/runner/.copilot\n")

	// Create unified renderer with Copilot-specific options
	createRenderer := func(isLast bool) *MCPConfigRendererUnified {
		return NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields: true,
			InlineArgs:           true,
			Format:               "json",
			IsLast:               isLast,
			ActionMode:           GetActionModeFromWorkflowData(workflowData),
		})
	}

	gatewayConfig := buildMCPGatewayConfig(workflowData)

	options := JSONMCPConfigOptions{
		ConfigPath:    "/home/runner/.copilot/mcp-config.json",
		GatewayConfig: gatewayConfig,
		Renderers: MCPToolRenderers{
			RenderGitHub: func(yaml *strings.Builder, githubTool any, isLast bool, workflowData *WorkflowData) {
				renderer := createRenderer(isLast)
				renderer.RenderGitHubMCP(yaml, githubTool, workflowData)
			},
			RenderPlaywright: func(yaml *strings.Builder, playwrightTool any, isLast bool) {
				renderer := createRenderer(isLast)
				renderer.RenderPlaywrightMCP(yaml, playwrightTool)
			},
			RenderSerena: func(yaml *strings.Builder, serenaTool any, isLast bool) {
				renderer := createRenderer(isLast)
				renderer.RenderSerenaMCP(yaml, serenaTool)
			},
			RenderCacheMemory: func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {
				// Cache-memory is not used for Copilot SDK (filtered out)
			},
			RenderAgenticWorkflows: func(yaml *strings.Builder, isLast bool) {
				renderer := createRenderer(isLast)
				renderer.RenderAgenticWorkflowsMCP(yaml)
			},
			RenderSafeOutputs: func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {
				renderer := createRenderer(isLast)
				renderer.RenderSafeOutputsMCP(yaml, workflowData)
			},
			RenderSafeInputs: func(yaml *strings.Builder, safeInputs *SafeInputsConfig, isLast bool) {
				renderer := createRenderer(isLast)
				renderer.RenderSafeInputsMCP(yaml, safeInputs, workflowData)
			},
			RenderWebFetch: func(yaml *strings.Builder, isLast bool) {
				renderMCPFetchServerConfig(yaml, "json", "              ", isLast, true)
			},
			RenderCustomMCPConfig: func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
				return e.renderSDKMCPConfigWithContext(yaml, toolName, toolConfig, isLast, workflowData)
			},
		},
		FilterTool: func(toolName string) bool {
			return toolName != "cache-memory"
		},
	}

	_ = RenderJSONMCPConfig(yaml, tools, mcpTools, workflowData, options)
}

// renderSDKMCPConfigWithContext generates custom MCP server configuration for the SDK engine.
func (e *CopilotSDKEngine) renderSDKMCPConfigWithContext(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool, workflowData *WorkflowData) error {
	copilotSDKLog.Printf("Rendering custom MCP config for tool: %s", toolName)

	rewriteLocalhost := workflowData != nil && (workflowData.SandboxConfig == nil ||
		workflowData.SandboxConfig.Agent == nil ||
		!workflowData.SandboxConfig.Agent.Disabled)

	renderer := MCPConfigRenderer{
		Format:                   "json",
		IndentLevel:              "                ",
		RequiresCopilotFields:    true,
		RewriteLocalhostToDocker: rewriteLocalhost,
	}

	yaml.WriteString("              \"" + toolName + "\": {\n")

	if err := renderSharedMCPConfig(yaml, toolName, toolConfig, renderer); err != nil {
		return err
	}

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}

	return nil
}

// ParseLogMetrics parses structured JSON output from the copilot-runner binary.
// The runner produces a structured JSON output file with metrics, so parsing is simpler
// than the CLI engine's log parsing. Falls back to the same JSONL/debug log parsing
// as the CLI engine if structured output is not available.
func (e *CopilotSDKEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	copilotSDKLog.Printf("Parsing log metrics for Copilot SDK engine")

	// Try to parse structured runner output first
	if metrics, success := parseRunnerOutput(logContent, verbose); success {
		copilotSDKLog.Printf("Successfully parsed structured runner output")
		return metrics
	}

	// Fall back to session JSONL parsing (same as CLI engine)
	copilotSDKLog.Printf("Falling back to session JSONL parsing")
	var metrics LogMetrics
	var totalTokenUsage int
	toolCallMap := make(map[string]*ToolCallInfo)
	var currentSequence []string
	turns := 0

	lines := strings.Split(logContent, "\n")
	foundSessionEntry := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" || !strings.HasPrefix(trimmedLine, "{") {
			continue
		}

		var entry SessionEntry
		if err := json.Unmarshal([]byte(trimmedLine), &entry); err != nil {
			continue
		}

		foundSessionEntry = true

		switch entry.Type {
		case "system":
			if verbose {
				copilotSDKLog.Printf("Found system init entry")
			}

		case "assistant":
			if entry.Message != nil {
				for _, content := range entry.Message.Content {
					if content.Type == "tool_use" {
						toolName := content.Name
						currentSequence = append(currentSequence, toolName)
						inputSize := 0
						if content.Input != nil {
							inputJSON, _ := json.Marshal(content.Input)
							inputSize = len(inputJSON)
						}
						if toolInfo, exists := toolCallMap[toolName]; exists {
							toolInfo.CallCount++
							if inputSize > toolInfo.MaxInputSize {
								toolInfo.MaxInputSize = inputSize
							}
						} else {
							toolCallMap[toolName] = &ToolCallInfo{
								Name:         toolName,
								CallCount:    1,
								MaxInputSize: inputSize,
							}
						}
					}
				}
			}

		case "user":
			if entry.Message != nil {
				for _, content := range entry.Message.Content {
					if content.Type == "tool_result" && content.ToolUseID != "" {
						outputSize := len(content.Content)
						for _, toolInfo := range toolCallMap {
							if outputSize > toolInfo.MaxOutputSize {
								toolInfo.MaxOutputSize = outputSize
								break
							}
						}
					}
				}
			}

		case "result":
			if entry.Usage != nil {
				totalTokenUsage = entry.Usage.InputTokens + entry.Usage.OutputTokens
				turns = entry.NumTurns
			}
		}
	}

	if !foundSessionEntry {
		copilotSDKLog.Printf("No session entries found in log content")
		return metrics
	}

	if len(currentSequence) > 0 {
		metrics.ToolSequences = append(metrics.ToolSequences, currentSequence)
	}

	FinalizeToolMetrics(FinalizeToolMetricsOptions{
		Metrics:         &metrics,
		ToolCallMap:     toolCallMap,
		CurrentSequence: currentSequence,
		Turns:           turns,
		TokenUsage:      totalTokenUsage,
	})

	return metrics
}

// parseRunnerOutput attempts to parse the structured JSON output from the copilot-runner binary.
// Returns true if successful, false if the format is not recognized.
func parseRunnerOutput(logContent string, verbose bool) (LogMetrics, bool) {
	var metrics LogMetrics

	// Look for the runner output JSON in the log content
	// The runner writes a JSON block prefixed with a marker
	const outputMarker = "COPILOT_RUNNER_OUTPUT:"
	markerIdx := strings.Index(logContent, outputMarker)
	if markerIdx == -1 {
		return metrics, false
	}

	jsonStart := markerIdx + len(outputMarker)
	jsonContent := strings.TrimSpace(logContent[jsonStart:])

	// Find the end of the JSON block (first newline after the JSON)
	if endIdx := strings.Index(jsonContent, "\n"); endIdx != -1 {
		jsonContent = jsonContent[:endIdx]
	}

	var output RunnerOutput
	if err := json.Unmarshal([]byte(jsonContent), &output); err != nil {
		if verbose {
			copilotSDKLog.Printf("Failed to parse runner output JSON: %v", err)
		}
		return metrics, false
	}

	metrics.TokenUsage = output.Metrics.TokenUsage
	metrics.Turns = output.Metrics.Turns
	metrics.EstimatedCost = output.Metrics.EstimatedCost

	// Convert tool calls
	toolCallMap := make(map[string]*ToolCallInfo)
	for _, tc := range output.Metrics.ToolCalls {
		toolCallMap[tc.Name] = &ToolCallInfo{
			Name:          tc.Name,
			CallCount:     tc.Count,
			MaxInputSize:  tc.MaxInputSize,
			MaxOutputSize: tc.MaxOutputSize,
		}
	}

	if len(output.Metrics.ToolSequences) > 0 {
		metrics.ToolSequences = output.Metrics.ToolSequences
	}

	FinalizeToolMetrics(FinalizeToolMetricsOptions{
		Metrics:     &metrics,
		ToolCallMap: toolCallMap,
		Turns:       output.Metrics.Turns,
		TokenUsage:  output.Metrics.TokenUsage,
	})

	return metrics, true
}

// RunnerOutput represents the structured JSON output from the copilot-runner binary.
type RunnerOutput struct {
	Success  bool          `json:"success"`
	Response string        `json:"response"`
	Metrics  RunnerMetrics `json:"metrics"`
	Errors   []string      `json:"errors"`
}

// RunnerMetrics contains metrics collected by the copilot-runner during execution.
type RunnerMetrics struct {
	TokenUsage    int              `json:"token_usage"`
	Turns         int              `json:"turns"`
	ToolCalls     []RunnerToolCall `json:"tool_calls"`
	ToolSequences [][]string       `json:"tool_sequences"`
	EstimatedCost float64          `json:"estimated_cost"`
	Duration      int              `json:"duration_seconds"`
}

// RunnerToolCall represents a tool call metric from the runner.
type RunnerToolCall struct {
	Name          string `json:"name"`
	Count         int    `json:"count"`
	MaxInputSize  int    `json:"max_input_size"`
	MaxOutputSize int    `json:"max_output_size"`
}

// GetLogParserScriptId returns the JavaScript script name for parsing Copilot SDK logs.
// Uses the same parser as the CLI engine since log formats are compatible.
func (e *CopilotSDKEngine) GetLogParserScriptId() string {
	return "parse_copilot_log"
}

// GetLogFileForParsing returns the log directory for Copilot SDK logs.
func (e *CopilotSDKEngine) GetLogFileForParsing() string {
	return logsFolder
}

// GetFirewallLogsCollectionStep returns steps for collecting firewall logs.
func (e *CopilotSDKEngine) GetFirewallLogsCollectionStep(workflowData *WorkflowData) []GitHubActionStep {
	var steps []GitHubActionStep

	// Copy session state files to logs folder (same as CLI engine)
	sessionCopyStep := generateCopilotSessionFileCopyStep()
	steps = append(steps, sessionCopyStep)

	return steps
}

// GetSquidLogsSteps returns the steps for uploading and parsing Squid logs.
func (e *CopilotSDKEngine) GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep {
	var steps []GitHubActionStep

	if isFirewallEnabled(workflowData) {
		copilotSDKLog.Printf("Adding Squid logs upload and parsing steps for workflow: %s", workflowData.Name)

		squidLogsUpload := generateSquidLogsUploadStep(workflowData.Name)
		steps = append(steps, squidLogsUpload)

		firewallLogParsing := generateFirewallLogParsingStep(workflowData.Name)
		steps = append(steps, firewallLogParsing)
	}

	return steps
}

// GetCleanupStep returns the post-execution cleanup step.
func (e *CopilotSDKEngine) GetCleanupStep(workflowData *WorkflowData) GitHubActionStep {
	return GitHubActionStep([]string{})
}
