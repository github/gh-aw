package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var safeInputsGeneratorLog = logger.New("workflow:safe_inputs_generator")

// SafeInputsToolJSON represents a tool configuration for the tools.json file
type SafeInputsToolJSON struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	InputSchema map[string]any    `json:"inputSchema"`
	Handler     string            `json:"handler,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
}

// SafeInputsConfigJSON represents the tools.json configuration file structure
type SafeInputsConfigJSON struct {
	ServerName string               `json:"serverName"`
	Version    string               `json:"version"`
	LogDir     string               `json:"logDir,omitempty"`
	Tools      []SafeInputsToolJSON `json:"tools"`
}

// generateSafeInputsToolsConfig generates the tools.json configuration for the safe-inputs MCP server
func generateSafeInputsToolsConfig(safeInputs *SafeInputsConfig) string {
	safeInputsGeneratorLog.Printf("Generating safe-inputs tools.json config: tool_count=%d", len(safeInputs.Tools))

	config := SafeInputsConfigJSON{
		ServerName: "safeinputs",
		Version:    constants.SafeInputsMCPVersion,
		LogDir:     SafeInputsDirectory + "/logs",
		Tools:      []SafeInputsToolJSON{},
	}

	// Sort tool names for stable output
	toolNames := make([]string, 0, len(safeInputs.Tools))
	for toolName := range safeInputs.Tools {
		toolNames = append(toolNames, toolName)
	}
	sort.Strings(toolNames)

	for _, toolName := range toolNames {
		toolConfig := safeInputs.Tools[toolName]

		// Build input schema
		inputSchema := map[string]any{
			"type":       "object",
			"properties": make(map[string]any),
		}

		props := inputSchema["properties"].(map[string]any)
		var required []string

		// Sort input names for stable output
		inputNames := make([]string, 0, len(toolConfig.Inputs))
		for paramName := range toolConfig.Inputs {
			inputNames = append(inputNames, paramName)
		}
		sort.Strings(inputNames)

		for _, paramName := range inputNames {
			param := toolConfig.Inputs[paramName]
			propDef := map[string]any{
				"type":        param.Type,
				"description": param.Description,
			}
			if param.Default != nil {
				propDef["default"] = param.Default
			}
			props[paramName] = propDef
			if param.Required {
				required = append(required, paramName)
			}
		}

		sort.Strings(required)
		if len(required) > 0 {
			inputSchema["required"] = required
		}

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

		// Build env list of required environment variables (not actual secrets)
		// This documents which env vars the tool needs, but doesn't store secret values
		// The actual values are passed as environment variables and accessed via process.env
		var envRefs map[string]string
		if len(toolConfig.Env) > 0 {
			envRefs = make(map[string]string)
			// Sort env var names for stable output
			envVarNames := make([]string, 0, len(toolConfig.Env))
			for envVarName := range toolConfig.Env {
				envVarNames = append(envVarNames, envVarName)
			}
			sort.Strings(envVarNames)

			for _, envVarName := range envVarNames {
				// Store just the environment variable name without $ prefix or secret value
				// Handlers access the actual value via process.env[envVarName] at runtime
				envRefs[envVarName] = envVarName
			}
		}

		config.Tools = append(config.Tools, SafeInputsToolJSON{
			Name:        toolName,
			Description: toolConfig.Description,
			InputSchema: inputSchema,
			Handler:     handler,
			Env:         envRefs,
			Timeout:     toolConfig.Timeout,
		})
	}

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		safeInputsGeneratorLog.Printf("Error marshaling tools config: %v", err)
		return "{}"
	}
	safeInputsGeneratorLog.Printf("Generated tools.json config: size=%d bytes", len(jsonBytes))
	return string(jsonBytes)
}

// generateSafeInputsMCPServerScript generates the entry point script for the safe-inputs MCP server
// This script uses HTTP transport exclusively
func generateSafeInputsMCPServerScript(safeInputs *SafeInputsConfig) string {
	safeInputsGeneratorLog.Print("Generating safe-inputs MCP server entry point script")
	var sb strings.Builder

	// HTTP transport - server started in separate step
	sb.WriteString(`// @ts-check
