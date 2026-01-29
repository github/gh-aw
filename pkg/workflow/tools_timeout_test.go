//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"
)

func TestClaudeEngineWithToolsTimeout(t *testing.T) {
	engine := NewClaudeEngine()

	tests := []struct {
		name           string
		toolsTimeout   int
		expectedEnvVar string
	}{
		{
			name:           "default timeout when not specified",
			toolsTimeout:   0,
			expectedEnvVar: "", // GH_AW_TOOL_TIMEOUT not set when 0
		},
		{
			name:           "custom timeout of 30 seconds",
			toolsTimeout:   30,
			expectedEnvVar: "GH_AW_TOOL_TIMEOUT: 30", // env var in seconds
		},
		{
			name:           "custom timeout of 120 seconds",
			toolsTimeout:   120,
			expectedEnvVar: "GH_AW_TOOL_TIMEOUT: 120", // env var in seconds
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				ToolsTimeout: tt.toolsTimeout,
				Tools:        map[string]any{},
			}

			// Get execution steps
			executionSteps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
			if len(executionSteps) == 0 {
				t.Fatal("Expected at least one execution step")
			}

			// Check the execution step for timeout environment variables
			stepContent := strings.Join([]string(executionSteps[0]), "\n")

			// Determine expected timeouts in milliseconds
			toolTimeoutMs := 60000     // default for tool operations
			startupTimeoutMs := 120000 // default for startup
			if tt.toolsTimeout > 0 {
				toolTimeoutMs = tt.toolsTimeout * 1000
			}

			// Check for MCP_TIMEOUT (uses startup timeout, defaults to 120s)
			expectedMcpTimeout := fmt.Sprintf("MCP_TIMEOUT: %d", startupTimeoutMs)
			if !strings.Contains(stepContent, expectedMcpTimeout) {
				t.Errorf("Expected '%s' in execution step", expectedMcpTimeout)
			}

			// Check for MCP_TOOL_TIMEOUT (uses tool timeout)
			expectedMcpToolTimeout := fmt.Sprintf("MCP_TOOL_TIMEOUT: %d", toolTimeoutMs)
			if !strings.Contains(stepContent, expectedMcpToolTimeout) {
				t.Errorf("Expected '%s' in execution step", expectedMcpToolTimeout)
			}

			// Check for BASH_DEFAULT_TIMEOUT_MS (uses tool timeout)
			expectedBashDefault := fmt.Sprintf("BASH_DEFAULT_TIMEOUT_MS: %d", toolTimeoutMs)
			if !strings.Contains(stepContent, expectedBashDefault) {
				t.Errorf("Expected '%s' in execution step", expectedBashDefault)
			}

			// Check for BASH_MAX_TIMEOUT_MS (uses tool timeout)
			expectedBashMax := fmt.Sprintf("BASH_MAX_TIMEOUT_MS: %d", toolTimeoutMs)
			if !strings.Contains(stepContent, expectedBashMax) {
				t.Errorf("Expected '%s' in execution step", expectedBashMax)
			}

			// Check for GH_AW_TOOL_TIMEOUT if expected
			if tt.expectedEnvVar != "" {
				if !strings.Contains(stepContent, tt.expectedEnvVar) {
					t.Errorf("Expected '%s' in execution step, got: %s", tt.expectedEnvVar, stepContent)
				}
			} else {
				// When timeout is 0, GH_AW_TOOL_TIMEOUT should not be present
				if strings.Contains(stepContent, "GH_AW_TOOL_TIMEOUT") {
					t.Errorf("Did not expect GH_AW_TOOL_TIMEOUT in execution step when timeout is 0")
				}
			}
		})
	}
}

func TestCodexEngineWithToolsTimeout(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name            string
		toolsTimeout    int
		expectedTimeout string
		expectedEnvVar  string
	}{
		{
			name:            "default timeout when not specified",
			toolsTimeout:    0,
			expectedTimeout: "tool_timeout_sec = 60", // 60 seconds default (changed from 120)
			expectedEnvVar:  "",                      // GH_AW_TOOL_TIMEOUT not set when 0
		},
		{
			name:            "custom timeout of 30 seconds",
			toolsTimeout:    30,
			expectedTimeout: "tool_timeout_sec = 30",
			expectedEnvVar:  "GH_AW_TOOL_TIMEOUT: 30", // env var in seconds
		},
		{
			name:            "custom timeout of 180 seconds",
			toolsTimeout:    180,
			expectedTimeout: "tool_timeout_sec = 180",
			expectedEnvVar:  "GH_AW_TOOL_TIMEOUT: 180", // env var in seconds
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				ToolsTimeout: tt.toolsTimeout,
				Name:         "test-workflow",
				Tools: map[string]any{
					"github": map[string]any{},
				},
			}

			// Render MCP config
			var configBuilder strings.Builder
			mcpTools := []string{"github"}
			engine.RenderMCPConfig(&configBuilder, workflowData.Tools, mcpTools, workflowData)
			configContent := configBuilder.String()

			if !strings.Contains(configContent, tt.expectedTimeout) {
				t.Errorf("Expected '%s' in MCP config, got: %s", tt.expectedTimeout, configContent)
			}

			// Check for GH_AW_TOOL_TIMEOUT in execution steps
			executionSteps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
			if len(executionSteps) == 0 {
				t.Fatal("Expected at least one execution step")
			}

			stepContent := strings.Join([]string(executionSteps[0]), "\n")
			if tt.expectedEnvVar != "" {
				if !strings.Contains(stepContent, tt.expectedEnvVar) {
					t.Errorf("Expected '%s' in execution step, got: %s", tt.expectedEnvVar, stepContent)
				}
			} else {
				// When timeout is 0, GH_AW_TOOL_TIMEOUT should not be present
				if strings.Contains(stepContent, "GH_AW_TOOL_TIMEOUT") {
					t.Errorf("Did not expect GH_AW_TOOL_TIMEOUT in execution step when timeout is 0")
				}
			}
		})
	}
}

