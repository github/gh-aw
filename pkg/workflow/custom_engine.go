package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var customEngineLog = logger.New("workflow:custom_engine")

// CustomEngine represents a custom agentic engine that executes user-defined GitHub Actions steps
type CustomEngine struct {
	BaseEngine
}

// NewCustomEngine creates a new CustomEngine instance
func NewCustomEngine() *CustomEngine {
	return &CustomEngine{
		BaseEngine: BaseEngine{
			id:                     "custom",
			displayName:            "Custom Steps",
			description:            "Executes user-defined GitHub Actions steps",
			experimental:           false,
			supportsToolsAllowlist: false,
			supportsHTTPTransport:  false,
			supportsMaxTurns:       true,  // Custom engine supports max-turns for consistency
			supportsWebFetch:       false, // Custom engine does not have built-in web-fetch support
			supportsWebSearch:      false, // Custom engine does not have built-in web-search support
		},
	}
}

// GetRequiredSecretNames returns empty for custom engine as secrets depend on user-defined steps
// Custom engine steps should explicitly reference the secrets they need
func (e *CustomEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	// Custom engine doesn't have predefined secrets
	// User-defined steps should explicitly reference secrets they need
	// MCP gateway API key is added if MCP servers are present
	secrets := []string{}

	if HasMCPServers(workflowData) {
		secrets = append(secrets, "MCP_GATEWAY_API_KEY")
	}

	return secrets
}

// GetInstallationSteps returns empty installation steps since custom engine doesn't need installation
func (e *CustomEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	return []GitHubActionStep{}
}

