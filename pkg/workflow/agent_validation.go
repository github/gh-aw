// This file provides agent file and feature support validation.
//
// # Agent Validation
//
// This file validates agent-specific configuration and feature compatibility
// for agentic workflows. It ensures that:
//   - Custom agent files exist when specified
//   - Engine features are supported (HTTP transport, max-turns, web-search)
//   - Workflow triggers have appropriate security constraints
//
// # Validation Functions
//
//   - validateAgentFile() - Validates custom agent file exists
//   - validateHTTPTransportSupport() - Validates HTTP MCP compatibility with engine
//   - validateMaxTurnsSupport() - Validates max-turns feature support
//   - validateWebSearchSupport() - Validates web-search feature support (warning)
//   - validateWorkflowRunBranches() - Validates workflow_run has branch restrictions
//
// # Validation Patterns
//
// This file uses several patterns:
//   - File existence validation: Agent files
//   - Feature compatibility checks: Engine capabilities
//   - Security validation: workflow_run branch restrictions
//   - Warning vs error: Some validations warn instead of fail
//
// # Security Considerations
//
// The validateWorkflowRunBranches function enforces security best practices:
//   - In strict mode: Errors when workflow_run lacks branch restrictions
//   - In normal mode: Warns when workflow_run lacks branch restrictions
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates custom agent file configuration
//   - It checks engine feature compatibility
//   - It validates agent-specific requirements
//   - It enforces security constraints on triggers
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var agentValidationLog = logger.New("workflow:agent_validation")

// validateAgentFile validates that the custom agent file specified in imports exists
func (c *Compiler) validateAgentFile(workflowData *WorkflowData, markdownPath string) error {
	// Check if agent file is specified in imports
	if workflowData.AgentFile == "" {
		return nil // No agent file specified, no validation needed
	}

	agentPath := workflowData.AgentFile
	agentValidationLog.Printf("Validating agent file exists: %s", agentPath)

	var fullAgentPath string

	// Check if agentPath is already absolute
	if filepath.IsAbs(agentPath) {
		// Use the path as-is (for backward compatibility with tests)
		fullAgentPath = agentPath
	} else {
		// Agent file path is relative to repository root (e.g., ".github/agents/file.md")
		// Need to resolve it relative to the markdown file's directory
		markdownDir := filepath.Dir(markdownPath)
		// Navigate up from .github/workflows to repository root
		repoRoot := filepath.Join(markdownDir, "..", "..")
		fullAgentPath = filepath.Join(repoRoot, agentPath)
	}

	// Check if the file exists
	if _, err := os.Stat(fullAgentPath); err != nil {
		if os.IsNotExist(err) {
			return formatCompilerError(markdownPath, "error",
				fmt.Sprintf("agent file '%s' does not exist. Ensure the file exists in the repository and is properly imported.", agentPath), nil)
		}
		// Other error (permissions, etc.)
		return formatCompilerError(markdownPath, "error",
			fmt.Sprintf("failed to access agent file '%s': %v", agentPath, err), err)
	}

	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
			fmt.Sprintf("✓ Agent file exists: %s", agentPath)))
	}

	return nil
}

// validateHTTPTransportSupport validates that HTTP MCP servers are only used with engines that support HTTP transport
func (c *Compiler) validateHTTPTransportSupport(tools map[string]any, engine CodingAgentEngine) error {
	if engine.SupportsHTTPTransport() {
		// Engine supports HTTP transport, no validation needed
		return nil
	}

	// Engine doesn't support HTTP transport, check for HTTP MCP servers
	for toolName, toolConfig := range tools {
		if config, ok := toolConfig.(map[string]any); ok {
			if hasMcp, mcpType := hasMCPConfig(config); hasMcp && mcpType == "http" {
				return fmt.Errorf("tool '%s' uses HTTP transport which is not supported by engine '%s'. Only stdio transport is supported. Use a different engine (e.g., copilot) or change the tool to use stdio transport. Example:\ntools:\n  %s:\n    type: stdio\n    command: \"node server.js\"", toolName, engine.GetID(), toolName)
			}
		}
	}

	return nil
}

