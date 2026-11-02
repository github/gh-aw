package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

var mcpScriptsGeneratorLog = logger.New("workflow:mcp_scripts_generator")

// MCPScriptsToolJSON represents a tool configuration for the tools.json file
type MCPScriptsToolJSON struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	InputSchema  map[string]any    `json:"inputSchema"`
	Handler      string            `json:"handler,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Timeout      int               `json:"timeout,omitempty"`
}

// MCPScriptsConfigJSON represents the tools.json configuration file structure
type MCPScriptsConfigJSON struct {
	ServerName string               `json:"serverName"`
	Version    string               `json:"version"`
	LogDir     string               `json:"logDir,omitempty"`
	Tools      []MCPScriptsToolJSON `json:"tools"`
}

// GenerateMCPScriptsToolsConfig generates the tools.json configuration for the mcp-scripts MCP server
func GenerateMCPScriptsToolsConfig(mcpScripts *MCPScriptsConfig) string {
	mcpScriptsGeneratorLog.Printf("Generating mcp-scripts tools.json config: tool_count=%d", len(mcpScripts.Tools))

	config := MCPScriptsConfigJSON{
		ServerName: "mcpscripts",
		Version:    constants.MCPScriptsMCPVersion,
		LogDir:     MCPScriptsDirectory + "/logs",
		Tools:      []MCPScriptsToolJSON{},
	}

	// Sort tool names for stable output
	toolNames := sliceutil.SortedKeys(mcpScripts.Tools)

	for _, toolName := range toolNames {
		toolConfig := mcpScripts.Tools[toolName]

		// Determine handler path based on script type
		var handler string
		if toolConfig.Script != "" {
			handler = toolName + ".cjs"
		} else if toolConfig.Run != "" {
			handler = toolName + ".sh"
		} else if toolConfig.Py != "" {
			handler = toolName + ".py"
		} else if toolConfig.Go != "" {
			handler = toolName + ".go"
		}

		var dependencies []string
		if len(toolConfig.Dependencies) > 0 {
			dependencies = append([]string(nil), toolConfig.Dependencies...)
			sort.Strings(dependencies)
		}

		config.Tools = append(config.Tools, MCPScriptsToolJSON{
			Name:         toolName,
			Description:  toolConfig.Description,
			InputSchema:  generateMCPScriptInputSchema(toolConfig.Inputs),
			Handler:      handler,
			Dependencies: dependencies,
			Env:          generateMCPScriptEnvRefs(toolName, toolConfig.Env),
			Timeout:      toolConfig.Timeout,
		})
	}

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		mcpScriptsGeneratorLog.Printf("Error marshaling tools config: %v", err)
		return "{}"
	}
	mcpScriptsGeneratorLog.Printf("Generated tools.json config: size=%d bytes", len(jsonBytes))
	return string(jsonBytes)
}

func generateMCPScriptInputSchema(inputs map[string]*MCPScriptParam) map[string]any {
	props := make(map[string]any)
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	var required []string
	for _, name := range sliceutil.SortedKeys(inputs) {
		param := inputs[name]
		prop := map[string]any{
			"type":        param.Type,
			"description": param.Description,
		}
		if param.Default != nil {
			prop["default"] = param.Default
		}
		if param.Items != nil {
			prop["items"] = param.Items
		}
		props[name] = prop
		if param.Required {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// Hex encoding preserves distinct tool/name pairs without sanitization collisions.
func mcpScriptEnvBindingName(toolName, envName string) string {
	return fmt.Sprintf("GH_AW_MCP_SCRIPTS_ENV_%x_%x", toolName, envName)
}

func generateMCPScriptEnvRefs(toolName string, env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	refs := make(map[string]string)
	for _, name := range sliceutil.SortedKeys(env) {
		refs[name] = mcpScriptEnvBindingName(toolName, name)
	}
	return refs
}

// GenerateMCPScriptsMCPServerScript generates the entry point script for the mcp-scripts MCP server
// This script uses HTTP transport exclusively
func GenerateMCPScriptsMCPServerScript(mcpScripts *MCPScriptsConfig) string {
	mcpScriptsGeneratorLog.Print("Generating mcp-scripts MCP server entry point script")
	var sb strings.Builder

	// HTTP transport - server started in separate step
	sb.WriteString(`// @ts-check
// Auto-generated mcp-scripts MCP server entry point (HTTP transport)
// This script uses the reusable mcp_scripts_mcp_server_http module

const path = require("path");
const { startHttpServer } = require("./mcp_scripts_mcp_server_http.cjs");