// GetExecutionSteps returns the GitHub Actions steps for executing custom steps
func (e *CustomEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	stepCount := 0
	if workflowData.EngineConfig != nil {
		stepCount = len(workflowData.EngineConfig.Steps)
	}
	customEngineLog.Printf("Building custom engine execution steps: workflow=%s, custom_steps=%d", workflowData.Name, stepCount)

	var steps []GitHubActionStep

	// Generate each custom step if they exist, with environment variables
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Steps) > 0 {
		for _, step := range workflowData.EngineConfig.Steps {
			// Create a copy of the step to avoid modifying the original
			stepCopy := make(map[string]any)
			for k, v := range step {
				stepCopy[k] = v
			}

			// Convert to typed step for action pinning
			typedStep, err := MapToStep(stepCopy)
			if err != nil {
				customEngineLog.Printf("Failed to convert step to typed step, skipping action pinning: %v", err)
				// Continue with original stepCopy without action pinning
				stepMap := stepCopy

				// Prepare environment variables to merge
				envVars := make(map[string]string)

				// Always add GH_AW_PROMPT for agentic workflows
				envVars["GH_AW_PROMPT"] = "/tmp/gh-aw/aw-prompts/prompt.txt"

				// Add GH_AW_MCP_CONFIG for MCP server configuration
				envVars["GH_AW_MCP_CONFIG"] = "/tmp/gh-aw/mcp-config/mcp-servers.json"

				// Add GH_AW_SAFE_OUTPUTS if safe-outputs feature is used
				applySafeOutputEnvToMap(envVars, workflowData)

				// Add GH_AW_MAX_TURNS if max-turns is configured
				if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
					envVars["GH_AW_MAX_TURNS"] = workflowData.EngineConfig.MaxTurns
				}

				// Add GH_AW_ARGS if args are configured
				if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Args) > 0 {
					// Join args with space separator for environment variable
					envVars["GH_AW_ARGS"] = strings.Join(workflowData.EngineConfig.Args, " ")
				}

				// Add custom environment variables from engine config
				if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
					for key, value := range workflowData.EngineConfig.Env {
						envVars[key] = value
					}
				}

				// Merge environment variables into the step
				if len(envVars) > 0 {
					if existingEnv, exists := stepMap["env"]; exists {
						// If step already has env section, merge them
						if envMap, ok := existingEnv.(map[string]any); ok {
							for key, value := range envVars {
								envMap[key] = value
							}
							stepMap["env"] = envMap
						} else {
							// If env is not a map, replace it with our combined env
							// Convert string map to any map for compatibility
							envAny := make(map[string]any)
							for k, v := range envVars {
								envAny[k] = v
							}
							stepMap["env"] = envAny
						}
					} else {
						// If no env section exists, add our env vars
						// Convert string map to any map for compatibility
						envAny := make(map[string]any)
						for k, v := range envVars {
							envAny[k] = v
						}
						stepMap["env"] = envAny
					}
				}

				stepYAML, err := e.convertStepToYAML(stepMap)
				if err != nil {
					// Log error but continue with other steps
					continue
				}

				// Split the step YAML into lines to create a GitHubActionStep
				stepLines := strings.Split(strings.TrimRight(stepYAML, "\n"), "\n")

				// Remove empty lines at the end
				for len(stepLines) > 0 && strings.TrimSpace(stepLines[len(stepLines)-1]) == "" {
					stepLines = stepLines[:len(stepLines)-1]
				}

				steps = append(steps, GitHubActionStep(stepLines))
				continue
			}

			// Apply action pinning using type-safe version
			pinnedStep := ApplyActionPinToTypedStep(typedStep, workflowData)

			// Convert pinned step back to map for environment variable merging
			stepMap := pinnedStep.ToMap()

			// Prepare environment variables to merge
			envVars := make(map[string]string)

			// Always add GH_AW_PROMPT for agentic workflows
			envVars["GH_AW_PROMPT"] = "/tmp/gh-aw/aw-prompts/prompt.txt"

			// Add GH_AW_MCP_CONFIG for MCP server configuration
			envVars["GH_AW_MCP_CONFIG"] = "/tmp/gh-aw/mcp-config/mcp-servers.json"

			// Add GH_AW_SAFE_OUTPUTS if safe-outputs feature is used
			applySafeOutputEnvToMap(envVars, workflowData)

			// Add GH_AW_MAX_TURNS if max-turns is configured
			if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
				envVars["GH_AW_MAX_TURNS"] = workflowData.EngineConfig.MaxTurns
			}

			// Add GH_AW_ARGS if args are configured
			if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Args) > 0 {
				// Join args with space separator for environment variable
				envVars["GH_AW_ARGS"] = strings.Join(workflowData.EngineConfig.Args, " ")
			}

			// Add custom environment variables from engine config
			if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
				for key, value := range workflowData.EngineConfig.Env {
					envVars[key] = value
				}
			}

			// Merge environment variables into the step
			if len(envVars) > 0 {
				if existingEnv, exists := stepMap["env"]; exists {
					// If step already has env section, merge them
					if envMap, ok := existingEnv.(map[string]any); ok {
						for key, value := range envVars {
							envMap[key] = value
						}
						stepMap["env"] = envMap
					} else {
						// If env is not a map, replace it with our combined env
						// Convert string map to any map for compatibility
						envAny := make(map[string]any)
						for k, v := range envVars {
							envAny[k] = v
						}
						stepMap["env"] = envAny
					}
				} else {
					// If no env section exists, add our env vars
					// Convert string map to any map for compatibility
					envAny := make(map[string]any)
					for k, v := range envVars {
						envAny[k] = v
					}
					stepMap["env"] = envAny
				}
			}

			stepYAML, err := e.convertStepToYAML(stepMap)
			if err != nil {
				// Log error but continue with other steps
				continue
			}

			// Split the step YAML into lines to create a GitHubActionStep
			stepLines := strings.Split(strings.TrimRight(stepYAML, "\n"), "\n")

			// Remove empty lines at the end
			for len(stepLines) > 0 && strings.TrimSpace(stepLines[len(stepLines)-1]) == "" {
				stepLines = stepLines[:len(stepLines)-1]
			}

			steps = append(steps, GitHubActionStep(stepLines))
		}
	}

	// Add a step to ensure the log file exists for consistency with other engines
	logStepLines := []string{
		"      - name: Ensure log file exists",
		"        run: |",
		"          echo \"Custom steps execution completed\" >> " + logFile,
		"          touch " + logFile,
	}
	steps = append(steps, GitHubActionStep(logStepLines))

	return steps
}

