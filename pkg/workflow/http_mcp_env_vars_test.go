//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCollectMCPEnvironmentVariables_HTTPMCPWithSecrets tests that HTTP MCP servers
// with secrets in headers get their environment variables added to the Start MCP gateway step
func TestCollectMCPEnvironmentVariables_HTTPMCPWithSecrets(t *testing.T) {
	tools := map[string]any{
		"github": map[string]any{
			"mode":     "remote",
			"toolsets": []string{"repos", "issues"},
		},
		"tavily": map[string]any{
			"type": "http",
			"url":  "https://mcp.tavily.com/mcp/",
			"headers": map[string]string{
				"Authorization": "Bearer ${{ secrets.TAVILY_API_KEY }}",
			},
		},
	}

	mcpTools := []string{"github", "tavily"}
	workflowData := &WorkflowData{
		GitHubToken: "${{ secrets.GITHUB_TOKEN }}",
	}
	hasAgenticWorkflows := false

	envVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, hasAgenticWorkflows)

	// Verify TAVILY_API_KEY is present
	assert.Contains(t, envVars, "TAVILY_API_KEY", "TAVILY_API_KEY should be extracted from HTTP MCP headers")
	assert.Equal(t, "${{ secrets.TAVILY_API_KEY }}", envVars["TAVILY_API_KEY"], "TAVILY_API_KEY should have the correct secret expression")
}

// TestCollectMCPEnvironmentVariables_MultipleHTTPMCPServers tests that multiple HTTP MCP servers
// with different secrets all get their environment variables added
func TestCollectMCPEnvironmentVariables_MultipleHTTPMCPServers(t *testing.T) {
	tools := map[string]any{
		"tavily": map[string]any{
			"type": "http",
			"url":  "https://mcp.tavily.com/mcp/",
			"headers": map[string]string{
				"Authorization": "Bearer ${{ secrets.TAVILY_API_KEY }}",
			},
		},
		"datadog": map[string]any{
			"type": "http",
			"url":  "https://api.datadoghq.com/mcp",
			"headers": map[string]string{
				"DD-API-KEY":         "${{ secrets.DD_API_KEY }}",
				"DD-APPLICATION-KEY": "${{ secrets.DD_APP_KEY }}",
			},
		},
	}

	mcpTools := []string{"tavily", "datadog"}
	workflowData := &WorkflowData{}
	hasAgenticWorkflows := false

	envVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, hasAgenticWorkflows)

	// Verify all secrets are present
	assert.Contains(t, envVars, "TAVILY_API_KEY", "TAVILY_API_KEY should be extracted")
	assert.Contains(t, envVars, "DD_API_KEY", "DD_API_KEY should be extracted")
	assert.Contains(t, envVars, "DD_APP_KEY", "DD_APP_KEY should be extracted")
	assert.Len(t, envVars, 3, "Should have exactly 3 environment variables")
}

// TestCollectMCPEnvironmentVariables_HTTPMCPWithoutSecrets verifies that HTTP MCP servers without secrets don't add env vars
func TestCollectMCPEnvironmentVariables_HTTPMCPWithoutSecrets(t *testing.T) {
	tools := map[string]any{
		"public_api": map[string]any{
			"type": "http",
			"url":  "https://api.example.com/mcp",
			"headers": map[string]string{
				"Content-Type": "application/json",
			},
		},
	}

	mcpTools := []string{"public_api"}
	workflowData := &WorkflowData{}
	hasAgenticWorkflows := false

	envVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, hasAgenticWorkflows)

	// Should not add any env vars since there are no secrets
	assert.Empty(t, envVars, "Should not add environment variables for HTTP MCP without secrets")
}
