// This file implements the GitHub Copilot SDK agentic engine.
//
// The Copilot SDK engine is a variant of the Copilot engine that uses the
// Copilot SDK Node.js client instead of directly invoking the copilot CLI.
// This approach starts the CLI in headless mode on a specific port and uses
// the copilot-client.js wrapper to communicate with it.
//
// Key differences from the standard Copilot engine:
//   - Starts Copilot CLI in headless mode on port 10002
//   - Uses copilot-client.js wrapper via Node.js
//   - Passes configuration via GH_AW_COPILOT_CONFIG environment variable
//   - Uses Docker internal host domain for MCP server connections
//
// This implementation follows the same module organization as copilot_engine.go.

package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotSDKLog = logger.New("workflow:copilot_sdk_engine")

// CopilotSDKEngine represents the GitHub Copilot SDK agentic engine
type CopilotSDKEngine struct {
	BaseEngine
}

// NewCopilotSDKEngine creates a new Copilot SDK engine instance
func NewCopilotSDKEngine() *CopilotSDKEngine {
	copilotSDKLog.Print("Creating new Copilot SDK engine instance")
	return &CopilotSDKEngine{
		BaseEngine: BaseEngine{
			id:                     "copilot-sdk",
			displayName:            "GitHub Copilot SDK",
			description:            "Uses GitHub Copilot SDK with headless mode",
			experimental:           true,
			supportsToolsAllowlist: true,
			supportsHTTPTransport:  true,
			supportsMaxTurns:       false,
			supportsWebFetch:       true,
			supportsWebSearch:      false,
			supportsFirewall:       false, // SDK mode doesn't use firewall/sandbox
			supportsPlugins:        false, // SDK mode doesn't support plugins yet
			supportsLLMGateway:     false,
		},
	}
}

// SupportsLLMGateway returns the LLM gateway port for Copilot SDK engine
func (e *CopilotSDKEngine) SupportsLLMGateway() int {
	return 10002 // Copilot SDK uses port 10002 for LLM gateway
}

// GetDefaultDetectionModel returns the default model for threat detection
func (e *CopilotSDKEngine) GetDefaultDetectionModel() string {
	return string(constants.DefaultCopilotDetectionModel)
}

// GetRequiredSecretNames returns the list of secrets required by the Copilot SDK engine
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

	copilotSDKLog.Printf("Total required secrets: %d", len(secrets))
	return secrets
}

// GetDeclaredOutputFiles returns the list of output files that may be produced
func (e *CopilotSDKEngine) GetDeclaredOutputFiles() []string {
	return []string{
		"/tmp/gh-aw/copilot-sdk/event-log.jsonl", // Event log from copilot-client
	}
}

// GetInstallationSteps returns the GitHub Actions steps for installing Copilot SDK
func (e *CopilotSDKEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	copilotSDKLog.Printf("Generating installation steps for Copilot SDK: workflow=%s", workflowData.Name)

	// Use the same installation steps as the standard Copilot engine
	// This will install the Copilot CLI and validate secrets
	copilotEngine := NewCopilotEngine()
	return copilotEngine.GetInstallationSteps(workflowData)
}

// GetExecutionSteps returns the GitHub Actions steps for executing Copilot SDK
func (e *CopilotSDKEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	copilotSDKLog.Printf("Generating execution steps for Copilot SDK: workflow=%s", workflowData.Name)

	var steps []GitHubActionStep

	// Step 1: Start Copilot CLI in headless mode on port 10002
	steps = append(steps, e.generateCopilotHeadlessStep())

	// Step 2: Prepare copilot-client configuration
	steps = append(steps, e.generateConfigurationStep(workflowData))

	// Step 3: Execute copilot-client.js
	steps = append(steps, e.generateClientExecutionStep(workflowData))

	return steps
}

// generateCopilotHeadlessStep creates a step to start Copilot CLI in headless mode
func (e *CopilotSDKEngine) generateCopilotHeadlessStep() GitHubActionStep {
	var stepLines []string
	stepLines = append(stepLines, "      - name: Start Copilot CLI in headless mode")
	stepLines = append(stepLines, "        run: |")
	stepLines = append(stepLines, "          # Start Copilot CLI in headless mode on port 10002")
	stepLines = append(stepLines, "          copilot --headless --port 10002 &")
	stepLines = append(stepLines, "          COPILOT_PID=$!")
	stepLines = append(stepLines, "          echo \"COPILOT_PID=${COPILOT_PID}\" >> $GITHUB_ENV")
	stepLines = append(stepLines, "          ")
	stepLines = append(stepLines, "          # Wait for Copilot to be ready")
	stepLines = append(stepLines, "          sleep 5")
	stepLines = append(stepLines, "          ")
	stepLines = append(stepLines, "          # Verify Copilot is running")
	stepLines = append(stepLines, "          if ! kill -0 ${COPILOT_PID} 2>/dev/null; then")
	stepLines = append(stepLines, "            echo \"::error::Copilot CLI failed to start\"")
	stepLines = append(stepLines, "            exit 1")
	stepLines = append(stepLines, "          fi")
	stepLines = append(stepLines, "          ")
	stepLines = append(stepLines, "          echo \"✓ Copilot CLI started in headless mode on port 10002\"")

	return GitHubActionStep(stepLines)
}

