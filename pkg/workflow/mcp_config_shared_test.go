//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestRenderSafeOutputsMCPConfigShared tests the shared renderSafeOutputsMCPConfig function
func TestRenderSafeOutputsMCPConfigShared(t *testing.T) {
	tests := []struct {
		name         string
		isLast       bool
		wantContains []string
		wantEnding   string
	}{
		{
			name:   "safe outputs config not last",
			isLast: false,
			wantContains: []string{
				`"safeoutputs": {`,
				`"type": "http"`,
				`"url": "http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT"`,
				`"headers": {`,
				`"Authorization": "$GH_AW_SAFE_OUTPUTS_API_KEY"`,
			},
			wantEnding: "},\n",
		},
		{
			name:   "safe outputs config is last",
			isLast: true,
			wantContains: []string{
				`"safeoutputs": {`,
				`"type": "http"`,
				`"url": "http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT"`,
			},
			wantEnding: "}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			renderSafeOutputsMCPConfig(&yaml, tt.isLast, nil)

			result := yaml.String()

			// Check all required strings are present
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("renderSafeOutputsMCPConfig() result missing %q\nGot:\n%s", want, result)
				}
			}

			// Check correct ending
			if !strings.HasSuffix(result, tt.wantEnding) {
				// Show last part of result for debugging, but handle short strings
				endSnippet := result
				if len(result) > 10 {
					endSnippet = result[len(result)-10:]
				}
				t.Errorf("renderSafeOutputsMCPConfig() ending = %q, want suffix %q", endSnippet, tt.wantEnding)
			}
		})
	}
}

// TestRenderCustomMCPConfigWrapperShared tests the shared renderCustomMCPConfigWrapper function
func TestRenderCustomMCPConfigWrapperShared(t *testing.T) {
	tests := []struct {
		name         string
		toolName     string
		toolConfig   map[string]any
		isLast       bool
		wantContains []string
		wantEnding   string
		wantError    bool
	}{
		{
			name:     "custom MCP config not last",
			toolName: "my-tool",
			toolConfig: map[string]any{
				"command": "node",
				"args":    []string{"server.js"},
			},
			isLast: false,
			wantContains: []string{
				`"my-tool": {`,
				`"command": "node"`,
			},
			wantEnding: "},\n",
			wantError:  false,
		},
		{
			name:     "custom MCP config is last",
			toolName: "another-tool",
			toolConfig: map[string]any{
				"command": "python",
				"args":    []string{"-m", "server"},
			},
			isLast: true,
			wantContains: []string{
				`"another-tool": {`,
				`"command": "python"`,
			},
			wantEnding: "}\n",
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			err := renderCustomMCPConfigWrapper(&yaml, tt.toolName, tt.toolConfig, tt.isLast)

			if (err != nil) != tt.wantError {
				t.Errorf("renderCustomMCPConfigWrapper() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError {
				return
			}

			result := yaml.String()

			// Check all required strings are present
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("renderCustomMCPConfigWrapper() result missing %q\nGot:\n%s", want, result)
				}
			}

			// Check correct ending
			if !strings.HasSuffix(result, tt.wantEnding) {
				// Show last part of result for debugging, but handle short strings
				endSnippet := result
				if len(result) > 10 {
					endSnippet = result[len(result)-10:]
				}
				t.Errorf("renderCustomMCPConfigWrapper() ending = %q, want suffix %q", endSnippet, tt.wantEnding)
			}
		})
	}
}

// TestEngineMethodsDelegateToShared ensures engine methods properly delegate to shared functions
func TestEngineMethodsDelegateToShared(t *testing.T) {
	t.Run("Claude engine Playwright delegation via unified renderer", func(t *testing.T) {
		// Use unified renderer with Claude-specific options
		renderer := NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields: false,
			InlineArgs:           false,
			Format:               "json",
			IsLast:               false,
		})
		var yaml strings.Builder
		playwrightTool := map[string]any{
			"allowed_domains": []any{"example.com"},
		}

		renderer.RenderPlaywrightMCP(&yaml, playwrightTool)
		result := yaml.String()

		if !strings.Contains(result, `"playwright": {`) {
			t.Error("Claude engine should use unified renderer for Playwright MCP config")
		}
	})

	t.Run("Custom engine Playwright delegation", func(t *testing.T) {
		// Use unified renderer with Custom engine options
		renderer := NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields: false,
			InlineArgs:           false,
			Format:               "json",
			IsLast:               false,
		})
		var yaml strings.Builder
		playwrightTool := map[string]any{
			"allowed_domains": []any{"example.com"},
		}

		renderer.RenderPlaywrightMCP(&yaml, playwrightTool)
		result := yaml.String()

		if !strings.Contains(result, `"playwright": {`) {
			t.Error("Custom engine Playwright should produce output via unified renderer")
		}
	})

	t.Run("Claude and Custom engines produce identical output", func(t *testing.T) {
		// Claude engine via unified renderer
		claudeRenderer := NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields: false,
			InlineArgs:           false,
			Format:               "json",
			IsLast:               false,
		})

		// Custom engine also uses unified renderer with same options
		customRenderer := NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields: false,
			InlineArgs:           false,
			Format:               "json",
			IsLast:               false,
		})

		playwrightTool := map[string]any{
			"allowed_domains": []any{"example.com", "test.com"},
		}

		var claudeYAML strings.Builder
		claudeRenderer.RenderPlaywrightMCP(&claudeYAML, playwrightTool)

		var customYAML strings.Builder
		customRenderer.RenderPlaywrightMCP(&customYAML, playwrightTool)

		if claudeYAML.String() != customYAML.String() {
			t.Error("Claude and Custom engines should produce identical Playwright MCP config")
		}
	})
}

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
