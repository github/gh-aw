//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestNewMCPConfigRenderer(t *testing.T) {
	tests := []struct {
		name    string
		options MCPRendererOptions
	}{
		{
			name: "copilot options",
			options: MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               false,
			},
		},
		{
			name: "claude options",
			options: MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               true,
			},
		},
		{
			name: "codex options",
			options: MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "toml",
				IsLast:               false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := NewMCPConfigRenderer(tt.options)
			if renderer == nil {
				t.Fatal("Expected non-nil renderer")
			}
			if renderer.options.Format != tt.options.Format {
				t.Errorf("Expected format %s, got %s", tt.options.Format, renderer.options.Format)
			}
			if renderer.options.IncludeCopilotFields != tt.options.IncludeCopilotFields {
				t.Errorf("Expected IncludeCopilotFields %t, got %t", tt.options.IncludeCopilotFields, renderer.options.IncludeCopilotFields)
			}
			if renderer.options.InlineArgs != tt.options.InlineArgs {
				t.Errorf("Expected InlineArgs %t, got %t", tt.options.InlineArgs, renderer.options.InlineArgs)
			}
			if renderer.options.IsLast != tt.options.IsLast {
				t.Errorf("Expected IsLast %t, got %t", tt.options.IsLast, renderer.options.IsLast)
			}
		})
	}
}

func TestRenderSafeOutputsMCP_JSON_Copilot(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               false,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	// Verify Safe Outputs now uses HTTP transport
	if !strings.Contains(output, `"type": "http"`) {
		t.Error("Expected 'type': 'http' field (safe outputs uses HTTP transport)")
	}
	if !strings.Contains(output, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs server ID")
	}
	// Verify HTTP-based configuration
	if !strings.Contains(output, `"url": "http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT"`) {
		t.Error("Expected HTTP URL field")
	}
	if !strings.Contains(output, `"headers": {`) {
		t.Error("Expected headers field")
	}
	if !strings.Contains(output, `"Authorization":`) {
		t.Error("Expected Authorization header")
	}
	// Check for env var with backslash escaping (Copilot format)
	if !strings.Contains(output, `\${`) {
		t.Error("Expected backslash-escaped env vars for Copilot")
	}
}

func TestRenderSafeOutputsMCP_JSON_Claude(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "json",
		IsLast:               true,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	// Verify HTTP transport is used (same as Copilot)
	if !strings.Contains(output, `"type": "http"`) {
		t.Error("Expected 'type': 'http' field for HTTP transport")
	}
	if !strings.Contains(output, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs server ID")
	}
	// Should not contain 'tools' field (HTTP servers don't have tools field)
	if strings.Contains(output, `"tools"`) {
		t.Error("Should not contain 'tools' field for HTTP servers")
	}
	// Check for env var without backslash escaping (Claude format)
	if strings.Contains(output, `\${`) {
		t.Error("Should not have backslash-escaped env vars for Claude")
	}
	if !strings.Contains(output, `"$GH_AW_SAFE_OUTPUTS`) {
		t.Error("Expected direct shell variable reference for Claude")
	}
}

func TestRenderSafeOutputsMCP_TOML(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "toml",
		IsLast:               false,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	// Verify TOML format with HTTP transport
	if !strings.Contains(output, "[mcp_servers.safeoutputs]") {
		t.Error("Expected TOML section header")
	}
	if !strings.Contains(output, `type = "http"`) {
		t.Error("Expected TOML type field for HTTP transport")
	}
	if !strings.Contains(output, `url = "http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT"`) {
		t.Error("Expected TOML HTTP URL")
	}
	if !strings.Contains(output, "[mcp_servers.safeoutputs.headers]") {
		t.Error("Expected TOML headers section")
	}
	if !strings.Contains(output, `Authorization = "$GH_AW_SAFE_OUTPUTS_API_KEY"`) {
		t.Error("Expected TOML Authorization header")
	}
}

