// This file provides agent file and feature support validation.
//
// # Agent Validation
//
// This file validates agent-specific configuration and feature compatibility
// for agentic workflows. It ensures that:
//   - Custom agent files exist when specified
//   - Engine features are supported (HTTP transport, max-turns, web-search, bare mode)
//   - Workflow triggers have appropriate security constraints
//
// # Validation Functions
//
//   - validateAgentFile() - Validates custom agent file exists
//   - validateMaxTurnsSupport() - Validates max-turns feature support
//   - validateMaxContinuationsSupport() - Validates max-continuations feature support
//   - validateMaxToolDenialsSupport() - Validates max-tool-denials support for Copilot SDK mode
//   - validateWebSearchSupport() - Validates web-search feature support (warning)
//   - validateBareModeSupport() - Validates bare mode feature support (warning)
//   - validateBashCommandAllowlistSupport() - Errors when restricted bash allowlist is unsupported
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
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

	// Validate path characters to prevent shell injection via crafted filenames.
	// Only alphanumeric characters, dots, underscores, hyphens, forward slashes,
	// and spaces are permitted. Shell metacharacters are rejected.
	if !agentFilePathRegex.MatchString(agentPath) {
		return formatCompilerError(markdownPath, "error",
			fmt.Sprintf("agent file path '%s' contains invalid characters. Only alphanumeric characters, dots, underscores, hyphens, forward slashes, and spaces are allowed.", agentPath), nil)
	}

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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(
			"✓ Agent file exists: "+agentPath))
	}

	return nil
}

// validateCapabilitySupport is a shared helper that checks whether an engine supports a
// required capability. It returns an error with a standard "not supported" message when
// the feature is requested but the engine capability is missing.
// Returns nil when featureSet is false (feature not requested) or when the engine supports it.
func validateCapabilitySupport(featureName string, featureSet bool, capabilitySupported bool, engineID string) error {
	if !featureSet {
		return nil
	}
	if !capabilitySupported {
		return fmt.Errorf("%s not supported: engine '%s' does not support the %s feature", featureName, engineID, featureName)
	}
	return nil
}

// validateMaxTurnsSupport validates that max-turns is only used with engines that support this feature
func (c *Compiler) validateMaxTurnsSupport(frontmatter map[string]any, engine CodingAgentEngine) error {
	// Check if max-turns is specified in the engine config
	_, engineConfig, _ := c.ExtractEngineConfig(frontmatter)

	hasMaxTurns := engineConfig != nil && engineConfig.MaxTurns != ""

	if hasMaxTurns {
		agentValidationLog.Printf("Validating max-turns support: engine=%s", engine.GetID())
	}
	return validateCapabilitySupport("max-turns", hasMaxTurns, engine.GetCapabilities().MaxTurns, engine.GetID())
}

// emitContextWindowSupportWarning warns when the resolved engine configuration,
// including imported engine settings, sets context-window for an unsupported engine.
func (c *Compiler) emitContextWindowSupportWarning(workflowData *WorkflowData) {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.ContextWindow == 0 {
		return
	}
	engine, err := c.getAgenticEngine(workflowData.AI)
	if err != nil {
		return
	}
	c.validateContextWindowSupport(workflowData.EngineConfig, engine)
}

// validateContextWindowSupport warns when context-window is used with an engine that cannot consume it.
func (c *Compiler) validateContextWindowSupport(engineConfig *EngineConfig, engine CodingAgentEngine) {
	if engineConfig == nil || engineConfig.ContextWindow == 0 || engine.GetCapabilities().ContextWindow {
		return
	}

	agentValidationLog.Printf("Engine %s does not support context-window, emitting warning", engine.GetID())
	fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf(
		"Engine '%s' does not support context-window (engine.context-window). The setting will be ignored.",
		engine.GetID())))
	c.IncrementWarningCount()
}

// validateMaxContinuationsSupport validates that max-continuations is only used with engines that support this feature
func (c *Compiler) validateMaxContinuationsSupport(frontmatter map[string]any, engine CodingAgentEngine) error {
	// Check if max-continuations is specified in the engine config
	_, engineConfig, _ := c.ExtractEngineConfig(frontmatter)

	hasMaxContinuations := engineConfig != nil && engineConfig.MaxContinuations != 0

	if hasMaxContinuations {
		agentValidationLog.Printf("Validating max-continuations support: engine=%s", engine.GetID())
	}
	return validateCapabilitySupport("max-continuations", hasMaxContinuations, engine.GetCapabilities().MaxContinuations, engine.GetID())
}

