//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestGenerateMCPScriptsMCPServerScript(t *testing.T) {
	config := &MCPScriptsConfig{
		Tools: map[string]*MCPScriptToolConfig{
			"search-issues": {
				Name:        "search-issues",
				Description: "Search for issues in the repository",
				Script:      "return 'hello';",
				Inputs: map[string]*MCPScriptParam{
					"query": {
						Type:        "string",
						Description: "Search query",
						Required:    true,
					},
				},
			},
			"echo-message": {
				Name:        "echo-message",
				Description: "Echo a message",
				Run:         "echo $INPUT_MESSAGE",
				Inputs: map[string]*MCPScriptParam{
					"message": {
						Type:        "string",
						Description: "Message to echo",
						Default:     "Hello",
					},
				},
			},
			"analyze-data": {
				Name:        "analyze-data",
				Description: "Analyze data with Python",
				Py:          "import json\nprint(json.dumps({'result': 'success'}))",
				Dependencies: []string{
					"urllib3",
					"requests",
				},
				Inputs: map[string]*MCPScriptParam{
					"data": {
						Type:        "string",
						Description: "Data to analyze",
						Required:    true,
					},
				},
			},
			"process-data": {
				Name:        "process-data",
				Description: "Process data with Go",
				Go:          "result := map[string]any{\"count\": len(inputs)}\njson.NewEncoder(os.Stdout).Encode(result)",
				Inputs: map[string]*MCPScriptParam{
					"data": {
						Type:        "string",
						Description: "Data to process",
						Required:    true,
					},
				},
			},
		},
	}

	// Test the entry point script
	script := GenerateMCPScriptsMCPServerScript(config)

	// Check for HTTP server entry point structure
	if !strings.Contains(script, "mcp_scripts_mcp_server_http.cjs") {
		t.Error("Script should reference the HTTP MCP server module")
	}

	if !strings.Contains(script, "startHttpServer") {
		t.Error("Script should use startHttpServer function")
	}

	if !strings.Contains(script, "tools.json") {
		t.Error("Script should reference tools.json configuration file")
	}

	if !strings.Contains(script, "${RUNNER_TEMP}/gh-aw/mcp-scripts/logs") {
		t.Error("Script should specify log directory")
	}

	if !strings.Contains(script, "GH_AW_MCP_SCRIPTS_PORT") {
		t.Error("Script should reference GH_AW_MCP_SCRIPTS_PORT environment variable")
	}

	if !strings.Contains(script, "GH_AW_MCP_SCRIPTS_API_KEY") {
		t.Error("Script should reference GH_AW_MCP_SCRIPTS_API_KEY environment variable")
	}
	if !strings.Contains(script, "apiKey: apiKey,") {
		t.Error("Script should pass the API key to the HTTP MCP server")
	}

	// Test the tools configuration JSON
	toolsJSON := GenerateMCPScriptsToolsConfig(config)

	if !strings.Contains(toolsJSON, `"serverName": "mcpscripts"`) {
		t.Error("Tools config should contain server name 'safeinputs'")
	}

	if !strings.Contains(toolsJSON, `"name": "search-issues"`) {
		t.Error("Tools config should contain search-issues tool")
	}

	if !strings.Contains(toolsJSON, `"name": "echo-message"`) {
		t.Error("Tools config should contain echo-message tool")
	}

	if !strings.Contains(toolsJSON, `"name": "analyze-data"`) {
		t.Error("Tools config should contain analyze-data tool")
	}

	if !strings.Contains(toolsJSON, `"name": "process-data"`) {
		t.Error("Tools config should contain process-data tool")
	}

	// Check for JavaScript tool handler
	if !strings.Contains(toolsJSON, `"handler": "search-issues.cjs"`) {
		t.Error("Tools config should reference JavaScript tool handler file")
	}

	// Check for shell tool handler
	if !strings.Contains(toolsJSON, `"handler": "echo-message.sh"`) {
		t.Error("Tools config should reference shell script handler file")
	}

	// Check for Python tool handler
	if !strings.Contains(toolsJSON, `"handler": "analyze-data.py"`) {
		t.Error("Tools config should reference Python script handler file")
	}

	if !strings.Contains(toolsJSON, `"dependencies": [`) {
		t.Error("Tools config should include dependencies array when configured")
	}
	if !strings.Contains(toolsJSON, `"requests",`) || !strings.Contains(toolsJSON, `"urllib3"`) {
		t.Error("Tools config should include configured dependencies")
	}
	if strings.Index(toolsJSON, `"requests"`) > strings.Index(toolsJSON, `"urllib3"`) {
		t.Error("Tools config should sort dependencies for stable output")
	}

	// Check for Go tool handler
	if !strings.Contains(toolsJSON, `"handler": "process-data.go"`) {
		t.Error("Tools config should reference Go script handler file")
	}

	// Check for input schema
	if !strings.Contains(toolsJSON, `"description": "Search query"`) {
		t.Error("Tools config should contain input descriptions")
	}

	if !strings.Contains(toolsJSON, `"required"`) {
		t.Error("Tools config should contain required fields array")
	}
}

