// This file provides engine validation for agentic workflows.
//
// # Engine Validation
//
// This file validates top-level engine configuration fields used in agentic workflows,
// including the engine version, MCP timeout settings, and multi-file engine specification
// consistency.
//
// # Validation Functions
//
//   - validateEngineVersion() - Warns when engine.version is set to "latest"
//   - validateEngineMCPSessionTimeout() - Validates engine.mcp.session-timeout duration
//   - validateEngineMCPToolTimeout() - Validates engine.mcp.tool-timeout duration
//   - validateSingleEngineSpecification() - Validates that only one engine field exists across all files
//   - EngineHasValidateSecretStep() - Reports whether an engine provides a validate-secret step
//
// # Validation Pattern: Engine Registry
//
// Engine validation uses the compiler's engine registry:
//   - Supports exact engine ID matching (e.g., "copilot", "claude")
//   - Supports prefix matching for backward compatibility (e.g., "codex-experimental")
//   - Empty engine IDs are valid and use the default engine
//   - Detailed logging of validation steps for debugging
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates engine version or CLI pinning settings
//   - It validates engine.mcp timeout values
//   - It checks engine specification consistency across main and included files
//   - It validates engine field presence or absence at the top level
//
// For engine driver and script validation, see engine_driver_validation.go.
// For inline engine definition and auth validation, see engine_inline_definition_validation.go.
// For engine configuration extraction, see engine.go.
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var engineValidationLog = logger.New("workflow:engine_validation")

// validateEngineVersion warns when the workflow explicitly pins the engine CLI
// to "latest". Unpinned "latest" versions change unpredictably and undermine
// supply chain security guarantees.
func (c *Compiler) validateEngineVersion(workflowData *WorkflowData) error {
	if err := c.validatePiEngineConfig(workflowData); err != nil {
		return err
	}
	if workflowData.EngineConfig == nil || workflowData.EngineConfig.Version == "" {
		// No explicit version set; the compiler uses its own pinned default.
		return nil
	}

	if !strings.EqualFold(workflowData.EngineConfig.Version, "latest") {
		return nil
	}

	engineValidationLog.Print("engine.version: latest detected")

	warningMsg := "engine.version: latest is set – the engine CLI will be installed without a pinned version. " +
		"This is a supply chain security risk: unpinned 'latest' versions can change unexpectedly " +
		"and may introduce vulnerabilities or breaking changes. " +
		"Pin the engine version to a specific version for reproducibility and security."

	fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warningMsg))
	c.IncrementWarningCount()
	return nil
}

// validateEngineMCPSessionTimeout validates optional engine.mcp.session-timeout configuration.
// The value must be a valid Go duration string of at least 5m (no upper bound).
func (c *Compiler) validateEngineMCPSessionTimeout(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.MCPSessionTimeout == "" {
		return nil
	}

	raw := workflowData.EngineConfig.MCPSessionTimeout

	d, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("engine.mcp.session-timeout: invalid duration %q. Must be a valid Go duration string (e.g. \"30m\", \"4h\", \"24h\").\n\nExamples:\n  engine:\n    mcp:\n      session-timeout: 4h\n\nSee: %s", raw, constants.DocsEnginesURL)
	}

	if d < constants.MCPSessionTimeoutMin {
		return fmt.Errorf("engine.mcp.session-timeout: %q is too short (minimum is 5m).\n\nExamples:\n  session-timeout: 30m\n  session-timeout: 4h\n\nSee: %s", raw, constants.DocsEnginesURL)
	}

	engineValidationLog.Printf("engine.mcp.session-timeout validated: %s (%s)", raw, d)
	return nil
}