// validateMaxToolDenialsSupport validates that max-tool-denials is only used with
// the Copilot engine in Copilot SDK mode.
func (c *Compiler) validateMaxToolDenialsSupport(frontmatter map[string]any, engine CodingAgentEngine) error {
	_, engineConfig, _ := c.ExtractEngineConfig(frontmatter)

	if engineConfig == nil || engineConfig.MaxToolDenials == "" {
		return nil
	}

	agentValidationLog.Printf("Validating max-tool-denials support: engine=%s, maxToolDenials=%s, copilotSDK=%v",
		engine.GetID(), engineConfig.MaxToolDenials, engineConfig.CopilotSDK)

	if engine.GetID() != string(constants.CopilotEngine) {
		return fmt.Errorf("max-tool-denials not supported: engine '%s' does not support max-tool-denials (supported only with engine 'copilot' and engine.copilot-sdk: true)", engine.GetID())
	}

	if !engineConfig.CopilotSDK {
		return errors.New("max-tool-denials requires Copilot SDK mode: set engine.copilot-sdk: true when using max-tool-denials")
	}

	return nil
}

// validateUniversalLLMConsumerModel validates that universal consumer engines
// (behavior-defined engines using the universal-llm-consumer secret strategy)
// declare a provider-qualified engine.model.
func (c *Compiler) validateUniversalLLMConsumerModel(frontmatter map[string]any, engine CodingAgentEngine) error {
	behaviorEngine, ok := engine.(*BehaviorDefinedEngine)
	if !ok || !behaviorEngine.usesUniversalLLMConsumer() {
		return nil
	}

	_, engineConfig, model := c.ExtractEngineConfig(frontmatter)
	if engineConfig == nil || strings.TrimSpace(model) == "" {
		return fmt.Errorf("engine.model is required for engine '%s' and must use provider/model format (for example: copilot/gpt-5, anthropic/claude-sonnet-4, openai/gpt-4.1)", engine.GetID())
	}

	if _, err := resolveUniversalLLMBackendFromModel(model); err != nil {
		return fmt.Errorf("invalid engine.model for engine '%s': %w", engine.GetID(), err)
	}

	return nil
}

// validatePiEngineRequirements validates Pi's required tool configuration.
func (c *Compiler) validatePiEngineRequirements(tools *ToolsConfig, engine CodingAgentEngine) error {
	if engine.GetID() != "pi" {
		return nil
	}

	if tools == nil || tools.GitHub == nil ||
		(tools.GitHub.Mode != GitHubMCPModeGHProxy && tools.GitHub.Mode != GitHubMCPModeCLI) {
		return errors.New("engine 'pi' requires tools.github.mode: gh-proxy")
	}

	if !tools.CLIProxy {
		return errors.New("engine 'pi' requires tools.cli-proxy: true")
	}

	return nil
}

// validateWebSearchSupport validates that web-search tool is only used with engines that support this feature
func (c *Compiler) validateWebSearchSupport(frontmatter map[string]any, tools map[string]any, engine CodingAgentEngine) {
	// Check if web-search tool is requested
	_, hasWebSearch := tools["web-search"]

	if !hasWebSearch {
		// No web-search specified, no validation needed
		return
	}

	agentValidationLog.Printf("Validating web-search support for engine: %s", engine.GetID())

	// web-search is specified, check if the engine supports it
	if !engine.GetCapabilities().WebSearch {
		agentValidationLog.Printf("Engine %s does not natively support web-search tool, emitting warning", engine.GetID())
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Engine '%s' does not support the web-search tool. See https://github.github.com/gh-aw/guides/web-search/ for alternatives.", engine.GetID())))
		c.IncrementWarningCount()
		return
	}

	if engine.GetID() == string(constants.CopilotEngine) {
		_, engineConfig, _ := c.ExtractEngineConfig(frontmatter)
		if !copilotSupportsWebSearch(engineConfig) {
			version := ""
			if engineConfig != nil {
				version = engineConfig.Version
			}
			agentValidationLog.Printf("Copilot CLI version %s does not support web-search tool, emitting warning", version)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf(
				"Copilot CLI versions before %s do not support the web-search tool. Upgrade engine.version or omit it to use the default Copilot CLI version.",
				constants.CopilotWebSearchMinVersion)))
			c.IncrementWarningCount()
		}
	}
}

