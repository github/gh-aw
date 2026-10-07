package workflow

// This file provides Gemini engine tool configuration logic.
//
// It handles two key responsibilities:
//
//  1. Tool Core Mapping (computeGeminiToolsCore):
//     Converts neutral tool names from the workflow configuration into
//     Gemini CLI built-in tool names for the tools.core allowlist in
//     .gemini/settings.json. This restricts the agent to only the tools
//     explicitly requested by the workflow.
//
//  2. Settings Step Generation (generateGeminiSettingsStep):
//     Generates a GitHub Actions step that writes or merges .gemini/settings.json
//     before the Gemini CLI execution. This step always sets:
//     - context.includeDirectories: ["/tmp/"] so file tools can access /tmp/
//     - tools.core: derived from neutral tool configuration
//     The merge approach ensures MCP server config (written by convert_gateway_config_gemini.sh)
//     is preserved while adding the context and tool settings.

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/github/gh-aw/pkg/logger"
)

var geminiToolsLog = logger.New("workflow:gemini_tools")

// computeGeminiToolsCore maps neutral tool names to Gemini CLI built-in tool names
// for use in the tools.core allowlist in .gemini/settings.json.
//
// Neutral tool → Gemini CLI tool mapping:
//   - bash: [cmd, ...]     → run_shell_command(cmd), ... (one entry per command)
//   - bash: * or bash: nil → run_shell_command           (allow all shell commands)
//   - edit: {}             → replace, write_file          (file write tools)
//
// Read-only file system tools are always included as they are essential for
// agentic workflows: glob, grep_search, list_directory, read_file, read_many_files.
//
// See: https://github.com/google-gemini/gemini-cli/blob/main/docs/tools/file-system.md
// See: https://github.com/google-gemini/gemini-cli/blob/main/docs/tools/shell.md
func computeGeminiToolsCore(tools map[string]any) []string {
	// Always include essential read-only file system tools
	toolsCore := []string{
		"glob",
		"grep_search",
		"list_directory",
		"read_file",
		"read_many_files",
	}

	if tools == nil {
		return toolsCore
	}

	toolsCore = append(toolsCore, computeGeminiShellTools(tools)...)

	// Map edit neutral tool to write_file and replace (Gemini's file write tools)
	if _, hasEdit := tools["edit"]; hasEdit {
		geminiToolsLog.Print("edit → replace, write_file")
		toolsCore = append(toolsCore, "replace", "write_file")
	}
	if _, hasWebFetch := tools["web-fetch"]; hasWebFetch {
		toolsCore = append(toolsCore, "web_fetch")
	}

	sort.Strings(toolsCore)
	return toolsCore
}

func computeGeminiShellTools(tools map[string]any) []string {
	var toolsCore []string
	// Map bash neutral tool to run_shell_command
	if bashConfig, hasBash := tools["bash"]; hasBash {
		bashCommands, ok := bashConfig.([]any)
		if !ok || len(bashCommands) == 0 {
			// bash with no specific commands - allow all shell commands
			geminiToolsLog.Print("bash (no specific commands) → run_shell_command")
			toolsCore = append(toolsCore, "run_shell_command")
		} else {
			// Check for wildcard (* or :*)
			hasWildcard := false
			for _, cmd := range bashCommands {
				if cmdStr, ok := cmd.(string); ok && (cmdStr == "*" || cmdStr == ":*") {
					hasWildcard = true
					break
				}
			}
			if hasWildcard {
				geminiToolsLog.Print("bash wildcard → run_shell_command")
				toolsCore = append(toolsCore, "run_shell_command")
			} else {
				// Add an entry for each specific command: run_shell_command(cmd)
				for _, cmd := range bashCommands {
					if cmdStr, ok := cmd.(string); ok {
						// Normalize trailing " *" wildcard (e.g. "jq *" → "jq") so that
						// all engines emit the canonical prefix form (run_shell_command(jq))
						// regardless of whether the command was written with or without the wildcard.
						normalized, _ := normalizeBashCommand(cmdStr)
						entry := fmt.Sprintf("run_shell_command(%s)", normalized)
						geminiToolsLog.Printf("bash %q → %s", cmdStr, entry)
						toolsCore = append(toolsCore, entry)
					}
				}
			}
		}
	}

	return toolsCore
}