func TestExtractToolsTimeout(t *testing.T) {
	compiler := &Compiler{}

	tests := []struct {
		name            string
		tools           map[string]any
		expectedTimeout int
		shouldError     bool
	}{
		{
			name:            "no timeout specified",
			tools:           map[string]any{},
			expectedTimeout: 0,
			shouldError:     false,
		},
		{
			name: "timeout as int",
			tools: map[string]any{
				"timeout": 45,
			},
			expectedTimeout: 45,
			shouldError:     false,
		},
		{
			name: "timeout as int64",
			tools: map[string]any{
				"timeout": int64(90),
			},
			expectedTimeout: 90,
			shouldError:     false,
		},
		{
			name: "timeout as uint",
			tools: map[string]any{
				"timeout": uint(75),
			},
			expectedTimeout: 75,
			shouldError:     false,
		},
		{
			name: "timeout as uint64",
			tools: map[string]any{
				"timeout": uint64(120),
			},
			expectedTimeout: 120,
			shouldError:     false,
		},
		{
			name: "timeout as float64",
			tools: map[string]any{
				"timeout": 60.0,
			},
			expectedTimeout: 60,
			shouldError:     false,
		},
		{
			name:            "nil tools",
			tools:           nil,
			expectedTimeout: 0,
			shouldError:     false,
		},
		{
			name: "zero timeout - should fail",
			tools: map[string]any{
				"timeout": 0,
			},
			expectedTimeout: 0,
			shouldError:     true,
		},
		{
			name: "negative timeout - should fail",
			tools: map[string]any{
				"timeout": -5,
			},
			expectedTimeout: 0,
			shouldError:     true,
		},
		{
			name: "minimum valid timeout (1)",
			tools: map[string]any{
				"timeout": 1,
			},
			expectedTimeout: 1,
			shouldError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeout, err := compiler.extractToolsTimeout(tt.tools)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
				if timeout != tt.expectedTimeout {
					t.Errorf("Expected timeout %d, got %d", tt.expectedTimeout, timeout)
				}
			}
		})
	}
}

func TestCopilotEngineWithToolsTimeout(t *testing.T) {
	engine := NewCopilotEngine()

	tests := []struct {
		name           string
		toolsTimeout   int
		expectedEnvVar string
	}{
		{
			name:           "default timeout when not specified",
			toolsTimeout:   0,
			expectedEnvVar: "", // GH_AW_TOOL_TIMEOUT not set when 0
		},
		{
			name:           "custom timeout of 45 seconds",
			toolsTimeout:   45,
			expectedEnvVar: "GH_AW_TOOL_TIMEOUT: 45", // env var in seconds
		},
		{
			name:           "custom timeout of 200 seconds",
			toolsTimeout:   200,
			expectedEnvVar: "GH_AW_TOOL_TIMEOUT: 200", // env var in seconds
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				ToolsTimeout: tt.toolsTimeout,
				Tools:        map[string]any{},
			}

			// Get execution steps
			executionSteps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
			if len(executionSteps) == 0 {
				t.Fatal("Expected at least one execution step")
			}

			stepContent := strings.Join([]string(executionSteps[0]), "\n")

			// Check for GH_AW_TOOL_TIMEOUT if expected
			if tt.expectedEnvVar != "" {
				if !strings.Contains(stepContent, tt.expectedEnvVar) {
					t.Errorf("Expected '%s' in execution step, got: %s", tt.expectedEnvVar, stepContent)
				}
			} else {
				// When timeout is 0, GH_AW_TOOL_TIMEOUT should not be present
				if strings.Contains(stepContent, "GH_AW_TOOL_TIMEOUT") {
					t.Errorf("Did not expect GH_AW_TOOL_TIMEOUT in execution step when timeout is 0")
				}
			}
		})
	}
}