// validateBareModeSupport validates that bare mode is only used with engines that support this feature.
// Emits a warning and has no effect on engines that do not support bare mode.
func (c *Compiler) validateBareModeSupport(frontmatter map[string]any, engine CodingAgentEngine) {
	_, engineConfig, _ := c.ExtractEngineConfig(frontmatter)

	if engineConfig == nil || !engineConfig.Bare {
		// bare mode not requested, no validation needed
		return
	}

	agentValidationLog.Printf("Validating bare mode support for engine: %s", engine.GetID())

	if !engine.GetCapabilities().BareMode {
		agentValidationLog.Printf("Engine %s does not support bare mode, emitting warning", engine.GetID())
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Engine '%s' does not support bare mode (engine.bare: true). Bare mode is only supported for the 'copilot' and 'claude' engines. The setting will be ignored.", engine.GetID())))
		c.IncrementWarningCount()
	}
}

// validateBashCommandAllowlistSupport errors when an explicit bash restriction is used
// with an engine that cannot enforce it. An explicit restriction is any of:
//   - bash: false  (disabling bash — silently ignored at runtime)
//   - bash: []     (empty allowlist — silently ignored at runtime)
//   - bash: [cmd1, cmd2, ...]  (non-wildcard list — silently ignored at runtime)
//
// Engines that do not map these configurations to their own CLI syntax silently ignore them,
// creating the dangerous illusion of restriction where none exists.
func (c *Compiler) validateBashCommandAllowlistSupport(tools map[string]any, engine CodingAgentEngine) error {
	capabilities := engine.GetCapabilities()
	if capabilities.BashCommandAllowlist {
		return nil
	}
	if !HasBashExplicitRestriction(tools) {
		return nil
	}
	if capabilities.BashDisable && hasBashFullyDisabled(tools) {
		// The engine cannot enforce a per-command allowlist, but tools.bash here requests a
		// complete refusal of all bash commands (bash: false, or bash: []), which the engine
		// can honor directly (e.g. Codex's features.shell_tool=false), so this is safe to allow.
		agentValidationLog.Printf("Engine %s fully disables bash instead of enforcing an allowlist", engine.GetID())
		return nil
	}
	agentValidationLog.Printf("Engine %s does not support bash command allowlist, emitting error", engine.GetID())
	return fmt.Errorf("engine '%s' does not support bash command allow-listing: tools.bash with specific commands is silently ignored at runtime for this engine. "+
		"Use 'bash: [\"*\"]' to allow all commands, 'bash: false' to fully disable bash, or remove the tools.bash entry. "+
		"To restrict bash commands to a specific allowlist, switch to an engine that supports this feature (copilot, claude, or gemini)",
		engine.GetID())
}

// HasBashExplicitRestriction reports true when the tools map contains a bash configuration
// that represents an explicit restriction: bash: false, bash: [], or a non-wildcard command list.
// Only absent/nil bash, bash: true, and wildcard lists (["*"], [":*"]) return false.
// This function is used for compile-time validation and by the `gh aw fix` codemods.
// See hasBashRestrictedAllowlist for the variant used in MCP CLI command injection.
func HasBashExplicitRestriction(tools map[string]any) bool {
	if tools == nil {
		return false
	}
	bashConfig, hasBash := tools["bash"]
	if !hasBash || bashConfig == nil {
		return false
	}
	if asBool, ok := bashConfig.(bool); ok {
		// bash: false disables bash (explicit restriction); bash: true allows all (unrestricted)
		return !asBool
	}
	bashCommands, ok := bashConfig.([]any)
	if !ok {
		return false
	}
	// empty list explicitly allows no commands — that is a restriction
	if len(bashCommands) == 0 {
		return true
	}
	for _, cmd := range bashCommands {
		if cmdStr, ok := cmd.(string); ok && (cmdStr == "*" || cmdStr == ":*") {
			return false
		}
	}
	return true
}