// Configuration file path (generated alongside this script)
const configPath = path.join(__dirname, "tools.json");

// Get port and API key from environment variables
const port = parseInt(process.env.GH_AW_MCP_SCRIPTS_PORT || "3000", 10);
const apiKey = process.env.GH_AW_MCP_SCRIPTS_API_KEY;

// Start the HTTP server
startHttpServer(configPath, {
  port: port,
  apiKey: apiKey,
  stateless: true,
  logDir: "${RUNNER_TEMP}/gh-aw/mcp-scripts/logs"
}).catch(error => {
  console.error("Failed to start mcp-scripts HTTP server:", error);
  process.exit(1);
});
`)

	return sb.String()
}

// formatMultiLineComment formats a description as comment lines using the given prefix (e.g., "# " or "// ").
// Trailing newlines from YAML block scalars are trimmed so no empty comment line is emitted.
func formatMultiLineComment(description, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimRight(description, "\n"), "\n", "\n"+prefix) + "\n"
}

// GenerateMCPScriptJavaScriptToolScript generates the JavaScript tool file for a mcp-script tool
// The user's script code is automatically wrapped in a function with module.exports,
// so users can write simple code without worrying about exports.
// Input parameters are destructured and available as local variables.
func GenerateMCPScriptJavaScriptToolScript(toolConfig *MCPScriptToolConfig) string {
	mcpScriptsLog.Printf("Generating JavaScript tool script: tool=%s, input_count=%d", toolConfig.Name, len(toolConfig.Inputs))
	var sb strings.Builder

	sb.WriteString("// @ts-check\n")
	sb.WriteString("// Auto-generated mcp-script tool: " + toolConfig.Name + "\n\n")
	sb.WriteString("/**\n")
	sb.WriteString(" * " + toolConfig.Description + "\n")
	sb.WriteString(" * @param {Object} inputs - Input parameters\n")
	// Sort input names for stable code generation in JSDoc
	inputNamesForDoc := sliceutil.SortedKeys(toolConfig.Inputs)
	for _, paramName := range inputNamesForDoc {
		param := toolConfig.Inputs[paramName]
		sb.WriteString(" * @param {" + string(param.Type) + "} inputs." + paramName + " - " + param.Description + "\n")
	}
	sb.WriteString(" * @returns {Promise<any>} Tool result\n")
	sb.WriteString(" */\n")
	sb.WriteString("async function execute(inputs) {\n")

	// Destructure inputs to make parameters available as local variables
	if len(toolConfig.Inputs) > 0 {
		var paramNames []string
		for paramName := range toolConfig.Inputs {
			safeName := stringutil.SanitizeParameterName(paramName)
			if safeName != paramName {
				// If sanitized, use alias
				paramNames = append(paramNames, fmt.Sprintf("%s: %s", paramName, safeName))
			} else {
				paramNames = append(paramNames, paramName)
			}
		}
		sort.Strings(paramNames)
		sb.WriteString("  const { " + strings.Join(paramNames, ", ") + " } = inputs || {};\n\n")
	}

	// Indent the user's script code
	sb.WriteString("  " + strings.ReplaceAll(toolConfig.Script, "\n", "\n  ") + "\n")
	sb.WriteString("}\n\n")
	sb.WriteString("module.exports = { execute };\n\n")

	// Delegate subprocess execution to the shared runner module
	sb.WriteString("// Run when executed directly (as a subprocess by the MCP handler)\n")
	sb.WriteString("if (require.main === module) {\n")
	sb.WriteString("  require(\"./mcp-scripts-runner.cjs\")(execute);\n")
	sb.WriteString("}\n")

	return sb.String()
}

// GenerateMCPScriptShellToolScript generates the shell script for a mcp-script tool
func GenerateMCPScriptShellToolScript(toolConfig *MCPScriptToolConfig) string {
	mcpScriptsLog.Printf("Generating shell tool script: tool=%s", toolConfig.Name)
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("# Auto-generated mcp-script tool: " + toolConfig.Name + "\n")
	sb.WriteString(formatMultiLineComment(toolConfig.Description, "# ") + "\n")
	sb.WriteString("set +o histexpand\n")
	sb.WriteString("set -euo pipefail\n\n")
	sb.WriteString(toolConfig.Run + "\n")

	return sb.String()
}

// GenerateMCPScriptPythonToolScript generates the Python script for a mcp-script tool
// Python scripts receive inputs as a dictionary (parsed from JSON stdin):
// - Input parameters are available as a pre-parsed 'inputs' dictionary
// - Individual parameters can be destructured: param = inputs.get('param', default)
// - Outputs are printed to stdout as JSON
// - Environment variables from env: field are available via os.environ
func GenerateMCPScriptPythonToolScript(toolConfig *MCPScriptToolConfig) string {
	mcpScriptsLog.Printf("Generating Python tool script: tool=%s, input_count=%d", toolConfig.Name, len(toolConfig.Inputs))
	var sb strings.Builder

	sb.WriteString("#!/usr/bin/env python3\n")
	sb.WriteString("# Auto-generated mcp-script tool: " + toolConfig.Name + "\n")
	sb.WriteString(formatMultiLineComment(toolConfig.Description, "# ") + "\n")
	sb.WriteString("import json\n")
	sb.WriteString("import os\n")
	sb.WriteString("import sys\n\n")

	// Add wrapper code to read inputs from stdin
	sb.WriteString("# Read inputs from stdin (JSON format)\n")
	sb.WriteString("try:\n")
	sb.WriteString("    inputs = json.loads(sys.stdin.read()) if not sys.stdin.isatty() else {}\n")
	sb.WriteString("except (json.JSONDecodeError, Exception):\n")
	sb.WriteString("    inputs = {}\n\n")

	// Add helper comment about input parameters
	if len(toolConfig.Inputs) > 0 {
		sb.WriteString("# Input parameters available in 'inputs' dictionary:\n")
		// Sort input names for stable code generation
		inputNames := sliceutil.SortedKeys(toolConfig.Inputs)
		for _, paramName := range inputNames {
			param := toolConfig.Inputs[paramName]
			defaultValue := ""
			if param.Default != nil {
				defaultValue = fmt.Sprintf(", default=%v", param.Default)
			}
			sb.WriteString("# " + stringutil.SanitizePythonVariableName(paramName) + " = inputs.get('" + paramName + "'" + defaultValue + ")  # " + param.Description + "\n")
		}
		sb.WriteString("\n")
	}

	// Add user's Python code
	sb.WriteString("# User code:\n")
	sb.WriteString(toolConfig.Py + "\n")

	return sb.String()
}

// GenerateMCPScriptGoToolScript generates the Go script for a mcp-script tool
// Go scripts receive inputs as JSON via stdin and output JSON to stdout:
// - Input parameters are decoded from stdin into a map[string]any
// - Outputs are printed to stdout as JSON
// - Environment variables from env: field are available via os.Getenv()
func GenerateMCPScriptGoToolScript(toolConfig *MCPScriptToolConfig) string {
	mcpScriptsLog.Printf("Generating Go tool script: tool=%s, input_count=%d", toolConfig.Name, len(toolConfig.Inputs))
	var sb strings.Builder

	sb.WriteString("package main\n\n")
	sb.WriteString("// Auto-generated mcp-script tool: " + toolConfig.Name + "\n")
	sb.WriteString(formatMultiLineComment(toolConfig.Description, "// ") + "\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"encoding/json\"\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"io\"\n")
	sb.WriteString("\t\"os\"\n")
	sb.WriteString(")\n\n")

	sb.WriteString("func main() {\n")
	sb.WriteString("\t// Read inputs from stdin (JSON format)\n")
	sb.WriteString("\tvar inputs map[string]any\n")
	sb.WriteString("\tinputData, err := io.ReadAll(os.Stdin)\n")
	sb.WriteString("\tif err == nil && len(inputData) > 0 {\n")
	sb.WriteString("\t\t_ = json.Unmarshal(inputData, &inputs)\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\tif inputs == nil {\n")
	sb.WriteString("\t\tinputs = make(map[string]any)\n")
	sb.WriteString("\t}\n\n")

	// Add helper comment about input parameters
	if len(toolConfig.Inputs) > 0 {
		sb.WriteString("\t// Input parameters available in 'inputs' map:\n")
		// Sort input names for stable code generation
		inputNames := sliceutil.SortedKeys(toolConfig.Inputs)
		for _, paramName := range inputNames {
			param := toolConfig.Inputs[paramName]
			sb.WriteString("\t// " + stringutil.SanitizePythonVariableName(paramName) + " := inputs[\"" + paramName + "\"]  // " + param.Description + "\n")
		}
		sb.WriteString("\n")
	}

	// Add user's Go code with proper indentation
	sb.WriteString("\t// User code:\n")
	userCode := strings.TrimSpace(toolConfig.Go)
	// Indent user code
	indentedCode := strings.ReplaceAll(userCode, "\n", "\n\t")
	sb.WriteString("\t" + indentedCode + "\n")
	sb.WriteString("}\n")

	return sb.String()
}
