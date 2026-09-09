//go:build !integration

package workflow

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestHandleCustomMCPToolInSwitch verifies custom MCP tool handling in switch statements
func TestHandleCustomMCPToolInSwitch(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		tools         map[string]any
		isLast        bool
		shouldHandle  bool
		renderCalled  bool
		simulateError bool
	}{
		{
			name:     "Valid custom MCP tool",
			toolName: "custom-tool",
			tools: map[string]any{
				"custom-tool": map[string]any{
					"type":    "stdio",
					"command": "node",
					"args":    []string{"server.js"},
				},
			},
			isLast:       false,
			shouldHandle: true,
			renderCalled: true,
		},
		{
			name:     "Valid custom MCP tool - last in list",
			toolName: "custom-tool",
			tools: map[string]any{
				"custom-tool": map[string]any{
					"type":    "http",
					"url":     "https://example.com",
					"headers": map[string]string{"key": "value"},
				},
			},
			isLast:       true,
			shouldHandle: true,
			renderCalled: true,
		},
		{
			name:     "Tool config is not a map",
			toolName: "invalid-tool",
			tools: map[string]any{
				"invalid-tool": "just a string",
			},
			isLast:       false,
			shouldHandle: false,
			renderCalled: false,
		},
		{
			name:     "Tool has no MCP config",
			toolName: "non-mcp-tool",
			tools: map[string]any{
				"non-mcp-tool": map[string]any{
					"some-key": "some-value",
				},
			},
			isLast:       false,
			shouldHandle: false,
			renderCalled: false,
		},
		{
			name:     "Render function returns error",
			toolName: "error-tool",
			tools: map[string]any{
				"error-tool": map[string]any{
					"type":    "stdio",
					"command": "node",
				},
			},
			isLast:        false,
			shouldHandle:  true,
			renderCalled:  true,
			simulateError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			renderCalled := false

			// Create a mock render function
			renderFunc := func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
				renderCalled = true
				if tt.simulateError {
					return errors.New("simulated render error")
				}
				// Write some output to verify it was called
				fmt.Fprintf(yaml, "rendered: %s, isLast: %v\n", toolName, isLast)
				return nil
			}

			handled := HandleCustomMCPToolInSwitch(&yaml, tt.toolName, tt.tools, tt.isLast, renderFunc)

			if handled != tt.shouldHandle {
				t.Errorf("Expected handled=%v, got %v", tt.shouldHandle, handled)
			}

			if renderCalled != tt.renderCalled {
				t.Errorf("Expected renderCalled=%v, got %v", tt.renderCalled, renderCalled)
			}

			// If render was called and no error, verify output
			if tt.renderCalled && !tt.simulateError {
				output := yaml.String()
				if !strings.Contains(output, tt.toolName) {
					t.Errorf("Expected output to contain tool name %q, got: %q", tt.toolName, output)
				}
				if !strings.Contains(output, fmt.Sprintf("isLast: %v", tt.isLast)) {
					t.Errorf("Expected output to contain isLast=%v, got: %q", tt.isLast, output)
				}
			}
		})
	}
}