func TestRenderAgenticWorkflowsMCP_JSON_Copilot(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               true,
		ActionMode:           ActionModeDev, // Default action mode is dev
	})

	var yaml strings.Builder
	renderer.RenderAgenticWorkflowsMCP(&yaml)

	output := yaml.String()

	// Verify MCP Gateway Specification v1.0.0 fields
	if !strings.Contains(output, `"type": "stdio"`) {
		t.Error("Expected 'type': 'stdio' field per MCP Gateway Specification")
	}
	if !strings.Contains(output, `"agentic_workflows": {`) {
		t.Error("Expected agentic_workflows server ID")
	}
	// Per MCP Gateway Specification v1.0.0, stdio servers MUST use container format
	// In dev mode, should use locally built image
	if !strings.Contains(output, `"container": "localhost/gh-aw:dev"`) {
		t.Error("Expected dev mode container image for containerized server")
	}
	// In dev mode, should NOT have entrypoint (uses container's default ENTRYPOINT)
	if strings.Contains(output, `"entrypoint"`) {
		t.Error("Did not expect entrypoint field in dev mode (uses container's ENTRYPOINT)")
	}
	// In dev mode, should NOT have entrypointArgs (uses container's default CMD)
	if strings.Contains(output, `"entrypointArgs"`) {
		t.Error("Did not expect entrypointArgs field in dev mode (uses container's CMD)")
	}
	// In dev mode, should NOT have binary mounts
	if strings.Contains(output, `/opt/gh-aw:/opt/gh-aw:ro`) {
		t.Error("Did not expect /opt/gh-aw mount in dev mode (binary is in image)")
	}
	if strings.Contains(output, `/usr/bin/gh:/usr/bin/gh:ro`) {
		t.Error("Did not expect /usr/bin/gh mount in dev mode (gh CLI is in image)")
	}
	// Should have DEBUG and GITHUB_TOKEN
	if !strings.Contains(output, `"DEBUG": "*"`) {
		t.Error("Expected DEBUG set to literal '*' in env vars")
	}
	if !strings.Contains(output, `"GITHUB_TOKEN"`) {
		t.Error("Expected GITHUB_TOKEN in env vars")
	}
	// Should have working directory args
	if !strings.Contains(output, `"args": ["-w", "${{ github.workspace }}"]`) {
		t.Error("Expected args with working directory set to workspace")
	}
}

func TestRenderAgenticWorkflowsMCP_JSON_Claude(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "json",
		IsLast:               false,
		ActionMode:           ActionModeDev, // Default action mode is dev
	})

	var yaml strings.Builder
	renderer.RenderAgenticWorkflowsMCP(&yaml)

	output := yaml.String()

	// Verify Claude format (no Copilot-specific fields)
	if strings.Contains(output, `"type"`) {
		t.Error("Should not contain 'type' field for Claude")
	}
	if strings.Contains(output, `"tools"`) {
		t.Error("Should not contain 'tools' field for Claude")
	}
}

func TestRenderAgenticWorkflowsMCP_TOML(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "toml",
		IsLast:               false,
		ActionMode:           ActionModeDev, // Default action mode is dev
	})

	var yaml strings.Builder
	renderer.RenderAgenticWorkflowsMCP(&yaml)

	output := yaml.String()

	// Verify TOML format (per MCP Gateway Specification v1.0.0)
	if !strings.Contains(output, "[mcp_servers.agentic_workflows]") {
		t.Error("Expected TOML section header")
	}
	// Per MCP Gateway Specification v1.0.0, stdio servers MUST use container format
	// In dev mode, should use locally built image
	if !strings.Contains(output, `container = "localhost/gh-aw:dev"`) {
		t.Error("Expected dev mode container image for containerized server")
	}
	// In dev mode, should NOT have entrypoint (uses container's default ENTRYPOINT)
	if strings.Contains(output, `entrypoint =`) {
		t.Error("Did not expect entrypoint field in dev mode (uses container's ENTRYPOINT)")
	}
	// In dev mode, should NOT have entrypointArgs (uses container's default CMD)
	if strings.Contains(output, `entrypointArgs =`) {
		t.Error("Did not expect entrypointArgs field in dev mode (uses container's CMD)")
	}
	// In dev mode, should NOT have binary mounts
	if strings.Contains(output, `/opt/gh-aw:/opt/gh-aw:ro`) {
		t.Error("Did not expect /opt/gh-aw mount in dev mode (binary is in image)")
	}
	if strings.Contains(output, `/usr/bin/gh:/usr/bin/gh:ro`) {
		t.Error("Did not expect /usr/bin/gh mount in dev mode (gh CLI is in image)")
	}
	// Should have DEBUG, GH_TOKEN and GITHUB_TOKEN
	if !strings.Contains(output, `"DEBUG"`) {
		t.Error("Expected DEBUG in env_vars")
	}
	if !strings.Contains(output, `"GH_TOKEN"`) {
		t.Error("Expected GH_TOKEN in env_vars")
	}
	if !strings.Contains(output, `"GITHUB_TOKEN"`) {
		t.Error("Expected GITHUB_TOKEN in env_vars")
	}
}