// validateEngineMCPToolTimeout validates optional engine.mcp.tool-timeout configuration.
// The value must be a valid Go duration string between 10s and 600s inclusive.
func (c *Compiler) validateEngineMCPToolTimeout(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.MCPToolTimeout == "" {
		return nil
	}

	raw := workflowData.EngineConfig.MCPToolTimeout

	d, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("engine.mcp.tool-timeout: invalid duration %q. Must be a valid Go duration string (e.g. \"30s\", \"2m\", \"10m\").\n\nExamples:\n  engine:\n    mcp:\n      tool-timeout: 2m\n\nSee: %s", raw, constants.DocsEnginesURL)
	}

	if d < constants.MCPToolTimeoutMin {
		return fmt.Errorf("engine.mcp.tool-timeout: %q is too short (minimum is 10s).\n\nExamples:\n  tool-timeout: 30s\n  tool-timeout: 2m\n\nSee: %s", raw, constants.DocsEnginesURL)
	}

	if d > constants.MCPToolTimeoutMax {
		return fmt.Errorf("engine.mcp.tool-timeout: %q exceeds the maximum allowed value (600s / 10m).\n\nExamples:\n  tool-timeout: 2m\n  tool-timeout: 10m\n\nSee: %s", raw, constants.DocsEnginesURL)
	}

	engineValidationLog.Printf("engine.mcp.tool-timeout validated: %s (%s)", raw, d)
	return nil
}

// isModelOnlyEngineJSON reports whether engineJSON represents an engine object that
// contains only preference settings (no 'id' or 'runtime' field). Such configs express
// a preference (e.g., model size or MCP timeouts) without selecting a specific engine,
// and must not be counted as engine specifications in conflict detection.
// Only objects whose keys are exclusively from {"model", "mcp"} (with at least one)
// are considered preference-only; other objects (including empty objects or objects
// with unknown keys) fall through to normal validation.
func isModelOnlyEngineJSON(engineJSON string) bool {
	var obj map[string]any
	if err := json.Unmarshal([]byte(engineJSON), &obj); err != nil {
		return false // Not a JSON object; let normal validation handle it
	}
	_, hasID := obj["id"]
	_, hasRuntime := obj["runtime"]
	if hasID || hasRuntime {
		return false
	}
	// Require at least one known preference key; reject empty objects or unknown keys.
	hasPreference := false
	for k := range obj {
		switch k {
		case "model", "mcp":
			hasPreference = true
		default:
			return false // Unknown key — not a preference-only object
		}
	}
	return hasPreference
}

// isEngineDefinitionJSON reports whether engineJSON declares a shared engine definition
// (an engine object carrying a 'behaviors' block). Such entries define an engine's
// installation and execution rather than selecting the workflow's engine.
func isEngineDefinitionJSON(engineJSON string) bool {
	var obj map[string]any
	if err := json.Unmarshal([]byte(engineJSON), &obj); err != nil {
		return false
	}
	_, hasBehaviors := obj["behaviors"]
	return hasBehaviors
}