// hasBashFullyDisabled reports true when the tools map's bash configuration represents a
// complete refusal of all bash commands: bash: false, or bash: [] (empty allowlist). Unlike
// hasBashExplicitRestriction, this excludes non-empty, non-wildcard command lists, which require
// per-command allowlist support (EngineCapabilities.BashCommandAllowlist) to enforce safely.
// It is used to decide whether EngineCapabilities.BashDisable is sufficient to allow an explicit
// bash restriction that would otherwise be rejected by validateBashCommandAllowlistSupport.
func hasBashFullyDisabled(tools map[string]any) bool {
	if tools == nil {
		return false
	}
	bashConfig, hasBash := tools["bash"]
	if !hasBash || bashConfig == nil {
		return false
	}
	if asBool, ok := bashConfig.(bool); ok {
		return !asBool
	}
	bashCommands, ok := bashConfig.([]any)
	if !ok {
		return false
	}
	return len(bashCommands) == 0
}

// validateWorkflowRunBranches validates workflow_run trigger requirements.
// It enforces required workflows and branch restrictions guidance.
func (c *Compiler) validateWorkflowRunBranches(workflowData *WorkflowData, markdownPath string) error {
	if !strings.Contains(workflowData.On, "workflow_run") {
		return nil
	}
	agentValidationLog.Print("Validating workflow_run trigger requirements")
	workflowRunMap, ok := parseWorkflowRunTrigger(workflowData.On)
	if !ok {
		return nil
	}
	if err := validateWorkflowRunHasWorkflows(workflowRunMap, markdownPath); err != nil {
		return err
	}
	if _, hasBranches := workflowRunMap["branches"]; hasBranches {
		if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("✓ workflow_run trigger has branch restrictions"))
		}
		return nil
	}
	return c.emitWorkflowRunMissingBranches(markdownPath)
}

func parseWorkflowRunTrigger(onYAML string) (map[string]any, bool) {
	var parsedData map[string]any
	if err := yaml.Unmarshal([]byte(onYAML), &parsedData); err != nil {
		agentValidationLog.Printf("Could not parse On field as YAML: %v", err)
		return nil, false
	}
	onData, hasOn := parsedData["on"]
	if !hasOn {
		return nil, false
	}
	onMap, isMap := onData.(map[string]any)
	if !isMap {
		return nil, false
	}
	workflowRunVal, hasWorkflowRun := onMap["workflow_run"]
	if !hasWorkflowRun {
		return nil, false
	}
	workflowRunMap, ok := workflowRunVal.(map[string]any)
	return workflowRunMap, ok
}

func validateWorkflowRunHasWorkflows(workflowRunMap map[string]any, markdownPath string) error {
	workflowsVal, hasWorkflows := workflowRunMap["workflows"]
	if hasWorkflows && hasNonEmptyWorkflowRunWorkflows(workflowsVal) {
		return nil
	}
	message := `workflow_run trigger must include a non-empty workflows field.

GitHub Actions requires on.workflow_run.workflows to reference at least one workflow.
Without it, the compiled workflow is invalid and will be rejected.

Suggested fix:
on:
  workflow_run:
    workflows: ["your-workflow"]
    types: [completed]`
	return formatCompilerError(markdownPath, "error", message, nil)
}

func (c *Compiler) emitWorkflowRunMissingBranches(markdownPath string) error {
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
		return formatCompilerError(markdownPath, "error", message, nil)
	}
	formattedWarning := formatCompilerMessage(markdownPath, "warning", message)
	fmt.Fprintln(os.Stderr, formattedWarning)
	c.IncrementWarningCount()
	return nil
}

// hasNonEmptyWorkflowRunWorkflows returns true when workflow_run.workflows
// includes at least one non-empty workflow name.
//
// Supported types:
//   - string: valid when non-empty after trimming whitespace
//   - []string: valid when any item is non-empty after trimming whitespace
//   - []any: valid when any string item is non-empty after trimming whitespace
//
// For all other types, it returns false.
func hasNonEmptyWorkflowRunWorkflows(v any) bool {
	switch workflows := v.(type) {
	case string:
		return strings.TrimSpace(workflows) != ""
	case []string:
		for _, workflow := range workflows {
			if strings.TrimSpace(workflow) != "" {
				return true
			}
		}
		return false
	case []any:
		for _, workflow := range workflows {
			s, ok := workflow.(string)
			if ok && strings.TrimSpace(s) != "" {
				return true
			}
		}
		return false
	default:
		return false
	}
}
