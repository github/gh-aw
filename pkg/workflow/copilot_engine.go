// This file implements the GitHub Copilot CLI agentic engine.
//
// The Copilot engine is organized into focused modules:
//   - copilot_engine.go: Core engine interface and constructor
//   - copilot_engine_installation.go: Installation workflow generation
//   - copilot_engine_execution.go: Execution workflow and runtime configuration
//   - copilot_engine_tools.go: Tool permissions, arguments, and error patterns
//   - copilot_logs.go: Log parsing, metrics extraction, and log management
//   - copilot_mcp.go: MCP server configuration rendering
//   - copilot_srt.go: Sandbox Runtime (SRT) integration
//   - copilot_participant_steps.go: Copilot CLI participant steps
//
// This modular organization improves maintainability and makes it easier
// to locate and modify specific functionality.

package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotLog = logger.New("workflow:copilot_engine")

const logsFolder = "/tmp/gh-aw/sandbox/agent/logs/"

// CopilotEngine represents the GitHub Copilot CLI agentic engine.
// It provides integration with GitHub Copilot CLI for agentic workflows,
// including MCP server support, sandboxing (AWF/SRT), and tool permissions.
type CopilotEngine struct {
	BaseEngine
}

func NewCopilotEngine() *CopilotEngine {
	copilotLog.Print("Creating new Copilot engine instance")
	return &CopilotEngine{
		BaseEngine: BaseEngine{
			id:                     "copilot",
			displayName:            "GitHub Copilot CLI",
			description:            "Uses GitHub Copilot CLI with MCP server support",
			experimental:           false,
			supportsToolsAllowlist: true,
			supportsHTTPTransport:  true,  // Copilot CLI supports HTTP transport via MCP
			supportsMaxTurns:       false, // Copilot CLI does not support max-turns feature yet
			supportsWebFetch:       true,  // Copilot CLI has built-in web-fetch support
			supportsWebSearch:      false, // Copilot CLI does not have built-in web-search support
			supportsFirewall:       true,  // Copilot supports network firewalling via AWF
			supportsPlugins:        true,  // Copilot supports plugin installation
		},
	}
}

// GetDefaultDetectionModel returns the default model for threat detection
// Uses gpt-5.1-codex-mini as a cost-effective model for detection tasks (replacement for deprecated gpt-5-mini)
func (e *CopilotEngine) GetDefaultDetectionModel() string {
	return string(constants.DefaultCopilotDetectionModel)
}

// GetRequiredSecretNames returns the list of secrets required by the Copilot engine
// This includes COPILOT_GITHUB_TOKEN and optionally MCP_GATEWAY_API_KEY
func (e *CopilotEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	copilotLog.Print("Collecting required secrets for Copilot engine")
	secrets := []string{"COPILOT_GITHUB_TOKEN"}

	// Add MCP gateway API key if MCP servers are present (gateway is always started with MCP servers)
	if HasMCPServers(workflowData) {
		copilotLog.Print("Adding MCP_GATEWAY_API_KEY secret")
		secrets = append(secrets, "MCP_GATEWAY_API_KEY")
	}

	// Add GitHub token for GitHub MCP server if present
	if hasGitHubTool(workflowData.ParsedTools) {
		copilotLog.Print("Adding GITHUB_MCP_SERVER_TOKEN secret")
		secrets = append(secrets, "GITHUB_MCP_SERVER_TOKEN")
	}

	// Add HTTP MCP header secret names
	headerSecrets := collectHTTPMCPHeaderSecrets(workflowData.Tools)
	for varName := range headerSecrets {
		secrets = append(secrets, varName)
	}
	if len(headerSecrets) > 0 {
		copilotLog.Printf("Added %d HTTP MCP header secrets", len(headerSecrets))
	}

	// Add safe-inputs secret names
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		safeInputsSecrets := collectSafeInputsSecrets(workflowData.SafeInputs)
		for varName := range safeInputsSecrets {
			secrets = append(secrets, varName)
		}
		if len(safeInputsSecrets) > 0 {
			copilotLog.Printf("Added %d safe-inputs secrets", len(safeInputsSecrets))
		}
	}

	copilotLog.Printf("Total required secrets: %d", len(secrets))
	return secrets
}

// GetInstallationSteps is implemented in copilot_engine_installation.go

func (e *CopilotEngine) GetDeclaredOutputFiles() []string {
	// Session state files are copied to logs folder by GetFirewallLogsCollectionStep
	return []string{logsFolder}
}

// extractAddDirPaths extracts all directory paths from copilot args that follow --add-dir flags
func extractAddDirPaths(args []string) []string {
	var dirs []string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--add-dir" {
			dirs = append(dirs, args[i+1])
		}
	}
	return dirs
}

// GetExecutionSteps is implemented in copilot_engine_execution.go

// RenderMCPConfig is implemented in copilot_mcp.go

// ParseLogMetrics is implemented in copilot_logs.go

// extractToolCallSizes is implemented in copilot_logs.go

// processToolCalls is implemented in copilot_logs.go

// parseCopilotToolCallsWithSequence is implemented in copilot_logs.go

// GetLogParserScriptId is implemented in copilot_logs.go

// GetLogFileForParsing is implemented in copilot_logs.go

// GetFirewallLogsCollectionStep is implemented in copilot_logs.go

// GetSquidLogsSteps is implemented in copilot_logs.go

// GetCleanupStep is implemented in copilot_logs.go

// computeCopilotToolArguments is implemented in copilot_engine_tools.go

// generateCopilotToolArgumentsComment is implemented in copilot_engine_tools.go

// GetErrorPatterns is implemented in copilot_engine_tools.go

// generateAWFInstallationStep is implemented in copilot_engine_installation.go

// GenerateCopilotInstallerSteps creates GitHub Actions steps for installing Copilot CLI using the official installer script
// Parameters:
//   - version: The Copilot CLI version to install (e.g., "0.0.369" or "v0.0.369")
//   - stepName: The name to display for the install step (e.g., "Install GitHub Copilot CLI")
//
// Returns steps for installing Copilot CLI using the official install.sh script from the Copilot CLI repository.
// The script is downloaded from https://raw.githubusercontent.com/github/copilot-cli/main/install.sh
// and executed with the VERSION environment variable set.
//
// Security Implementation:
//  1. Downloads the official installer script from the Copilot CLI repository
//  2. Saves script to a temporary file before execution (not piped directly to bash)
//  3. Uses the official script which includes platform detection and error handling
//
// Version Handling:
// The VERSION environment variable is used by the install.sh script.
// The script automatically adds 'v' prefix if not present.
// Examples:
//   - VERSION=0.0.369 → downloads and installs v0.0.369
//   - VERSION=v0.0.369 → downloads and installs v0.0.369
//   - VERSION=1.2.3 → downloads and installs v1.2.3