// validateMaxTurnsSupport validates that max-turns is only used with engines that support this feature
func (c *Compiler) validateMaxTurnsSupport(frontmatter map[string]any, engine CodingAgentEngine) error {
	// Check if max-turns is specified in the engine config
	engineSetting, engineConfig := c.ExtractEngineConfig(frontmatter)
	_ = engineSetting // Suppress unused variable warning

	hasMaxTurns := engineConfig != nil && engineConfig.MaxTurns != ""

	if !hasMaxTurns {
		// No max-turns specified, no validation needed
		return nil
	}

	// max-turns is specified, check if the engine supports it
	if !engine.SupportsMaxTurns() {
		return fmt.Errorf("max-turns not supported: engine '%s' does not support the max-turns feature. Use engine: copilot or remove max-turns from your configuration. Example:\nengine:\n  id: copilot\n  max-turns: 5", engine.GetID())
	}

	// Engine supports max-turns - additional validation could be added here if needed
	// For now, we rely on JSON schema validation for format checking

	return nil
}

// validateWebSearchSupport validates that web-search tool is only used with engines that support this feature
func (c *Compiler) validateWebSearchSupport(tools map[string]any, engine CodingAgentEngine) {
	// Check if web-search tool is requested
	_, hasWebSearch := tools["web-search"]

	if !hasWebSearch {
		// No web-search specified, no validation needed
		return
	}

	// web-search is specified, check if the engine supports it
	if !engine.SupportsWebSearch() {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Engine '%s' does not support the web-search tool. See https://github.github.com/gh-aw/guides/web-search/ for alternatives.", engine.GetID())))
		c.IncrementWarningCount()
	}
}

// validateWorkflowRunBranches validates that workflow_run triggers include branch restrictions
// This is a security best practice to avoid running on all branches
func (c *Compiler) validateWorkflowRunBranches(workflowData *WorkflowData, markdownPath string) error {
	if workflowData.On == "" {
		return nil
	}

	agentValidationLog.Print("Validating workflow_run triggers for branch restrictions")

	// Parse the On field as YAML to check for workflow_run
	// The On field is a YAML string that starts with "on:" key
	var parsedData map[string]any
	if err := yaml.Unmarshal([]byte(workflowData.On), &parsedData); err != nil {
		// If we can't parse the YAML, skip this validation
		agentValidationLog.Printf("Could not parse On field as YAML: %v", err)
		return nil
	}

	// Extract the actual "on" section from the parsed data
	onData, hasOn := parsedData["on"]
	if !hasOn {
		// No "on" key found, skip validation
		return nil
	}

	onMap, isMap := onData.(map[string]any)
	if !isMap {
		// "on" is not a map, skip validation
		return nil
	}

	// Check if workflow_run is present
	workflowRunVal, hasWorkflowRun := onMap["workflow_run"]
	if !hasWorkflowRun {
		// No workflow_run trigger, no validation needed
		return nil
	}

	// Check if workflow_run has branches field
	workflowRunMap, isMap := workflowRunVal.(map[string]any)
	if !isMap {
		// workflow_run is not a map (unusual), skip validation
		return nil
	}

	_, hasBranches := workflowRunMap["branches"]
	if hasBranches {
		// Has branch restrictions, validation passed
		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("✓ workflow_run trigger has branch restrictions"))
		}
		return nil
	}

	// workflow_run without branches - this is a warning or error depending on mode
	message := "workflow_run trigger should include branch restrictions for security and performance.\n\n" +
		"Without branch restrictions, the workflow will run for workflow runs on ALL branches,\n" +
		"which can cause unexpected behavior and security issues.\n\n" +
		"Suggested fix: Add branch restrictions to your workflow_run trigger:\n" +
		"on:\n" +
		"  workflow_run:\n" +
		"    workflows: [\"your-workflow\"]\n" +
		"    types: [completed]\n" +
		"    branches:\n" +
		"      - main\n" +
		"      - develop"

	if c.strictMode {
		// In strict mode, this is an error
		return formatCompilerError(markdownPath, "error", message, nil)
	}

	// In normal mode, this is a warning
	formattedWarning := formatCompilerMessage(markdownPath, "warning", message)
	fmt.Fprintln(os.Stderr, formattedWarning)
	c.IncrementWarningCount()

	return nil
}