// Auto-generated safe-inputs MCP server entry point (HTTP transport)
// This script uses the reusable safe_inputs_mcp_server_http module

const path = require("path");
const { startHttpServer } = require("./safe_inputs_mcp_server_http.cjs");

// Configuration file path (generated alongside this script)
const configPath = path.join(__dirname, "tools.json");

// Get port and API key from environment variables
const port = parseInt(process.env.GH_AW_SAFE_INPUTS_PORT || "3000", 10);
const apiKey = process.env.GH_AW_SAFE_INPUTS_API_KEY || "";

// Start the HTTP server
startHttpServer(configPath, {
  port: port,
  stateless: true,
  logDir: "/opt/gh-aw/safe-inputs/logs"
}).catch(error => {
  console.error("Failed to start safe-inputs HTTP server:", error);
  process.exit(1);
});
`)

	return sb.String()
}

// generateSafeInputJavaScriptToolScript generates the JavaScript tool file for a safe-input tool
// The user's script code is automatically wrapped in a function with module.exports,
// so users can write simple code without worrying about exports.
// Input parameters are destructured and available as local variables.
func generateSafeInputJavaScriptToolScript(toolConfig *SafeInputToolConfig) string {
	safeInputsLog.Printf("Generating JavaScript tool script: tool=%s, input_count=%d", toolConfig.Name, len(toolConfig.Inputs))
	var sb strings.Builder

	sb.WriteString("// @ts-check\n")
	sb.WriteString("// Auto-generated safe-input tool: " + toolConfig.Name + "\n\n")
	sb.WriteString("/**\n")
	sb.WriteString(" * " + toolConfig.Description + "\n")
	sb.WriteString(" * @param {Object} inputs - Input parameters\n")
	// Sort input names for stable code generation in JSDoc
	inputNamesForDoc := make([]string, 0, len(toolConfig.Inputs))
	for paramName := range toolConfig.Inputs {
		inputNamesForDoc = append(inputNamesForDoc, paramName)
	}
	sort.Strings(inputNamesForDoc)
	for _, paramName := range inputNamesForDoc {
		param := toolConfig.Inputs[paramName]
		fmt.Fprintf(&sb, " * @param {%s} inputs.%s - %s\n", param.Type, paramName, param.Description)
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
		fmt.Fprintf(&sb, "  const { %s } = inputs || {};\n\n", strings.Join(paramNames, ", "))
	}

	// Indent the user's script code
	sb.WriteString("  " + strings.ReplaceAll(toolConfig.Script, "\n", "\n  ") + "\n")
	sb.WriteString("}\n\n")
	sb.WriteString("module.exports = { execute };\n")

	return sb.String()
}

// generateSafeInputShellToolScript generates the shell script for a safe-input tool
func generateSafeInputShellToolScript(toolConfig *SafeInputToolConfig) string {
	safeInputsLog.Printf("Generating shell tool script: tool=%s", toolConfig.Name)
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("# Auto-generated safe-input tool: " + toolConfig.Name + "\n")
	sb.WriteString("# " + toolConfig.Description + "\n\n")
	sb.WriteString("set -euo pipefail\n\n")
	sb.WriteString(toolConfig.Run + "\n")

	return sb.String()
}

// generateSafeInputPythonToolScript generates the Python script for a safe-input tool
// Python scripts receive inputs as a dictionary (parsed from JSON stdin):
// - Input parameters are available as a pre-parsed 'inputs' dictionary
// - Individual parameters can be destructured: param = inputs.get('param', default)
// - Outputs are printed to stdout as JSON
// - Environment variables from env: field are available via os.environ
func generateSafeInputPythonToolScript(toolConfig *SafeInputToolConfig) string {
	safeInputsLog.Printf("Generating Python tool script: tool=%s, input_count=%d", toolConfig.Name, len(toolConfig.Inputs))
	var sb strings.Builder

	sb.WriteString("#!/usr/bin/env python3\n")
	sb.WriteString("# Auto-generated safe-input tool: " + toolConfig.Name + "\n")
	sb.WriteString("# " + toolConfig.Description + "\n\n")
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
		inputNames := make([]string, 0, len(toolConfig.Inputs))
		for paramName := range toolConfig.Inputs {
			inputNames = append(inputNames, paramName)
		}
		sort.Strings(inputNames)
		for _, paramName := range inputNames {
			param := toolConfig.Inputs[paramName]
			defaultValue := ""
			if param.Default != nil {
				defaultValue = fmt.Sprintf(", default=%v", param.Default)
			}
			fmt.Fprintf(&sb, "# %s = inputs.get('%s'%s)  # %s\n",
				stringutil.SanitizePythonVariableName(paramName), paramName, defaultValue, param.Description)
		}
		sb.WriteString("\n")
	}

	// Add user's Python code
	sb.WriteString("# User code:\n")
	sb.WriteString(toolConfig.Py + "\n")

	return sb.String()
}

// generateSafeInputGoToolScript generates the Go script for a safe-input tool
// Go scripts receive inputs as JSON via stdin and output JSON to stdout:
// - Input parameters are decoded from stdin into a map[string]any
// - Outputs are printed to stdout as JSON
// - Environment variables from env: field are available via os.Getenv()
func generateSafeInputGoToolScript(toolConfig *SafeInputToolConfig) string {
	safeInputsLog.Printf("Generating Go tool script: tool=%s, input_count=%d", toolConfig.Name, len(toolConfig.Inputs))
	var sb strings.Builder

	sb.WriteString("package main\n\n")
	sb.WriteString("// Auto-generated safe-input tool: " + toolConfig.Name + "\n")
	sb.WriteString("// " + toolConfig.Description + "\n\n")
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
		inputNames := make([]string, 0, len(toolConfig.Inputs))
		for paramName := range toolConfig.Inputs {
			inputNames = append(inputNames, paramName)
		}
		sort.Strings(inputNames)
		for _, paramName := range inputNames {
			param := toolConfig.Inputs[paramName]
			fmt.Fprintf(&sb, "\t// %s := inputs[\"%s\"]  // %s\n", stringutil.SanitizePythonVariableName(paramName), paramName, param.Description)
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

// Public wrapper functions for CLI use

// GenerateSafeInputsToolsConfigForInspector generates the tools.json configuration for the safe-inputs MCP server
// This is a public wrapper for use by the CLI inspector command
func GenerateSafeInputsToolsConfigForInspector(safeInputs *SafeInputsConfig) string {
	return generateSafeInputsToolsConfig(safeInputs)
}

// GenerateSafeInputsMCPServerScriptForInspector generates the MCP server entry point script
// This is a public wrapper for use by the CLI inspector command
func GenerateSafeInputsMCPServerScriptForInspector(safeInputs *SafeInputsConfig) string {
	return generateSafeInputsMCPServerScript(safeInputs)
}

// GenerateSafeInputJavaScriptToolScriptForInspector generates a JavaScript tool handler script
// This is a public wrapper for use by the CLI inspector command
func GenerateSafeInputJavaScriptToolScriptForInspector(toolConfig *SafeInputToolConfig) string {
	return generateSafeInputJavaScriptToolScript(toolConfig)
}

// GenerateSafeInputShellToolScriptForInspector generates a shell script tool handler
// This is a public wrapper for use by the CLI inspector command
func GenerateSafeInputShellToolScriptForInspector(toolConfig *SafeInputToolConfig) string {
	return generateSafeInputShellToolScript(toolConfig)
}

// GenerateSafeInputPythonToolScriptForInspector generates a Python script tool handler
// This is a public wrapper for use by the CLI inspector command
func GenerateSafeInputPythonToolScriptForInspector(toolConfig *SafeInputToolConfig) string {
	return generateSafeInputPythonToolScript(toolConfig)
}

// GenerateSafeInputGoToolScriptForInspector generates a Go script tool handler
// This is a public wrapper for use by the CLI inspector command
func GenerateSafeInputGoToolScriptForInspector(toolConfig *SafeInputToolConfig) string {
	return generateSafeInputGoToolScript(toolConfig)
}