func TestGenerateMCPScriptsToolsConfigWithEnv(t *testing.T) {
	config := &MCPScriptsConfig{
		Tools: map[string]*MCPScriptToolConfig{
			"github-query": {
				Name:        "github-query",
				Description: "Query GitHub with authentication",
				Run:         "gh repo view $INPUT_REPO",
				Inputs: map[string]*MCPScriptParam{
					"repo": {
						Type:     "string",
						Required: true,
					},
				},
				Env: map[string]string{
					"GH_TOKEN": "${{ secrets.GITHUB_TOKEN }}",
					"API_KEY":  "${{ secrets.API_KEY }}",
				},
			},
		},
	}

	toolsJSON := GenerateMCPScriptsToolsConfig(config)

	// Verify that env field is present in tools.json
	if !strings.Contains(toolsJSON, `"env"`) {
		t.Error("Tools config should contain env field")
	}

	// The manifest refers to tool-specific transport names, never secret values.
	if !strings.Contains(toolsJSON, `"GH_TOKEN": "`+mcpScriptEnvBindingName("github-query", "GH_TOKEN")+`"`) {
		t.Error("Tools config should contain GH_TOKEN env variable name")
	}

	if !strings.Contains(toolsJSON, `"API_KEY": "`+mcpScriptEnvBindingName("github-query", "API_KEY")+`"`) {
		t.Error("Tools config should contain API_KEY env variable name")
	}

	// Verify that actual secret expressions are NOT in tools.json
	if strings.Contains(toolsJSON, "secrets.GITHUB_TOKEN") {
		t.Error("Tools config should NOT contain secret expressions")
	}

	if strings.Contains(toolsJSON, "secrets.API_KEY") {
		t.Error("Tools config should NOT contain secret expressions")
	}

	// Verify that $ prefix is not used (which might suggest variable expansion)
	if strings.Contains(toolsJSON, `"$GH_TOKEN"`) {
		t.Error("Tools config should NOT contain $ prefix in env values")
	}
}

func TestGenerateMCPScriptJavaScriptToolScript(t *testing.T) {
	config := &MCPScriptToolConfig{
		Name:        "test-tool",
		Description: "A test tool",
		Script:      "return inputs.value * 2;",
		Inputs: map[string]*MCPScriptParam{
			"value": {
				Type:        "number",
				Description: "Value to double",
			},
		},
	}

	script := GenerateMCPScriptJavaScriptToolScript(config)

	if !strings.Contains(script, "test-tool") {
		t.Error("Script should contain tool name")
	}

	if !strings.Contains(script, "A test tool") {
		t.Error("Script should contain description")
	}

	if !strings.Contains(script, "return inputs.value * 2;") {
		t.Error("Script should contain the tool script")
	}

	if !strings.Contains(script, "module.exports") {
		t.Error("Script should export execute function")
	}

	if !strings.Contains(script, "require.main === module") {
		t.Error("Script should have main execution block for subprocess execution")
	}

	if !strings.Contains(script, "require(\"./mcp-scripts-runner.cjs\")(execute)") {
		t.Error("Script should delegate to mcp-scripts-runner.cjs for subprocess execution")
	}
}