// RenderMCPConfig renders MCP configuration using unified renderer
func (e *CustomEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) {
	// Create unified renderer with Custom engine-specific options
	// Custom engine uses JSON format without Copilot-specific fields and multi-line args (like Claude)
	createRenderer := func(isLast bool) *MCPConfigRendererUnified {
		return NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields: false, // Custom engine doesn't use "type" and "tools" fields
			InlineArgs:           false, // Custom engine uses multi-line args format
			Format:               "json",
			IsLast:               isLast,
			ActionMode:           GetActionModeFromWorkflowData(workflowData),
		})
	}

	// Use shared JSON MCP config renderer with unified renderer methods
	_ = RenderJSONMCPConfig(yaml, tools, mcpTools, workflowData, JSONMCPConfigOptions{
		ConfigPath:    "/tmp/gh-aw/mcp-config/mcp-servers.json",
		GatewayConfig: buildMCPGatewayConfig(workflowData),
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
			RenderCacheMemory: e.renderCacheMemoryMCPConfig,
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
				renderMCPFetchServerConfig(yaml, "json", "              ", isLast, false)
			},
			RenderCustomMCPConfig: func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
				return e.renderCustomMCPConfigWithContext(yaml, toolName, toolConfig, isLast, workflowData)
			},
		},
	})
}

// renderCustomMCPConfigWithContext generates custom MCP server configuration using shared logic with workflow context
// This version includes workflowData to determine if localhost URLs should be rewritten
func (e *CustomEngine) renderCustomMCPConfigWithContext(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool, workflowData *WorkflowData) error {
	return renderCustomMCPConfigWrapperWithContext(yaml, toolName, toolConfig, isLast, workflowData)
}

// renderCacheMemoryMCPConfig generates the Memory MCP server configuration using shared logic
// Uses Docker-based @modelcontextprotocol/server-memory setup
// renderCacheMemoryMCPConfig handles cache-memory configuration without MCP server mounting
// Cache-memory is now a simple file share, not an MCP server
func (e *CustomEngine) renderCacheMemoryMCPConfig(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {
	// Cache-memory no longer uses MCP server mounting
	// The cache folder is available as a simple file share at /tmp/gh-aw/cache-memory/
	// The folder is created by the cache step and is accessible to all tools
	// No MCP configuration is needed for simple file access
}

// ParseLogMetrics implements basic log parsing for custom engine
// For custom engines, try both Claude and Codex parsing approaches to extract turn information
func (e *CustomEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	customEngineLog.Printf("Parsing custom engine log metrics: log_size=%d bytes", len(logContent))
	var metrics LogMetrics

	// First try Claude-style parsing to see if the logs are Claude-format
	registry := GetGlobalEngineRegistry()
	claudeEngine, err := registry.GetEngine("claude")
	if err == nil {
		claudeMetrics := claudeEngine.ParseLogMetrics(logContent, verbose)
		if claudeMetrics.Turns > 0 || claudeMetrics.TokenUsage > 0 || claudeMetrics.EstimatedCost > 0 {
			// Found structured data, use Claude parsing
			customEngineLog.Print("Using Claude-style parsing for custom engine logs")
			return claudeMetrics
		}
	}

	// Try Codex-style parsing if Claude didn't yield results
	codexEngine, err := registry.GetEngine("codex")
	if err == nil {
		codexMetrics := codexEngine.ParseLogMetrics(logContent, verbose)
		if codexMetrics.Turns > 0 || codexMetrics.TokenUsage > 0 {
			// Found some data, use Codex parsing
			customEngineLog.Print("Using Codex-style parsing for custom engine logs")
			return codexMetrics
		}
	}

	// Fall back to basic parsing if neither Claude nor Codex approaches work
	customEngineLog.Print("Using basic fallback parsing for custom engine logs")

	lines := strings.Split(logContent, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Custom engine continues with basic processing
	}

	// Note: Custom engine doesn't collect individual errors - this is handled
	// by the Claude/Codex parsers if their log formats are detected

	return metrics
}

// GetLogParserScriptId returns the JavaScript script name for parsing custom engine logs
func (e *CustomEngine) GetLogParserScriptId() string {
	return "parse_custom_log"
}