// validateSingleEngineSpecification validates that only one engine field exists across all files
func (c *Compiler) validateSingleEngineSpecification(mainEngineSetting string, includedEnginesJSON []string) (string, error) { //nolint:largefunc // Preserve existing engine-import validation and error precedence.
	var allEngines []string
	// firstIncludedRealEngine holds the raw JSON of the first non-model-only engine spec
	// from included files. It is used below to extract the engine ID when the single
	// engine specification originates from an included file rather than the main workflow.
	var firstIncludedRealEngine string

	// Add main engine if specified
	if mainEngineSetting != "" {
		allEngines = append(allEngines, mainEngineSetting)
	}

	// Add included engines — skip preference-only configs (objects with only 'model'/'mcp'
	// keys and no 'id' or 'runtime'). These express a model or MCP preference without
	// selecting an engine and must not be counted as engine specifications (avoids spurious
	// "multiple engine fields" errors when a shared workflow only declares engine.model
	// without engine.id). Objects with unknown keys or empty objects are not skipped
	// and will continue through normal validation.
	// Engine *definition* files (declaring behaviors) describe how to run an engine
	// rather than selecting one, so they only act as a selection when nothing else
	// specifies an engine.
	var firstIncludedDefinition string
	for _, engineJSON := range includedEnginesJSON {
		if engineJSON == "" {
			continue
		}
		if isModelOnlyEngineJSON(engineJSON) {
			continue
		}
		if isEngineDefinitionJSON(engineJSON) {
			if firstIncludedDefinition == "" {
				firstIncludedDefinition = engineJSON
			}
			continue
		}
		allEngines = append(allEngines, engineJSON)
		if firstIncludedRealEngine == "" {
			firstIncludedRealEngine = engineJSON
		}
	}
	if len(allEngines) == 0 && firstIncludedDefinition != "" {
		allEngines = append(allEngines, firstIncludedDefinition)
		firstIncludedRealEngine = firstIncludedDefinition
	}

	// Check count (only counting real engine specifications)
	if len(allEngines) == 0 {
		return "", nil // No engine specification found anywhere; will use default
	}

	if len(allEngines) > 1 {
		return "", fmt.Errorf("multiple engine fields found (%d engine specifications detected). Only one engine field is allowed across the main workflow and all included files. Remove duplicate engine specifications to keep only one.\n\nExample:\nengine: copilot\n\nSee: %s", len(allEngines), constants.DocsEnginesURL)
	}

	// Exactly one engine found - parse and return it
	if mainEngineSetting != "" {
		return mainEngineSetting, nil
	}

	// Must be from included file - parse the first real included engine specification
	var firstEngine any
	if err := json.Unmarshal([]byte(firstIncludedRealEngine), &firstEngine); err != nil {
		return "", fmt.Errorf("failed to parse included engine configuration: %w. Expected string or object format.\n\nExample (string):\nengine: copilot\n\nExample (object):\nengine:\n  id: copilot\n  model: gpt-4\n\nSee: %s", err, constants.DocsEnginesURL)
	}

	// Handle string format
	if engineStr, ok := firstEngine.(string); ok {
		return engineStr, nil
	} else if engineObj, ok := firstEngine.(map[string]any); ok {
		// Handle object format: either engine.id (named engine) or engine.runtime.id (inline definition)
		if id, hasID := engineObj["id"]; hasID {
			if idStr, ok := id.(string); ok {
				return idStr, nil
			}
		}
		// Handle inline definition with 'runtime' sub-object (engine.runtime.id)
		if runtime, hasRuntime := engineObj["runtime"]; hasRuntime {
			if runtimeObj, ok := runtime.(map[string]any); ok {
				if id, hasID := runtimeObj["id"]; hasID {
					if idStr, ok := id.(string); ok {
						return idStr, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("invalid engine configuration in included file, missing or invalid 'id' field. Expected string, object with 'id' field, or inline definition with 'runtime.id'.\n\nExample (string):\nengine: copilot\n\nExample (object with id):\nengine:\n  id: copilot\n  model: gpt-4\n\nExample (inline runtime definition):\nengine:\n  runtime:\n    id: codex\n\nSee: %s", constants.DocsEnginesURL)
}

// EngineHasValidateSecretStep checks if the engine provides a validate-secret step.
// This is used to determine whether the secret_verification_result job output should be added.
//
// The validate-secret step is provided by engines that override GetSecretValidationStep():
//   - Copilot engine: Adds step unless permissions.copilot-requests is write or custom command is set
//   - Claude engine: Adds step unless custom command is set
//   - Codex engine: Adds step unless custom command is set
//   - Gemini engine: Adds step unless custom command is set
//   - Custom engine: Never adds this step (uses BaseEngine default which returns empty)
//
// Parameters:
//   - engine: The agentic engine to check
//   - data: The workflow data (needed for GetSecretValidationStep)
//
// Returns:
//   - bool: true if the engine provides a validate-secret step, false otherwise
func EngineHasValidateSecretStep(engine CodingAgentEngine, data *WorkflowData) bool {
	step := engine.GetSecretValidationStep(data)
	return len(step) > 0
}
