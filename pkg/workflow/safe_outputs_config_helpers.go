package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
)

// ========================================
// Safe Output Configuration Helpers
// ========================================

// formatSafeOutputsRunsOn formats the runs-on value from SafeOutputsConfig for job output
func (c *Compiler) formatSafeOutputsRunsOn(safeOutputs *SafeOutputsConfig) string {
	if safeOutputs == nil || safeOutputs.RunsOn == "" {
		return fmt.Sprintf("runs-on: %s", constants.DefaultActivationJobRunnerImage)
	}

	return fmt.Sprintf("runs-on: %s", safeOutputs.RunsOn)
}

// HasSafeOutputsEnabled checks if any safe-outputs are enabled
func HasSafeOutputsEnabled(safeOutputs *SafeOutputsConfig) bool {
	enabled := hasAnySafeOutputEnabled(safeOutputs)

	if safeOutputsConfigLog.Enabled() {
		safeOutputsConfigLog.Printf("Safe outputs enabled check: %v", enabled)
	}

	return enabled
}

// GetEnabledSafeOutputToolNames returns a list of enabled safe output tool names.
// NOTE: Tool names should NOT be included in agent prompts. The agent should query
// the MCP server to discover available tools. This function is used for generating
// the tools.json file that the MCP server provides, and for diagnostic logging.
func GetEnabledSafeOutputToolNames(safeOutputs *SafeOutputsConfig) []string {
	tools := getEnabledSafeOutputToolNamesReflection(safeOutputs)

	if safeOutputsConfigLog.Enabled() {
		safeOutputsConfigLog.Printf("Enabled safe output tools: %v", tools)
	}

	return tools
}
