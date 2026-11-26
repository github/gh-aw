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
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var geminiToolsLog = logger.New("workflow:gemini_tools")

// Match Gemini CLI's NEVER_ALLOWED_NAME_PATTERNS substring policy, deliberately
// excluding benign names such as AUTHOR_NAME and KEYSTONE_REGION rather than
// allowing an explicit allowlist to bypass sensitive-name redaction.
// See packages/core/src/services/environmentSanitization.ts in google-gemini/gemini-cli.
var geminiSensitiveEnvName = regexp.MustCompile(`(?i)token|secret|password|passwd|key|auth|credential|creds|private|cert`)

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

	// Map bash neutral tool to run_shell_command
	if bashConfig, hasBash := tools["bash"]; hasBash {
		toolsCore = append(toolsCore, computeGeminiShellTools(bashConfig)...)
	}

	// Map edit neutral tool to write_file and replace (Gemini's file write tools)
	if _, hasEdit := tools["edit"]; hasEdit {
		geminiToolsLog.Print("edit → replace, write_file")
		toolsCore = append(toolsCore, "replace")
		toolsCore = append(toolsCore, "write_file")
	}

	// Map web-fetch neutral tool to web_fetch (Gemini's native HTTP fetch tool)
	// See: https://geminicli.com/docs/tools/web-fetch/
	if _, hasWebFetch := tools["web-fetch"]; hasWebFetch {
		geminiToolsLog.Print("web-fetch → web_fetch")
		toolsCore = append(toolsCore, "web_fetch")
	}

	sort.Strings(toolsCore)
	return toolsCore
}

func computeGeminiShellTools(bashConfig any) []string {
	bashCommands, ok := bashConfig.([]any)
	if !ok || len(bashCommands) == 0 {
		return []string{"run_shell_command"}
	}
	hasWildcard := slices.ContainsFunc(bashCommands, func(cmd any) bool {
		cmdStr, ok := cmd.(string)
		return ok && (cmdStr == "*" || cmdStr == ":*")
	})
	if hasWildcard {
		geminiToolsLog.Print("unrestricted bash → run_shell_command")
		return []string{"run_shell_command"}
	}
	var tools []string
	for _, cmd := range bashCommands {
		if cmdStr, ok := cmd.(string); ok {
			// Normalize trailing wildcards to the engine-neutral command prefix.
			normalized, _ := normalizeBashCommand(cmdStr)
			entry := fmt.Sprintf("run_shell_command(%s)", normalized)
			geminiToolsLog.Printf("bash %q → %s", cmdStr, entry)
			tools = append(tools, entry)
		}
	}
	return tools
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

	tools := withMountedCLIShellCommandsInRestrictedBash(workflowData)

	// Compute tools.core from neutral tool configuration
	toolsCore := computeGeminiToolsCore(tools)
	geminiToolsLog.Printf("tools.core entries: %d", len(toolsCore))

	// Build the settings JSON object
	config := map[string]any{
		"context": map[string]any{
			"includeDirectories": []string{"/tmp/"},
		},
		"tools": map[string]any{
			"core": toolsCore,
		},
	}
	if allowed := e.geminiShellEnvironment(workflowData); len(allowed) > 0 {
		config["security"] = map[string]any{
			"environmentVariableRedaction": map[string]any{"enabled": true, "allowed": allowed},
		}
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		geminiToolsLog.Printf("ERROR: Failed to marshal Gemini settings: %v", err)
		configJSON = []byte(`{"context":{"includeDirectories":["/tmp/"]},"tools":{"core":[]}}`)
	}

	// Generate a shell script that:
	// - Creates the .gemini directory if needed
	// - Merges settings into an existing settings.json (from MCP gateway setup), or
	// - Creates a new settings.json when no MCP servers are configured
	//
	// The JSON config is passed via the GH_AW_GEMINI_BASE_CONFIG environment variable
	// to avoid any shell quoting issues with special characters in the JSON.
	//
	// jq merge: '$existing * $base' means the RIGHT operand ($base) overrides the LEFT
	// operand ($existing) for conflicting keys. Non-conflicting keys from $existing
	// (e.g. mcpServers written by convert_gateway_config_gemini.sh) are preserved.
	command := `mkdir -p "$GITHUB_WORKSPACE/.gemini"
SETTINGS="$GITHUB_WORKSPACE/.gemini/settings.json"
BASE_CONFIG="$GH_AW_GEMINI_BASE_CONFIG"
if [ -f "$SETTINGS" ]; then
  MERGED=$(jq -n --argjson base "$BASE_CONFIG" --argjson existing "$(cat "$SETTINGS")" '$existing * $base')
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

// Gemini strictly filters shell environments in GitHub Actions. Preserve
// explicit literal engine settings after agent overrides, never credentials or
// runtime expressions.
func (e *GeminiEngine) geminiShellEnvironment(data *WorkflowData) []string {
	excluded := ComputeAWFExcludeEnvVarNames(data, e.GetRequiredSecretNames(data))
	agentConfig := getAgentConfig(data)
	var allowed []string
	if data.EngineConfig != nil {
		for name, value := range data.EngineConfig.Env {
			if agentConfig != nil {
				if override, ok := agentConfig.Env[name]; ok {
					value = override
				}
			}
			if !slices.Contains(excluded, name) && !geminiSensitiveEnvName.MatchString(name) && !strings.Contains(value, "${{") {
				allowed = append(allowed, name)
			}
		}
	}
	if usesWebFetchMCPForEngine(e, data) {
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE"} {
			if !slices.ContainsFunc(excluded, func(value string) bool { return strings.EqualFold(value, name) }) &&
				!slices.ContainsFunc(allowed, func(value string) bool { return strings.EqualFold(value, name) }) {
				allowed = append(allowed, name)
			}
		}
	}
	slices.Sort(allowed)
	return allowed
}