func geminiBaseConfig(workflowData *WorkflowData, toolsCore []string) []byte {
	authType := "gemini-api-key"
	if isGeminiVertexWIF(workflowData) {
		authType = "vertex-ai"
	}
	config := map[string]any{
		"context":  map[string]any{"includeDirectories": []string{"/tmp/"}},
		"tools":    map[string]any{"core": toolsCore},
		"security": map[string]any{"auth": map[string]any{"selectedType": authType}},
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		panic(fmt.Sprintf("BUG: failed to marshal Gemini settings: %v", err))
	}
	return configJSON
}

// generateGeminiSettingsStep creates a GitHub Actions step that writes the
// Gemini CLI project settings file (.gemini/settings.json) before execution.
//
// This step:
//  1. Sets context.includeDirectories to ["/tmp/"] so that Gemini CLI file system
//     tools (write_file, replace) can access files in /tmp/ including
//     /tmp/gh-aw/cache-memory/ and other agent working directories.
//  2. Sets tools.core to the list of built-in tools derived from the workflow's
//     neutral tool configuration (bash → run_shell_command, edit → write_file/replace).
//  3. Merges the above settings with any existing .gemini/settings.json, which
//     may have been written by convert_gateway_config_gemini.sh with MCP server
//     configuration. The merge preserves the MCP server config while adding
//     the context and tools settings.
func (e *GeminiEngine) generateGeminiSettingsStep(workflowData *WorkflowData) GitHubActionStep {
	geminiToolsLog.Printf("Generating Gemini settings step for: %s", workflowData.Name)

	tools := workflowData.Tools
	if tools == nil {
		tools = make(map[string]any)
	}
	workflowDataWithEffectiveTools := *workflowData
	workflowDataWithEffectiveTools.Tools = tools
	tools = withMountedCLIShellCommandsInRestrictedBash(&workflowDataWithEffectiveTools)

	// Compute tools.core from neutral tool configuration
	toolsCore := computeGeminiToolsCore(tools)
	geminiToolsLog.Printf("tools.core entries: %d", len(toolsCore))

	// Proxy base URLs otherwise select Gemini's unsupported "gateway" auth mode.
	configJSON := geminiBaseConfig(workflowData, toolsCore)

	// Generate a shell script that:
	// - Creates the .gemini directory if needed
	// - Merges settings into an existing settings.json (from MCP gateway setup), or
	// - Creates a new settings.json when no MCP servers are configured
	//
	// The JSON config is passed via the GH_AW_GEMINI_BASE_CONFIG environment variable
	// to avoid any shell quoting issues with special characters in the JSON.
	//
	// Gemini 0.63 also enforces tools.core on MCP tools, so retain configured servers.
	// jq merge: '$existing * $base' means the RIGHT operand ($base) overrides the LEFT
	// operand ($existing) for conflicting keys. Non-conflicting keys from $existing
	// (e.g. mcpServers written by convert_gateway_config_gemini.sh) are preserved.
	command := `mkdir -p "$GITHUB_WORKSPACE/.gemini"
SETTINGS="$GITHUB_WORKSPACE/.gemini/settings.json"
BASE_CONFIG="$GH_AW_GEMINI_BASE_CONFIG"
if [ -f "$SETTINGS" ]; then
  MERGED=$(jq -n --argjson base "$BASE_CONFIG" --argjson existing "$(cat "$SETTINGS")" '$existing * $base | .tools.core += [(.mcpServers // {} | keys[]) | "mcp_" + . + "_*"]')
  echo "$MERGED" > "$SETTINGS"
else
  echo "$BASE_CONFIG" > "$SETTINGS"
fi`

	stepLines := []string{
		"      - name: Write Gemini Config",
	}
	env := map[string]string{
		"GH_AW_GEMINI_BASE_CONFIG": string(configJSON),
	}
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, env)
	return GitHubActionStep(stepLines)
}
