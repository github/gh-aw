//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestRewriteLocalhostToDockerHost tests the URL rewriting function for firewall containers
func TestRewriteLocalhostToDockerHost(t *testing.T) {
	tests := []struct {
		name     string
		inputURL string
		expected string
	}{
		{
			name:     "http://localhost with port",
			inputURL: "http://localhost:8765",
			expected: "http://host.docker.internal:8765",
		},
		{
			name:     "http://localhost without port",
			inputURL: "http://localhost",
			expected: "http://host.docker.internal",
		},
		{
			name:     "http://localhost with path",
			inputURL: "http://localhost:8765/mcp",
			expected: "http://host.docker.internal:8765/mcp",
		},
		{
			name:     "https://localhost with port",
			inputURL: "https://localhost:8443",
			expected: "https://host.docker.internal:8443",
		},
		{
			name:     "127.0.0.1 with port",
			inputURL: "http://127.0.0.1:8765",
			expected: "http://host.docker.internal:8765",
		},
		{
			name:     "external URL should not be rewritten",
			inputURL: "https://api.example.com/mcp",
			expected: "https://api.example.com/mcp",
		},
		{
			name:     "URL with localhost in path should not be fully rewritten",
			inputURL: "https://api.github.com/localhost",
			expected: "https://api.github.com/localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rewriteLocalhostToDockerHost(tt.inputURL)
			if result != tt.expected {
				t.Errorf("rewriteLocalhostToDockerHost(%q) = %q, want %q", tt.inputURL, result, tt.expected)
			}
		})
	}
}

// TestHTTPMCPServerLocalhostRewritingWithFirewall tests that HTTP MCP servers have localhost URLs rewritten
// when firewall is enabled (default behavior) and preserved when firewall is disabled
func TestHTTPMCPServerLocalhostRewritingWithFirewall(t *testing.T) {
	t.Run("localhost URL rewritten when firewall enabled (default)", func(t *testing.T) {
		// WorkflowData with nil SandboxConfig means firewall is enabled
		workflowData := &WorkflowData{Name: "test-workflow"}
		toolConfig := map[string]any{
			"type": "http",
			"url":  "http://localhost:8765",
		}

		var yaml strings.Builder
		err := renderCustomMCPConfigWrapperWithContext(&yaml, "gh-aw", toolConfig, true, workflowData)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		result := yaml.String()
		if !strings.Contains(result, "http://host.docker.internal:8765") {
			t.Errorf("Expected localhost to be rewritten to host.docker.internal, got:\n%s", result)
		}
		if strings.Contains(result, "http://localhost:8765") {
			t.Errorf("Expected localhost to NOT be present in output, got:\n%s", result)
		}
	})

	t.Run("localhost URL preserved when firewall disabled", func(t *testing.T) {
		// WorkflowData with SandboxConfig.Agent.Disabled = true means firewall is disabled
		workflowData := &WorkflowData{
			Name: "test-workflow",
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
		}
		toolConfig := map[string]any{
			"type": "http",
			"url":  "http://localhost:8765",
		}

		var yaml strings.Builder
		err := renderCustomMCPConfigWrapperWithContext(&yaml, "gh-aw", toolConfig, true, workflowData)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		result := yaml.String()
		if !strings.Contains(result, "http://localhost:8765") {
			t.Errorf("Expected localhost to be preserved when firewall disabled, got:\n%s", result)
		}
	})

	t.Run("external URL not rewritten regardless of firewall", func(t *testing.T) {
		workflowData := &WorkflowData{Name: "test-workflow"}
		toolConfig := map[string]any{
			"type": "http",
			"url":  "https://api.example.com/mcp",
		}

		var yaml strings.Builder
		err := renderCustomMCPConfigWrapperWithContext(&yaml, "api-server", toolConfig, true, workflowData)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		result := yaml.String()
		if !strings.Contains(result, "https://api.example.com/mcp") {
			t.Errorf("Expected external URL to be preserved, got:\n%s", result)
		}
	})
}