// generateConfigurationStep creates a step to prepare the copilot-client configuration
func (e *CopilotSDKEngine) generateConfigurationStep(workflowData *WorkflowData) GitHubActionStep {
	// Build the configuration JSON
	config := map[string]any{
		"cliUrl":       "http://host.docker.internal:10002", // Use Docker internal host with LLM gateway port
		"promptFile":   "/tmp/gh-aw/aw-prompts/prompt.txt",
		"eventLogFile": "/tmp/gh-aw/copilot-sdk/event-log.jsonl",
		"githubToken":  "${{ secrets.COPILOT_GITHUB_TOKEN }}",
		"logLevel":     "info",
	}

	// Add model if specified
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Model != "" {
		config["session"] = map[string]any{
			"model": workflowData.EngineConfig.Model,
		}
	}

	// Serialize configuration to JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		copilotSDKLog.Printf("Error marshaling config: %v", err)
		return []string{
			"      - name: Configure Copilot SDK client",
			"        run: |",
			fmt.Sprintf("          echo \"::error::Failed to marshal Copilot SDK configuration: %v\"", err),
			"          exit 1",
		}
	}

	var stepLines []string
	stepLines = append(stepLines, "      - name: Configure Copilot SDK client")
	stepLines = append(stepLines, "        run: |")
	stepLines = append(stepLines, "          # Create directory for event log")
	stepLines = append(stepLines, "          mkdir -p /tmp/gh-aw/copilot-sdk/")
	stepLines = append(stepLines, "          ")
	stepLines = append(stepLines, "          # Set configuration via environment variable")
	stepLines = append(stepLines, fmt.Sprintf("          echo 'GH_AW_COPILOT_CONFIG=%s' >> $GITHUB_ENV", string(configJSON)))

	return GitHubActionStep(stepLines)
}

// generateClientExecutionStep creates a step to execute copilot-client.js
func (e *CopilotSDKEngine) generateClientExecutionStep(workflowData *WorkflowData) GitHubActionStep {
	var stepLines []string
	stepLines = append(stepLines, "      - name: Execute Copilot SDK client")
	stepLines = append(stepLines, "        id: agentic_execution")
	stepLines = append(stepLines, "        run: |")
	stepLines = append(stepLines, "          # Execute copilot-client.js with Node.js")
	stepLines = append(stepLines, "          # Configuration is read from GH_AW_COPILOT_CONFIG environment variable")
	stepLines = append(stepLines, "          node /opt/gh-aw/copilot/copilot-client.js")
	stepLines = append(stepLines, "          ")
	stepLines = append(stepLines, "          # Check exit code")
	stepLines = append(stepLines, "          if [ $? -ne 0 ]; then")
	stepLines = append(stepLines, "            echo \"::error::Copilot SDK client execution failed\"")
	stepLines = append(stepLines, "            exit 1")
	stepLines = append(stepLines, "          fi")
	stepLines = append(stepLines, "          ")
	stepLines = append(stepLines, "          echo \"✓ Copilot SDK client execution completed\"")
	stepLines = append(stepLines, "        env:")
	stepLines = append(stepLines, "          GH_AW_COPILOT_CONFIG: ${{ env.GH_AW_COPILOT_CONFIG }}")

	return GitHubActionStep(stepLines)
}

// RenderMCPConfig renders MCP server configuration for Copilot SDK
// This uses the Docker internal host domain for server URLs
func (e *CopilotSDKEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) {
	copilotSDKLog.Print("Rendering MCP configuration for Copilot SDK")

	// Use the same MCP rendering as standard Copilot engine
	copilotEngine := NewCopilotEngine()

	// Create a temporary builder to capture the output
	var tempBuilder strings.Builder
	copilotEngine.RenderMCPConfig(&tempBuilder, tools, mcpTools, workflowData)

	// Replace localhost with Docker internal host domain
	config := tempBuilder.String()
	config = strings.ReplaceAll(config, "localhost", "host.docker.internal")
	config = strings.ReplaceAll(config, "127.0.0.1", "host.docker.internal")

	// Write to the output builder
	yaml.WriteString(config)
}

// ParseLogMetrics parses log metrics from the Copilot SDK engine output
func (e *CopilotSDKEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	copilotSDKLog.Print("Parsing log metrics for Copilot SDK")

	// For now, return minimal metrics
	// The event log is in JSONL format and could be parsed for detailed metrics
	var metrics LogMetrics

	return metrics
}

// GetLogParserScriptId returns the script ID for log parsing
func (e *CopilotSDKEngine) GetLogParserScriptId() string {
	return "parse-copilot-log"
}

// GetLogFileForParsing returns the path to the log file that should be parsed
func (e *CopilotSDKEngine) GetLogFileForParsing() string {
	return "/tmp/gh-aw/copilot-sdk/event-log.jsonl"
}
