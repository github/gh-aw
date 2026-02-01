//go:build integration

package workflow

import (
	"strings"
	"testing"
)

func TestCopilotEngine_HTTPMCPWithHeaderSecrets_Integration(t *testing.T) {
	// Create workflow data with HTTP MCP tool using header secrets
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"datadog": map[string]any{
				"type": "http",
				"url":  "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp",
				"headers": map[string]any{
					"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
					"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY }}",
					"DD_SITE":            "${{ secrets.DD_SITE || 'datadoghq.com' }}",
				},
				"allowed": []string{
					"search_datadog_dashboards",
					"search_datadog_slos",
					"search_datadog_metrics",
					"get_datadog_metric",
				},
			},
		},
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
	}

	engine := NewCopilotEngine()

	// Test MCP config rendering
	var mcpConfig strings.Builder
	mcpTools := []string{"datadog"}
	engine.RenderMCPConfig(&mcpConfig, workflowData.Tools, mcpTools, workflowData)

	mcpOutput := mcpConfig.String()

	// Verify MCP config contains headers with env var references (not secret expressions)
	expectedMCPChecks := []string{
		`"datadog": {`,
		`"type": "http"`,
		`"url": "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp"`,
		`"headers": {`,
		`"DD_API_KEY": "\${DD_API_KEY}"`,
		`"DD_APPLICATION_KEY": "\${DD_APPLICATION_KEY}"`,
		`"DD_SITE": "\${DD_SITE}"`,
		`"tools": [`,
		`"search_datadog_dashboards"`,
		`"env": {`,
		`"DD_API_KEY": "\${DD_API_KEY}"`,
		`"DD_APPLICATION_KEY": "\${DD_APPLICATION_KEY}"`,
		`"DD_SITE": "\${DD_SITE}"`,
	}

	for _, expected := range expectedMCPChecks {
		if !strings.Contains(mcpOutput, expected) {
			t.Errorf("Expected MCP config content not found: %q\nActual MCP config:\n%s", expected, mcpOutput)
		}
	}

	// Verify secret expressions are NOT in MCP config
	unexpectedMCPChecks := []string{
		`${{ secrets.DD_API_KEY }}`,
		`${{ secrets.DD_APPLICATION_KEY }}`,
		`${{ secrets.DD_SITE || 'datadoghq.com' }}`,
	}

	for _, unexpected := range unexpectedMCPChecks {
		if strings.Contains(mcpOutput, unexpected) {
			t.Errorf("Unexpected secret expression in MCP config: %q\nActual MCP config:\n%s", unexpected, mcpOutput)
		}
	}

	// Test execution steps to verify env variables are declared
	steps := engine.GetExecutionSteps(workflowData, "/tmp/log.txt")

	// Find the execution step
	var executionStepContent string
	for _, step := range steps {
		stepStr := strings.Join(step, "\n")
		if strings.Contains(stepStr, "Execute GitHub Copilot CLI") {
			executionStepContent = stepStr
			break
		}
	}

	if executionStepContent == "" {
		t.Fatal("Execution step not found")
	}

	// Verify env variables are declared with secret expressions
	expectedEnvChecks := []string{
		`DD_API_KEY: ${{ secrets.DD_API_KEY }}`,
		`DD_APPLICATION_KEY: ${{ secrets.DD_APPLICATION_KEY }}`,
		`DD_SITE: ${{ secrets.DD_SITE || 'datadoghq.com' }}`,
	}

	for _, expected := range expectedEnvChecks {
		if !strings.Contains(executionStepContent, expected) {
			t.Errorf("Expected env declaration not found: %q\nActual execution step:\n%s", expected, executionStepContent)
		}
	}
}

