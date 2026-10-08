package workflow

import (
	"encoding/json"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

var safeScriptsLog = logger.New("workflow:safe_scripts")

// SafeScriptConfig defines a custom safe output handler script that runs in the handler loop.
// Scripts run within the consolidated safe-outputs job as part of the handler manager,
// unlike SafeJobConfig which creates a separate GitHub Actions job.
type SafeScriptConfig struct {
	Name             string                      `yaml:"name,omitempty"`
	Description      string                      `yaml:"description,omitempty"`
	Inputs           map[string]*InputDefinition `yaml:"inputs,omitempty"`
	Script           string                      `yaml:"script,omitempty"` // Inline JavaScript handler (must export a main factory function)
	MemorySchema     map[string]any              `yaml:"-"`
	MemoryDefinition string                      `yaml:"-"`
	Max              int                         `yaml:"-"`
}

// parseSafeScriptsConfig parses safe-scripts configuration from a scripts map.
// This function expects a map of script configurations directly (from safe-outputs.scripts).
func parseSafeScriptsConfig(scriptsMap map[string]any) map[string]*SafeScriptConfig {
	if scriptsMap == nil {
		return nil
	}

	safeScriptsLog.Printf("Parsing %d safe-scripts from scripts map", len(scriptsMap))
	result := make(map[string]*SafeScriptConfig)

	for scriptName, scriptValue := range scriptsMap {
		scriptConfig, ok := scriptValue.(map[string]any)
		if !ok {
			continue
		}

		safeScript := &SafeScriptConfig{}

		// Parse name
		if name, exists := scriptConfig["name"]; exists {
			if nameStr, ok := name.(string); ok {
				safeScript.Name = nameStr
			}
		}

		// Parse description
		if description, exists := scriptConfig["description"]; exists {
			if descStr, ok := description.(string); ok {
				safeScript.Description = descStr
			}
		}

		// Parse inputs using the unified parsing function
		if inputs, exists := scriptConfig["inputs"]; exists {
			if inputsMap, ok := inputs.(map[string]any); ok {
				safeScript.Inputs = ParseInputDefinitions(inputsMap)
			}
		}

		// Parse script content
		if script, exists := scriptConfig["script"]; exists {
			if scriptStr, ok := script.(string); ok {
				safeScript.Script = scriptStr
			}
		}

		safeScriptsLog.Printf("Parsed safe-script configuration: name=%s, has_script=%v, has_inputs=%v",
			scriptName, safeScript.Script != "", len(safeScript.Inputs) > 0)
		result[scriptName] = safeScript
	}

	return result
}

// isSafeScriptName returns true if the script name is safe for use as a filename component.
// It rejects names that contain path separators or ".." sequences that could lead to
// path traversal when the generated filename is passed to require() at runtime.
func isSafeScriptName(name string) bool {
	return !strings.Contains(name, "/") &&
		!strings.Contains(name, "\\") &&
		!strings.Contains(name, "..")
}

// buildCustomSafeOutputScriptsJSON builds a JSON mapping of custom safe output script names to their
// .cjs filenames, for use in the GH_AW_SAFE_OUTPUT_SCRIPTS env var of the handler manager step.
// This allows the handler manager to load and dispatch messages to inline script handlers.
func buildCustomSafeOutputScriptsJSON(data *WorkflowData) string {
	if data.SafeOutputs == nil || len(data.SafeOutputs.Scripts) == 0 {
		return ""
	}

	// Build mapping of normalized script names to their .cjs filenames
	scriptMapping := make(map[string]string, len(data.SafeOutputs.Scripts))
	for scriptName := range data.SafeOutputs.Scripts {
		normalizedName := stringutil.NormalizeSafeOutputIdentifier(scriptName)
		// Reject names that could cause path traversal when the filename is passed to require()
		if !isSafeScriptName(normalizedName) {
			safeScriptsLog.Printf("Warning: skipping script %q — name contains unsafe path characters: %q", scriptName, normalizedName)
			continue
		}
		scriptMapping[normalizedName] = safeOutputScriptFilename(normalizedName)
	}

	// Sort keys for deterministic output
	keys := sliceutil.SortedKeys(scriptMapping)

	ordered := make(map[string]string, len(keys))
	for _, k := range keys {
		ordered[k] = scriptMapping[k]
	}

	jsonBytes, err := json.Marshal(ordered)
	if err != nil {
		safeScriptsLog.Printf("Warning: failed to marshal custom safe output scripts: %v", err)
		return ""
	}
	return string(jsonBytes)
}

// safeOutputScriptFilename returns the .cjs filename for a normalized safe output script name.
func safeOutputScriptFilename(normalizedName string) string {
	return "safe_output_script_" + normalizedName + ".cjs"
}

// generateCustomScriptToolDefinition creates an MCP tool definition for a custom safe-output script.
// Returns a map representing the tool definition in MCP format with name, description, and inputSchema.
// Scripts share the same tool schema generation logic as custom safe-output jobs.
func generateCustomScriptToolDefinition(scriptName string, scriptConfig *SafeScriptConfig) map[string]any {
	// Reuse custom job tool definition logic by adapting the script config
	jobConfig := &SafeJobConfig{
		Description: scriptConfig.Description,
		Inputs:      scriptConfig.Inputs,
	}
	tool := generateCustomJobToolDefinition(scriptName, jobConfig)
	if scriptConfig.MemorySchema != nil {
		tool["inputSchema"] = map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"memory": scriptConfig.MemorySchema},
			"required":   []string{"memory"},
		}
	}
	return tool
}