func TestGenerateMCPScriptShellToolScript(t *testing.T) {
	config := &MCPScriptToolConfig{
		Name:        "test-shell",
		Description: "A shell test tool",
		Run:         "echo $INPUT_MESSAGE",
	}

	script := GenerateMCPScriptShellToolScript(config)

	if !strings.Contains(script, "#!/bin/bash") {
		t.Error("Script should have bash shebang")
	}

	if !strings.Contains(script, "test-shell") {
		t.Error("Script should contain tool name")
	}

	if !strings.Contains(script, "set -euo pipefail") {
		t.Error("Script should have strict mode")
	}

	if !strings.Contains(script, "echo $INPUT_MESSAGE") {
		t.Error("Script should contain the run command")
	}
}

func TestGenerateMCPScriptShellToolScriptMultiLineDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "with trailing newline (YAML block scalar)",
			description: "First line of description.\nSecond line of description.\nThird line of description.\n",
		},
		{
			name:        "without trailing newline",
			description: "First line of description.\nSecond line of description.\nThird line of description.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &MCPScriptToolConfig{
				Name:        "test-shell-multiline",
				Description: tt.description,
				Run:         "echo hello",
			}

			script := GenerateMCPScriptShellToolScript(config)

			if !strings.Contains(script, "# First line of description.") {
				t.Error("Script should have first description line prefixed with #")
			}
			if !strings.Contains(script, "# Second line of description.") {
				t.Error("Script should have second description line prefixed with #")
			}
			if !strings.Contains(script, "# Third line of description.") {
				t.Error("Script should have third description line prefixed with #")
			}
		})
	}
}

func TestGenerateMCPScriptPythonToolScript(t *testing.T) {
	config := &MCPScriptToolConfig{
		Name:        "test-python",
		Description: "A Python test tool",
		Py:          "result = {'message': 'Hello from Python'}\nprint(json.dumps(result))",
		Inputs: map[string]*MCPScriptParam{
			"message": {
				Type:        "string",
				Description: "Message to process",
			},
			"count": {
				Type:        "number",
				Description: "Number of times",
			},
		},
	}

	script := GenerateMCPScriptPythonToolScript(config)

	if !strings.Contains(script, "#!/usr/bin/env python3") {
		t.Error("Script should have python3 shebang")
	}

	if !strings.Contains(script, "test-python") {
		t.Error("Script should contain tool name")
	}

	if !strings.Contains(script, "import json") {
		t.Error("Script should import json module")
	}

	if !strings.Contains(script, "import sys") {
		t.Error("Script should import sys module")
	}

	if !strings.Contains(script, "inputs = json.loads(sys.stdin.read())") {
		t.Error("Script should parse inputs from stdin")
	}

	if !strings.Contains(script, "result = {'message': 'Hello from Python'}") {
		t.Error("Script should contain the Python code")
	}

	// Check for input parameter documentation
	if !strings.Contains(script, "# message = inputs.get('message'") {
		t.Error("Script should document message parameter access")
	}

	if !strings.Contains(script, "# count = inputs.get('count'") {
		t.Error("Script should document count parameter access")
	}
}

func TestGenerateMCPScriptPythonToolScriptMultiLineDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "with trailing newline (YAML block scalar)",
			description: "First line of description.\nSecond line of description.\nThird line of description.\n",
		},
		{
			name:        "without trailing newline",
			description: "First line of description.\nSecond line of description.\nThird line of description.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &MCPScriptToolConfig{
				Name:        "test-python-multiline",
				Description: tt.description,
				Py:          "print('hello')",
			}

			script := GenerateMCPScriptPythonToolScript(config)

			if !strings.Contains(script, "# First line of description.") {
				t.Error("Script should have first description line prefixed with #")
			}
			if !strings.Contains(script, "# Second line of description.") {
				t.Error("Script should have second description line prefixed with #")
			}
			if !strings.Contains(script, "# Third line of description.") {
				t.Error("Script should have third description line prefixed with #")
			}
		})
	}
}