func TestRenderGitHubMCP_JSON_Copilot_Local(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               false,
	})

	githubTool := map[string]any{
		"mode":     "local",
		"toolsets": "default",
	}

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	var yaml strings.Builder
	renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)

	output := yaml.String()

	// Verify GitHub MCP config
	if !strings.Contains(output, `"github": {`) {
		t.Error("Expected github server ID")
	}
	if !strings.Contains(output, `"type": "stdio"`) {
		t.Error("Expected 'type': 'stdio' field for Copilot")
	}
	if !strings.Contains(output, `"container":`) {
		t.Error("Expected container field for local mode")
	}
}

func TestRenderGitHubMCP_JSON_Claude_Local(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "json",
		IsLast:               true,
	})

	githubTool := map[string]any{
		"mode":     "local",
		"toolsets": "default",
	}

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	var yaml strings.Builder
	renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)

	output := yaml.String()

	// Verify GitHub MCP config for Claude (no type field)
	if !strings.Contains(output, `"github": {`) {
		t.Error("Expected github server ID")
	}
	if !strings.Contains(output, `"container":`) {
		t.Error("Expected container field for local mode")
	}
	// Claude format does NOT include 'type' field (added only for Copilot)
	if strings.Contains(output, `"type"`) {
		t.Error("Should not contain 'type' field for Claude")
	}
}

func TestRenderGitHubMCP_JSON_Copilot_Remote(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               false,
	})

	githubTool := map[string]any{
		"mode":     "remote",
		"toolsets": "default",
	}

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	var yaml strings.Builder
	renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)

	output := yaml.String()

	// Verify remote GitHub MCP config
	if !strings.Contains(output, `"github": {`) {
		t.Error("Expected github server ID")
	}
	if !strings.Contains(output, `"type": "http"`) {
		t.Error("Expected 'type': 'http' field for remote mode")
	}
	if !strings.Contains(output, `"url"`) {
		t.Error("Expected url field for remote mode")
	}
}

func TestRenderGitHubMCP_TOML(t *testing.T) {
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "toml",
		IsLast:               false,
	})

	githubTool := map[string]any{
		"mode":     "local",
		"toolsets": "default",
	}

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	var yaml strings.Builder
	renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)

	output := yaml.String()

	// TOML format should now be supported and generate valid output
	if output == "" {
		t.Error("Expected non-empty output for TOML format")
	}

	// Verify key TOML elements are present
	expectedElements := []string{
		"[mcp_servers.github]",
		"user_agent =",
		"startup_timeout_sec =",
		"tool_timeout_sec =",
	}

	for _, expected := range expectedElements {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", expected, output)
		}
	}
}

func TestOptionCombinations(t *testing.T) {
	tests := []struct {
		name    string
		options MCPRendererOptions
	}{
		{
			name: "all true",
			options: MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               true,
			},
		},
		{
			name: "all false",
			options: MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               false,
			},
		},
		{
			name: "mixed copilot inline",
			options: MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               false,
			},
		},
		{
			name: "mixed claude inline",
			options: MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := NewMCPConfigRenderer(tt.options)

			// Test each render method doesn't panic
			var yaml strings.Builder

			playwrightTool := map[string]any{
				"allowed-domains": []string{"example.com"},
			}
			renderer.RenderPlaywrightMCP(&yaml, playwrightTool)

			yaml.Reset()
			renderer.RenderSafeOutputsMCP(&yaml, nil)

			yaml.Reset()
			renderer.RenderAgenticWorkflowsMCP(&yaml)

			yaml.Reset()
			githubTool := map[string]any{
				"mode":     "local",
				"toolsets": "default",
			}
			workflowData := &WorkflowData{Name: "test"}
			renderer.RenderGitHubMCP(&yaml, githubTool, workflowData)
		})
	}
}