func TestCopilotEngine_MultipleHTTPMCPTools_Integration(t *testing.T) {
	// Create workflow data with multiple HTTP MCP tools
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"datadog": map[string]any{
				"type": "http",
				"url":  "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp",
				"headers": map[string]any{
					"DD_API_KEY": "${{ secrets.DD_API_KEY }}",
				},
			},
			"custom": map[string]any{
				"type": "http",
				"url":  "https://api.custom.com/mcp",
				"headers": map[string]any{
					"CUSTOM_TOKEN": "${{ secrets.CUSTOM_TOKEN }}",
					"X-API-Key":    "${{ secrets.CUSTOM_API_KEY }}",
				},
			},
			"github": map[string]any{
				"allowed": []string{"get_file_contents"},
			},
		},
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
	}

	engine := NewCopilotEngine()

	// Test execution steps
	steps := engine.GetExecutionSteps(workflowData, "/tmp/log.txt")

	// Find the execution step
	var executionStepContent string
	for _, step := range steps {
		stepStr := strings.Join(step, "\n")
		if strings.Contains(stepStr, "Execute GitHub Copilot CLI") {
			executionStepContent = stepStr
			break
		}
	}

	if executionStepContent == "" {
		t.Fatal("Execution step not found")
	}

	// Verify all env variables from both tools are declared
	expectedEnvChecks := []string{
		`CUSTOM_API_KEY: ${{ secrets.CUSTOM_API_KEY }}`,
		`CUSTOM_TOKEN: ${{ secrets.CUSTOM_TOKEN }}`,
		`DD_API_KEY: ${{ secrets.DD_API_KEY }}`,
	}

	for _, expected := range expectedEnvChecks {
		if !strings.Contains(executionStepContent, expected) {
			t.Errorf("Expected env declaration not found: %q\nActual execution step:\n%s", expected, executionStepContent)
		}
	}

	// Verify env variables are sorted alphabetically
	ddIdx := strings.Index(executionStepContent, "DD_API_KEY:")
	customApiIdx := strings.Index(executionStepContent, "CUSTOM_API_KEY:")
	customTokenIdx := strings.Index(executionStepContent, "CUSTOM_TOKEN:")

	if customApiIdx >= customTokenIdx || customTokenIdx >= ddIdx {
		t.Errorf("Env variables are not sorted alphabetically in execution step")
	}
}

func TestCopilotEngine_HTTPMCPWithoutSecrets_Integration(t *testing.T) {
	// Create workflow data with HTTP MCP tool without secrets
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"custom": map[string]any{
				"type": "http",
				"url":  "https://api.example.com/mcp",
				"headers": map[string]any{
					"X-Static-Header": "static-value",
				},
			},
		},
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
	}

	engine := NewCopilotEngine()

	// Test MCP config rendering
	var mcpConfig strings.Builder
	mcpTools := []string{"custom"}
	engine.RenderMCPConfig(&mcpConfig, workflowData.Tools, mcpTools, workflowData)

	mcpOutput := mcpConfig.String()

	// Verify static header is present
	if !strings.Contains(mcpOutput, `"X-Static-Header": "static-value"`) {
		t.Errorf("Expected static header not found in MCP config:\n%s", mcpOutput)
	}

	// Verify no env section is added when there are no secrets
	if strings.Contains(mcpOutput, `"custom": {`) && strings.Contains(mcpOutput, `"env": {`) {
		// Check if env section is specifically for the custom tool (after its opening brace)
		customIdx := strings.Index(mcpOutput, `"custom": {`)
		nextToolIdx := strings.Index(mcpOutput[customIdx+12:], `": {`)
		if nextToolIdx == -1 {
			nextToolIdx = len(mcpOutput) - customIdx - 12
		}
		customSection := mcpOutput[customIdx : customIdx+12+nextToolIdx]

		if strings.Contains(customSection, `"env": {`) {
			t.Errorf("Unexpected env section found in MCP config for tool without secrets:\n%s", mcpOutput)
		}
	}

	// Test execution steps - should not have extra env variables
	steps := engine.GetExecutionSteps(workflowData, "/tmp/log.txt")

	var executionStepContent string
	for _, step := range steps {
		stepStr := strings.Join(step, "\n")
		if strings.Contains(stepStr, "Execute GitHub Copilot CLI") {
			executionStepContent = stepStr
			break
		}
	}

	// Verify no header-related env variables are added
	unexpectedEnvChecks := []string{
		`X_STATIC_HEADER:`,
		`STATIC_VALUE:`,
	}

	for _, unexpected := range unexpectedEnvChecks {
		if strings.Contains(executionStepContent, unexpected) {
			t.Errorf("Unexpected env variable found: %q\nActual execution step:\n%s", unexpected, executionStepContent)
		}
	}
}