// TestMCPScriptsStableCodeGeneration verifies that code generation produces stable, deterministic output
// when called multiple times with the same input. This ensures tools and inputs are sorted properly.
func TestMCPScriptsStableCodeGeneration(t *testing.T) {
	// Create a config with multiple tools and inputs to ensure sorting is tested
	config := &MCPScriptsConfig{
		Tools: map[string]*MCPScriptToolConfig{
			"zebra-tool": {
				Name:        "zebra-tool",
				Description: "A tool that starts with Z",
				Run:         "echo zebra",
				Inputs: map[string]*MCPScriptParam{
					"zebra-input": {Type: "string", Description: "Zebra input"},
					"alpha-input": {Type: "number", Description: "Alpha input"},
					"beta-input":  {Type: "boolean", Description: "Beta input"},
				},
				Env: map[string]string{
					"ZEBRA_SECRET": "${{ secrets.ZEBRA }}",
					"ALPHA_SECRET": "${{ secrets.ALPHA }}",
				},
			},
			"alpha-tool": {
				Name:        "alpha-tool",
				Description: "A tool that starts with A",
				Script:      "return 'alpha';",
				Inputs: map[string]*MCPScriptParam{
					"charlie-param": {Type: "string", Description: "Charlie param"},
					"alpha-param":   {Type: "string", Description: "Alpha param"},
				},
			},
			"middle-tool": {
				Name:        "middle-tool",
				Description: "A tool in the middle",
				Run:         "echo middle",
			},
		},
	}

	// Generate the entry point script multiple times and verify identical output
	iterations := 10
	entryScripts := make([]string, iterations)

	for i := range iterations {
		entryScripts[i] = GenerateMCPScriptsMCPServerScript(config)
	}

	// All entry point script iterations should produce identical output
	for i := 1; i < iterations; i++ {
		if entryScripts[i] != entryScripts[0] {
			t.Errorf("GenerateMCPScriptsMCPServerScript produced different output on iteration %d", i+1)
		}
	}

	// Generate the tools config JSON multiple times and verify identical output
	toolsConfigs := make([]string, iterations)

	for i := range iterations {
		toolsConfigs[i] = GenerateMCPScriptsToolsConfig(config)
	}

	// All tools config iterations should produce identical output
	for i := 1; i < iterations; i++ {
		if toolsConfigs[i] != toolsConfigs[0] {
			t.Errorf("GenerateMCPScriptsToolsConfig produced different output on iteration %d", i+1)
			// Find first difference for debugging
			for j := 0; j < len(toolsConfigs[0]) && j < len(toolsConfigs[i]); j++ {
				if toolsConfigs[0][j] != toolsConfigs[i][j] {
					start := max(j-50, 0)
					end := j + 50
					end = min(end, len(toolsConfigs[0]))
					end = min(end, len(toolsConfigs[i]))
					t.Errorf("First difference at position %d:\n  Expected: %q\n  Got: %q", j, toolsConfigs[0][start:end], toolsConfigs[i][start:end])
					break
				}
			}
		}
	}

	// Verify tools appear in sorted order in tools.json (alpha-tool before middle-tool before zebra-tool)
	alphaPos := strings.Index(toolsConfigs[0], `"name": "alpha-tool"`)
	middlePos := strings.Index(toolsConfigs[0], `"name": "middle-tool"`)
	zebraPos := strings.Index(toolsConfigs[0], `"name": "zebra-tool"`)

	if alphaPos == -1 || middlePos == -1 || zebraPos == -1 {
		t.Error("Tools config should contain all tools")
	}

	if alphaPos >= middlePos || middlePos >= zebraPos {
		t.Errorf("Tools should be sorted alphabetically: alpha(%d) < middle(%d) < zebra(%d)", alphaPos, middlePos, zebraPos)
	}

	// Test JavaScript tool script stability
	jsScripts := make([]string, iterations)
	for i := range iterations {
		jsScripts[i] = GenerateMCPScriptJavaScriptToolScript(config.Tools["alpha-tool"])
	}

	for i := 1; i < iterations; i++ {
		if jsScripts[i] != jsScripts[0] {
			t.Errorf("GenerateMCPScriptJavaScriptToolScript produced different output on iteration %d", i+1)
		}
	}

	// Verify inputs in JSDoc are sorted
	alphaParamPos := strings.Index(jsScripts[0], "inputs.alpha-param")
	charlieParamPos := strings.Index(jsScripts[0], "inputs.charlie-param")

	if alphaParamPos == -1 || charlieParamPos == -1 {
		t.Error("JavaScript script should contain all input parameters in JSDoc")
	}

	if alphaParamPos >= charlieParamPos {
		t.Errorf("Input parameters should be sorted alphabetically in JSDoc: alpha(%d) < charlie(%d)", alphaParamPos, charlieParamPos)
	}
}