// TestFormatStepWithCommandAndEnv verifies step formatting with command and environment variables
func TestFormatStepWithCommandAndEnv(t *testing.T) {
	tests := []struct {
		name            string
		stepLines       []string
		command         string
		env             map[string]string
		expectedContent []string
		notExpected     []string
	}{
		{
			name:      "Simple command without env",
			stepLines: []string{"      - name: Test Step"},
			command:   "echo 'hello world'",
			env:       map[string]string{},
			expectedContent: []string{
				"        run: |",
				"          echo 'hello world'",
			},
			notExpected: []string{"env:"},
		},
		{
			name:      "Multi-line command without env",
			stepLines: []string{"      - name: Multi-line Step"},
			command:   "set -o pipefail\necho 'line1'\necho 'line2'",
			env:       map[string]string{},
			expectedContent: []string{
				"        run: |",
				"          set -o pipefail",
				"          echo 'line1'",
				"          echo 'line2'",
			},
		},
		{
			name:      "Command with single env var",
			stepLines: []string{"      - name: Env Step"},
			command:   "npm test",
			env: map[string]string{
				"NODE_ENV": "production",
			},
			expectedContent: []string{
				"        run: |",
				"          npm test",
				"        env:",
				"          NODE_ENV: production",
			},
		},
		{
			name:      "Command with multiple env vars (sorted)",
			stepLines: []string{"      - name: Complex Step"},
			command:   "make build",
			env: map[string]string{
				"ZEBRA":   "last",
				"APPLE":   "first",
				"BETA":    "second",
				"VERSION": "${{ github.sha }}",
			},
			expectedContent: []string{
				"        run: |",
				"          make build",
				"        env:",
				"          APPLE: first",
				"          BETA: second",
				"          VERSION: ${{ github.sha }}",
				"          ZEBRA: last",
			},
		},
		{
			name: "Preserves existing step lines",
			stepLines: []string{
				"      - name: Preserved Step",
				"        id: my-id",
				"        timeout-minutes: 10",
			},
			command: "echo test",
			env: map[string]string{
				"KEY": "value",
			},
			expectedContent: []string{
				"      - name: Preserved Step",
				"        id: my-id",
				"        timeout-minutes: 10",
				"        run: |",
				"          echo test",
				"        env:",
				"          KEY: value",
			},
		},
		{
			name:      "Multi-line command with env vars",
			stepLines: []string{"      - name: Full Featured"},
			command:   "set -o pipefail\nINSTRUCTION=$(cat file.txt)\ncodex exec \"$INSTRUCTION\"",
			env: map[string]string{
				"CODEX_API_KEY": "${{ secrets.CODEX_API_KEY }}",
				"GITHUB_TOKEN":  "${{ secrets.GITHUB_TOKEN }}",
			},
			expectedContent: []string{
				"        run: |",
				"          set -o pipefail",
				"          INSTRUCTION=$(cat file.txt)",
				"          codex exec \"$INSTRUCTION\"",
				"        env:",
				"          CODEX_API_KEY: ${{ secrets.CODEX_API_KEY }}",
				"          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatStepWithCommandAndEnv(tt.stepLines, tt.command, tt.env)

			// Join result for easier comparison
			resultStr := strings.Join(result, "\n")

			// Verify expected content is present
			for _, expected := range tt.expectedContent {
				if !strings.Contains(resultStr, expected) {
					t.Errorf("Expected result to contain %q\nGot:\n%s", expected, resultStr)
				}
			}

			// Verify unexpected content is not present
			for _, notExp := range tt.notExpected {
				if strings.Contains(resultStr, notExp) {
					t.Errorf("Expected result NOT to contain %q\nGot:\n%s", notExp, resultStr)
				}
			}

			// Verify result length includes original lines plus new content
			if len(result) < len(tt.stepLines) {
				t.Errorf("Result should have at least %d lines, got %d", len(tt.stepLines), len(result))
			}
		})
	}
}

// TestFormatStepWithCommandAndEnv_EnvSorting verifies environment variables are sorted alphabetically
func TestFormatStepWithCommandAndEnv_EnvSorting(t *testing.T) {
	env := map[string]string{
		"Z_LAST":   "z",
		"A_FIRST":  "a",
		"M_MIDDLE": "m",
		"B_SECOND": "b",
	}

	result := FormatStepWithCommandAndEnv([]string{"      - name: Test"}, "echo test", env)
	resultStr := strings.Join(result, "\n")

	// Find the env section
	envStartIdx := -1
	for i, line := range result {
		if strings.Contains(line, "env:") {
			envStartIdx = i
			break
		}
	}

	if envStartIdx == -1 {
		t.Fatal("Could not find env section in result")
	}

	// Extract env var lines (skip the "env:" header)
	envLines := result[envStartIdx+1:]

	// Verify alphabetical order
	expectedOrder := []string{
		"          A_FIRST: a",
		"          B_SECOND: b",
		"          M_MIDDLE: m",
		"          Z_LAST: z",
	}

	for i, expected := range expectedOrder {
		if i >= len(envLines) {
			t.Fatalf("Not enough env lines. Expected at least %d, got %d", len(expectedOrder), len(envLines))
		}
		if envLines[i] != expected {
			t.Errorf("Env var at position %d:\nExpected: %q\nGot:      %q\nFull result:\n%s",
				i, expected, envLines[i], resultStr)
		}
	}
}

// TestFormatStepWithCommandAndEnv_Indentation verifies proper YAML indentation
func TestFormatStepWithCommandAndEnv_Indentation(t *testing.T) {
	result := FormatStepWithCommandAndEnv(
		[]string{"      - name: Test"},
		"echo line1\necho line2",
		map[string]string{"KEY": "value"},
	)

	// Check indentation levels
	for _, line := range result {
		if strings.Contains(line, "run: |") {
			if !strings.HasPrefix(line, "        ") {
				t.Errorf("'run:' should have 8 spaces indentation, got: %q", line)
			}
		}
		if strings.Contains(line, "echo") {
			if !strings.HasPrefix(line, "          ") {
				t.Errorf("Command lines should have 10 spaces indentation, got: %q", line)
			}
		}
		if strings.Contains(line, "env:") {
			if !strings.HasPrefix(line, "        ") {
				t.Errorf("'env:' should have 8 spaces indentation, got: %q", line)
			}
		}
		if strings.Contains(line, "KEY:") {
			if !strings.HasPrefix(line, "          ") {
				t.Errorf("Env vars should have 10 spaces indentation, got: %q", line)
			}
		}
	}
}

// TestRenderJSONMCPConfig tests the shared JSON MCP config rendering helper
func TestRenderJSONMCPConfig(t *testing.T) {
	tests := []struct {
		name              string
		tools             map[string]any
		mcpTools          []string
		options           JSONMCPConfigOptions
		expectedContent   []string
		unexpectedContent []string
	}{
		{
			name: "Basic config with GitHub and agentic-workflows",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []string{"get_repo"},
				},
				"agentic-workflows": nil,
			},
			mcpTools: []string{"github", "agentic-workflows"},
			options: JSONMCPConfigOptions{
				ConfigPath: "/tmp/test-config.json",
				Renderers: MCPToolRenderers{
					RenderGitHub: func(yaml *strings.Builder, githubTool map[string]any, isLast bool, workflowData *WorkflowData) {
						yaml.WriteString("              \"github\": { \"test\": true }")
						if !isLast {
							yaml.WriteString(",")
						}
						yaml.WriteString("\n")
					},
					RenderAgenticWorkflows: func(yaml *strings.Builder, isLast bool) {
						yaml.WriteString("              \"agentic-workflows\": { \"test\": true }")
						if !isLast {
							yaml.WriteString(",")
						}
						yaml.WriteString("\n")
					},
					RenderCacheMemory:     func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {},
					RenderSafeOutputs:     func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {},
					RenderCustomMCPConfig: nil,
				},
			},
			expectedContent: []string{
				"GH_AW_NODE=$(which node 2>/dev/null || command -v node 2>/dev/null || echo node)",
				"cat << GH_AW_MCP_CONFIG_NORM_EOF | \"$GH_AW_NODE\" \"${RUNNER_TEMP}/gh-aw/actions/start_mcp_gateway.cjs\"",
				"\"mcpServers\": {",
				"\"github\": { \"test\": true },",
				"\"agentic-workflows\": { \"test\": true }",
				"GH_AW_MCP_CONFIG_NORM_EOF",
			},
		},
		{
			name: "Config with tool filtering",
			tools: map[string]any{
				"github":       map[string]any{},
				"cache-memory": map[string]any{},
			},
			mcpTools: []string{"github", "cache-memory"},
			options: JSONMCPConfigOptions{
				ConfigPath: "/tmp/filtered-config.json",
				Renderers: MCPToolRenderers{
					RenderGitHub: func(yaml *strings.Builder, githubTool map[string]any, isLast bool, workflowData *WorkflowData) {
						yaml.WriteString("              \"github\": { \"filtered\": true }")
						if !isLast {
							yaml.WriteString(",")
						}
						yaml.WriteString("\n")
					},
					RenderCacheMemory:      func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {},
					RenderAgenticWorkflows: func(yaml *strings.Builder, isLast bool) {},
					RenderSafeOutputs:      func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {},
					RenderCustomMCPConfig:  nil,
				},
				FilterTool: func(toolName string) bool {
					// Filter out cache-memory
					return toolName != "cache-memory"
				},
			},
			expectedContent: []string{
				"GH_AW_NODE=$(which node 2>/dev/null || command -v node 2>/dev/null || echo node)",
				"cat << GH_AW_MCP_CONFIG_NORM_EOF | \"$GH_AW_NODE\" \"${RUNNER_TEMP}/gh-aw/actions/start_mcp_gateway.cjs\"",
				"\"github\": { \"filtered\": true }",
			},
			unexpectedContent: []string{
				"cache-memory",
			},
		},
		{
			name: "Config with post-EOF commands",
			tools: map[string]any{
				"github": map[string]any{},
			},
			mcpTools: []string{"github"},
			options: JSONMCPConfigOptions{
				ConfigPath: "/tmp/debug-config.json",
				Renderers: MCPToolRenderers{
					RenderGitHub: func(yaml *strings.Builder, githubTool map[string]any, isLast bool, workflowData *WorkflowData) {
						yaml.WriteString("              \"github\": {}\n")
					},
					RenderCacheMemory:      func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {},
					RenderAgenticWorkflows: func(yaml *strings.Builder, isLast bool) {},
					RenderSafeOutputs:      func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {},
					RenderCustomMCPConfig:  nil,
				},
				PostEOFCommands: func(yaml *strings.Builder) {
					yaml.WriteString("          echo \"DEBUG OUTPUT\"\n")
					yaml.WriteString("          cat /tmp/debug-config.json\n")
				},
			},
			expectedContent: []string{
				"GH_AW_MCP_CONFIG_NORM_EOF",
			},
			unexpectedContent: []string{
				"echo \"DEBUG OUTPUT\"",
				"cat /tmp/debug-config.json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			workflowData := &WorkflowData{}

			err := RenderJSONMCPConfig(&yaml, tt.tools, tt.mcpTools, workflowData, tt.options)
			if err != nil {
				t.Fatalf("RenderJSONMCPConfig failed: %v", err)
			}

			result := yaml.String()
			// Normalize randomized heredoc delimiters before content checks
			result = normalizeHeredocDelimiters(result)

			// Verify expected content
			for _, expected := range tt.expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected result to contain %q\nGot:\n%s", expected, result)
				}
			}

			// Verify unexpected content is not present
			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Expected result NOT to contain %q\nGot:\n%s", unexpected, result)
				}
			}
		})
	}
}