func TestGenerateMCPScriptGoToolScript(t *testing.T) {
	config := &MCPScriptToolConfig{
		Name:        "test-go",
		Description: "A Go test tool",
		Go:          "result := map[string]any{\"message\": \"Hello from Go\"}\njson.NewEncoder(os.Stdout).Encode(result)",
		Inputs: map[string]*MCPScriptParam{
			"message": {
				Type:        "string",
				Description: "Message to process",
			},
			"count": {
				Type:        "number",
				Description: "Number of times",
			},
		},
	}

	script := GenerateMCPScriptGoToolScript(config)

	if !strings.Contains(script, "package main") {
		t.Error("Script should have package main declaration")
	}

	if !strings.Contains(script, "test-go") {
		t.Error("Script should contain tool name")
	}

	if !strings.Contains(script, "import (") {
		t.Error("Script should have import section")
	}

	if !strings.Contains(script, "\"encoding/json\"") {
		t.Error("Script should import encoding/json")
	}

	if !strings.Contains(script, "\"os\"") {
		t.Error("Script should import os package")
	}

	if !strings.Contains(script, "var inputs map[string]any") {
		t.Error("Script should declare inputs variable")
	}

	if !strings.Contains(script, "io.ReadAll(os.Stdin)") {
		t.Error("Script should read inputs from stdin")
	}

	if !strings.Contains(script, "result := map[string]any{\"message\": \"Hello from Go\"}") {
		t.Error("Script should contain the Go code")
	}

	// Check for input parameter documentation
	if !strings.Contains(script, "// message := inputs[\"message\"]") {
		t.Error("Script should document message parameter access")
	}

	if !strings.Contains(script, "// count := inputs[\"count\"]") {
		t.Error("Script should document count parameter access")
	}
}

func TestGenerateMCPScriptGoToolScriptMultiLineDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{
			name:        "with trailing newline (YAML block scalar)",
			description: "First line of description.\nSecond line of description.\nThird line of description.\n",
		},
		{
			name:        "without trailing newline",
			description: "First line of description.\nSecond line of description.\nThird line of description.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &MCPScriptToolConfig{
				Name:        "test-go-multiline",
				Description: tt.description,
				Go:          "fmt.Println(\"hello\")",
			}

			script := GenerateMCPScriptGoToolScript(config)

			if !strings.Contains(script, "// First line of description.") {
				t.Error("Script should have first description line prefixed with //")
			}
			if !strings.Contains(script, "// Second line of description.") {
				t.Error("Script should have second description line prefixed with //")
			}
			if !strings.Contains(script, "// Third line of description.") {
				t.Error("Script should have third description line prefixed with //")
			}
		})
	}
}