// TestRenderJSONMCPConfig_IsLastHandling tests that isLast is properly set
func TestRenderJSONMCPConfig_IsLastHandling(t *testing.T) {
	tools := map[string]any{
		"github":            map[string]any{},
		"agentic-workflows": nil,
	}
	mcpTools := []string{"github", "agentic-workflows"}

	var callOrder []string
	var isLastValues []bool

	options := JSONMCPConfigOptions{
		ConfigPath: "/tmp/test.json",
		Renderers: MCPToolRenderers{
			RenderGitHub: func(yaml *strings.Builder, githubTool map[string]any, isLast bool, workflowData *WorkflowData) {
				callOrder = append(callOrder, "github")
				isLastValues = append(isLastValues, isLast)
			},
			RenderAgenticWorkflows: func(yaml *strings.Builder, isLast bool) {
				callOrder = append(callOrder, "agentic-workflows")
				isLastValues = append(isLastValues, isLast)
			},
			RenderCacheMemory:     func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {},
			RenderSafeOutputs:     func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {},
			RenderCustomMCPConfig: nil,
		},
	}

	var yaml strings.Builder
	workflowData := &WorkflowData{}
	err := RenderJSONMCPConfig(&yaml, tools, mcpTools, workflowData, options)
	if err != nil {
		t.Fatalf("RenderJSONMCPConfig failed: %v", err)
	}

	// Verify call order
	expectedOrder := []string{"github", "agentic-workflows"}
	if len(callOrder) != len(expectedOrder) {
		t.Fatalf("Expected %d calls, got %d", len(expectedOrder), len(callOrder))
	}
	for i, expected := range expectedOrder {
		if callOrder[i] != expected {
			t.Errorf("Call %d: expected %q, got %q", i, expected, callOrder[i])
		}
	}

	// Verify isLast values
	expectedIsLast := []bool{false, true}
	if len(isLastValues) != len(expectedIsLast) {
		t.Fatalf("Expected %d isLast values, got %d", len(expectedIsLast), len(isLastValues))
	}
	for i, expected := range expectedIsLast {
		if isLastValues[i] != expected {
			t.Errorf("Call %d (%s): expected isLast=%v, got %v", i, callOrder[i], expected, isLastValues[i])
		}
	}
}
