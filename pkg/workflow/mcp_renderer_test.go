//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestRenderJSONMCPConfig_GatewayAgentIDValidatesAgainstSchema(t *testing.T) {
	gatewayConfig := &MCPGatewayRuntimeConfig{
		Domain:  "${MCP_GATEWAY_DOMAIN}",
		AgentID: "${MCP_GATEWAY_AGENT_ID}",
	}

	var output strings.Builder
	err := RenderJSONMCPConfig(
		&output,
		map[string]any{},
		[]string{},
		&WorkflowData{Name: "test-workflow", FrontmatterHash: "abc123"},
		JSONMCPConfigOptions{
			ConfigPath:    "/tmp/test/mcp-servers.json",
			GatewayConfig: gatewayConfig,
			Renderers:     MCPToolRenderers{},
		},
	)
	if err != nil {
		t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
	}

	result := output.String()
	if strings.Contains(result, `"apiKey"`) {
		t.Fatalf("rendered gateway config must not contain apiKey:\n%s", result)
	}
	if !strings.Contains(result, `"agentId": "${MCP_GATEWAY_AGENT_ID}"`) {
		t.Fatalf("rendered gateway config must contain agentId:\n%s", result)
	}

	start := strings.Index(result, "          {\n")
	end := strings.LastIndex(result, "\n          GH_AW_MCP_CONFIG")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("failed to locate rendered gateway JSON in output:\n%s", result)
	}
	renderedJSON := strings.ReplaceAll(result[start:end], "$MCP_GATEWAY_PORT", "8080")
	renderedJSON = strings.ReplaceAll(renderedJSON, "${MCP_GATEWAY_DOMAIN}", "localhost")
	renderedJSON = strings.ReplaceAll(renderedJSON, "${MCP_GATEWAY_AGENT_ID}", "test-agent")
	var renderedConfig map[string]any
	if err := json.Unmarshal([]byte(renderedJSON), &renderedConfig); err != nil {
		t.Fatalf("rendered gateway config should be valid JSON: %v\njson:\n%s", err, renderedJSON)
	}

	schemaJSON, err := os.ReadFile("schemas/mcp-gateway-config.schema.json")
	if err != nil {
		t.Fatalf("failed to read gateway schema: %v", err)
	}
	schema, err := compileSchema(string(schemaJSON), "https://docs.github.com/gh-aw/schemas/mcp-gateway-config.schema.json")
	if err != nil {
		t.Fatalf("failed to compile gateway schema: %v", err)
	}
	if err := schema.Validate(renderedConfig); err != nil {
		t.Fatalf("rendered gateway config should validate against schema: %v\njson:\n%s", err, renderedJSON)
	}
}

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
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: true,
		InlineArgs:           true,
		Format:               "json",
		IsLast:               false,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	// Verify Safe Outputs now uses containerized stdio transport
	if !strings.Contains(output, `"type": "stdio"`) {
		t.Error("Expected 'type': 'stdio' field for Copilot safe outputs server")
	}
	if !strings.Contains(output, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs server ID")
	}
	if !strings.Contains(output, `"container": "`+pinnedGhAwNodeImage+`"`) {
		t.Error("Expected gh-aw node container image")
	}
	if !strings.Contains(output, `"mounts": ["\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw", "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "/tmp/gh-aw:/tmp/gh-aw:rw"]`) {
		t.Error("Expected workspace, safe-outputs, log, and tmp mounts")
	}
	if !strings.Contains(output, `"args": ["-w", "\${GITHUB_WORKSPACE}"]`) {
		t.Error("Expected working directory args")
	}
	if !strings.Contains(output, `"entrypoint": "sh"`) {
		t.Error("Expected entrypoint override to sh")
	}
	if !strings.Contains(output, `"entrypointArgs": ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`) {
		t.Error("Expected entrypointArgs to run the stdio MCP server script")
	}
	if !strings.Contains(output, `"GH_AW_SAFE_OUTPUTS_CONFIG_PATH": "\${GH_AW_SAFE_OUTPUTS_CONFIG_PATH}"`) {
		t.Error("Expected safe-outputs config path env var")
	}
	if !strings.Contains(output, `"GITHUB_EVENT_NAME": "\${GITHUB_EVENT_NAME}"`) {
		t.Error("Expected GitHub event name env var")
	}
	if !strings.Contains(output, `"GITHUB_EVENT_PATH": "\${GITHUB_EVENT_PATH}"`) {
		t.Error("Expected GitHub event path env var")
	}
	for _, name := range []string{
		"GH_AW_PR_HEAD_BASE_BRANCH",
		"GH_AW_PR_HEAD_BASE_SHA",
		"GH_AW_PR_HEAD_BASE_REPO",
		"GH_AW_PR_HEAD_BASE_PR_NUMBER",
		"GH_AW_PR_HEAD_BASE_REF",
		"GH_AW_PR_HEAD_REPO",
	} {
		if !strings.Contains(output, fmt.Sprintf(`"%s": "\${%s}"`, name, name)) {
			t.Errorf("Expected PR head baseline env var %s", name)
		}
	}
	if strings.Contains(output, `"url": "http://`) {
		t.Error("Did not expect HTTP URL field")
	}
	if strings.Contains(output, `"Authorization":`) {
		t.Error("Did not expect Authorization header")
	}
}

func TestRenderSafeOutputsMCP_JSON_Claude(t *testing.T) {
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "json",
		IsLast:               true,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	if !strings.Contains(output, `"safeoutputs": {`) {
		t.Error("Expected safeoutputs server ID")
	}
	if !strings.Contains(output, `"container": "`+pinnedGhAwNodeImage+`"`) {
		t.Error("Expected gh-aw node container image")
	}
	if !strings.Contains(output, `"entrypoint": "sh"`) {
		t.Error("Expected entrypoint override to sh")
	}
	if !strings.Contains(output, `"entrypointArgs": ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`) {
		t.Error("Expected entrypointArgs to run the stdio MCP server script")
	}
	if !strings.Contains(output, `"GH_AW_SAFE_OUTPUTS": "\${GH_AW_SAFE_OUTPUTS}"`) {
		t.Error("Expected backslash-escaped shell variable reference for safe outputs path")
	}
	if !strings.Contains(output, `"RUNNER_TEMP": "\${RUNNER_TEMP}"`) {
		t.Error("Expected backslash-escaped shell variable reference for RUNNER_TEMP")
	}
	if strings.Contains(output, `"tools"`) {
		t.Error("Should not contain 'tools' field")
	}
	if strings.Contains(output, `"type"`) {
		t.Error("Should not contain 'type' field for Claude stdio server config")
	}
	if strings.Contains(output, `"url": "http://`) {
		t.Error("Did not expect HTTP URL field")
	}
}

func TestRenderSafeOutputsMCP_TOML(t *testing.T) {
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)
	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		IncludeCopilotFields: false,
		InlineArgs:           false,
		Format:               "toml",
		IsLast:               false,
	})

	var yaml strings.Builder
	renderer.RenderSafeOutputsMCP(&yaml, nil)

	output := yaml.String()

	// Verify TOML format with containerized stdio transport
	if !strings.Contains(output, "[mcp_servers.safeoutputs]") {
		t.Error("Expected TOML section header")
	}
	if !strings.Contains(output, `container = "`+pinnedGhAwNodeImage+`"`) {
		t.Error("Expected gh-aw node container image")
	}
	if !strings.Contains(output, `mounts = ["\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw", "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "/tmp/gh-aw:/tmp/gh-aw:rw"]`) {
		t.Error("Expected TOML mounts")
	}
	if !strings.Contains(output, `args = ["-w", "$GITHUB_WORKSPACE"]`) {
		t.Error("Expected TOML args")
	}
	if !strings.Contains(output, `entrypoint = "sh"`) {
		t.Error("Expected TOML entrypoint override to sh")
	}
	if !strings.Contains(output, `entrypointArgs = ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`) {
		t.Error("Expected TOML entrypointArgs to run the stdio MCP server script")
	}
	if !strings.Contains(output, `env_vars = ["DEBUG", "DEFAULT_BRANCH", "GH_AW_ASSETS_ALLOWED_EXTS", "GH_AW_ASSETS_BRANCH", "GH_AW_ASSETS_MAX_SIZE_KB", "GH_AW_MCP_LOG_DIR", "GH_AW_SAFE_OUTPUTS", "GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH", "GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST", "GH_AW_PR_HEAD_BASE_BRANCH", "GH_AW_PR_HEAD_BASE_SHA", "GH_AW_PR_HEAD_BASE_REPO", "GH_AW_PR_HEAD_BASE_PR_NUMBER", "GH_AW_PR_HEAD_BASE_REF", "GH_AW_PR_HEAD_REPO", "GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH", "GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_TOKEN", "GITHUB_WORKSPACE", "RUNNER_TEMP"]`) {
		t.Error("Expected TOML env vars")
	}
	if strings.Contains(output, `type = "http"`) {
		t.Error("Did not expect TOML HTTP type field")
	}
	if strings.Contains(output, `url = "http://`) {
		t.Error("Did not expect TOML HTTP URL")
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
	if !strings.Contains(output, `"`+constants.AgenticWorkflowsMCPServerID.String()+`": {`) {
		t.Error("Expected agenticworkflows server ID")
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
	if strings.Contains(output, `${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro`) {
		t.Error("Did not expect ${RUNNER_TEMP}/gh-aw mount in dev mode (binary is in image)")
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
	// Should have network access and working directory args
	if !strings.Contains(output, `"args": ["--network", "host", "-w", "\${GITHUB_WORKSPACE}"]`) {
		t.Error("Expected args with network access and working directory set to workspace")
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
	if !strings.Contains(output, "[mcp_servers."+constants.AgenticWorkflowsMCPServerID.String()+"]") {
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
	if strings.Contains(output, `${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro`) {
		t.Error("Did not expect ${RUNNER_TEMP}/gh-aw mount in dev mode (binary is in image)")
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

func TestRenderJSONMCPConfig_OTLPGateway(t *testing.T) {
	tests := []struct {
		name         string
		otlpEndpoint string
		otlpHeaders  string
		wantEndpoint bool
	}{
		{
			name:         "OTLP endpoint only (no headers)",
			otlpEndpoint: "https://otel.example.com:4318",
			otlpHeaders:  "",
			wantEndpoint: true,
		},
		{
			name:         "OTLP endpoint and headers",
			otlpEndpoint: "https://otel.example.com:4318",
			otlpHeaders:  "Authorization=Bearer token123",
			wantEndpoint: true,
		},
		{
			name:         "no OTLP config",
			otlpEndpoint: "",
			otlpHeaders:  "",
			wantEndpoint: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Domain:       "localhost",
				AgentID:      "test-api-key",
				OTLPEndpoint: tt.otlpEndpoint,
				OTLPHeaders:  tt.otlpHeaders,
			}

			workflowData := &WorkflowData{
				Name:            "test-workflow",
				FrontmatterHash: "abc123",
			}

			var output strings.Builder
			err := RenderJSONMCPConfig(
				&output,
				map[string]any{},
				[]string{},
				workflowData,
				JSONMCPConfigOptions{
					ConfigPath:    "/tmp/test/mcp-servers.json",
					GatewayConfig: gatewayConfig,
					Renderers:     MCPToolRenderers{},
				},
			)

			if err != nil {
				t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
			}

			result := output.String()

			// Verify no node/bash preamble for header conversion is emitted
			if strings.Contains(result, "_GH_AW_OTLP_HEADERS_JSON") {
				t.Error("output must not contain old _GH_AW_OTLP_HEADERS_JSON preamble")
			}
			if strings.Contains(result, "_GH_AW_OTLP_HEADERS_ESC") {
				t.Error("output must not contain _GH_AW_OTLP_HEADERS_ESC preamble")
			}

			// Verify headers field is never emitted in JSON config; headers are now
			// passed exclusively via the OTEL_EXPORTER_OTLP_HEADERS container env var.
			if strings.Contains(result, `"headers"`) {
				t.Errorf("headers field must not appear in gateway JSON config (use OTEL_EXPORTER_OTLP_HEADERS env var instead)\noutput:\n%s", result)
			}

			// Verify endpoint is present iff configured
			if tt.wantEndpoint && !strings.Contains(result, `"endpoint": "${OTEL_EXPORTER_OTLP_ENDPOINT}"`) {
				t.Errorf("expected endpoint in output\noutput:\n%s", result)
			}
			if !tt.wantEndpoint && strings.Contains(result, `"opentelemetry"`) {
				t.Errorf("expected no opentelemetry section when no endpoint configured\noutput:\n%s", result)
			}
		})
	}
}

// TestRenderJSONMCPConfig_SessionTimeout verifies that sessionTimeout is emitted
// in the gateway JSON section when set on the MCPGatewayRuntimeConfig.
func TestRenderJSONMCPConfig_SessionTimeout(t *testing.T) {
	tests := []struct {
		name           string
		sessionTimeout string
		wantField      bool
	}{
		{
			name:           "includes sessionTimeout when set",
			sessionTimeout: "4h",
			wantField:      true,
		},
		{
			name:           "omits sessionTimeout when empty",
			sessionTimeout: "",
			wantField:      false,
		},
		{
			name:           "includes sessionTimeout 30m",
			sessionTimeout: "30m",
			wantField:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Domain:         "localhost",
				AgentID:        "test-api-key",
				SessionTimeout: tt.sessionTimeout,
			}

			workflowData := &WorkflowData{
				Name:            "test-workflow",
				FrontmatterHash: "abc123",
			}

			var output strings.Builder
			err := RenderJSONMCPConfig(
				&output,
				map[string]any{},
				[]string{},
				workflowData,
				JSONMCPConfigOptions{
					ConfigPath:    "/tmp/test/mcp-servers.json",
					GatewayConfig: gatewayConfig,
					Renderers:     MCPToolRenderers{},
				},
			)

			if err != nil {
				t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
			}

			result := output.String()
			hasField := strings.Contains(result, `"sessionTimeout":`)
			if hasField != tt.wantField {
				t.Errorf("sessionTimeout field presence = %v, want %v\noutput:\n%s", hasField, tt.wantField, result)
			}

			if tt.wantField && tt.sessionTimeout != "" {
				expected := `"sessionTimeout": "` + tt.sessionTimeout + `"`
				if !strings.Contains(result, expected) {
					t.Errorf("expected %q in output\noutput:\n%s", expected, result)
				}
			}
		})
	}
}

// TestRenderJSONMCPConfig_ToolTimeout verifies that toolTimeout is emitted
// in the gateway JSON section when set on the MCPGatewayRuntimeConfig.
func TestRenderJSONMCPConfig_ToolTimeout(t *testing.T) {
	tests := []struct {
		name        string
		toolTimeout string
		expected    int
		wantField   bool
	}{
		{
			name:        "includes toolTimeout when set",
			toolTimeout: "2m",
			expected:    120,
			wantField:   true,
		},
		{
			name:        "omits toolTimeout when empty",
			toolTimeout: "",
			wantField:   false,
		},
		{
			name:        "includes toolTimeout 30s",
			toolTimeout: "30s",
			expected:    30,
			wantField:   true,
		},
		{
			name:        "rounds fractional toolTimeout to nearest second",
			toolTimeout: "90500ms",
			expected:    91,
			wantField:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Domain:      "localhost",
				AgentID:     "test-api-key",
				ToolTimeout: tt.toolTimeout,
			}

			workflowData := &WorkflowData{
				Name:            "test-workflow",
				FrontmatterHash: "abc123",
			}

			var output strings.Builder
			err := RenderJSONMCPConfig(
				&output,
				map[string]any{},
				[]string{},
				workflowData,
				JSONMCPConfigOptions{
					ConfigPath:    "/tmp/test/mcp-servers.json",
					GatewayConfig: gatewayConfig,
					Renderers:     MCPToolRenderers{},
				},
			)

			if err != nil {
				t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
			}

			result := output.String()
			hasField := strings.Contains(result, `"toolTimeout":`)
			if hasField != tt.wantField {
				t.Errorf("toolTimeout field presence = %v, want %v\noutput:\n%s", hasField, tt.wantField, result)
			}

			if tt.wantField && tt.toolTimeout != "" {
				expected := fmt.Sprintf(`"toolTimeout": %d`, tt.expected)
				if !strings.Contains(result, expected) {
					t.Errorf("expected %q in output\noutput:\n%s", expected, result)
				}
			}
		})
	}
}

// TestRenderJSONMCPConfig_StartupTimeout verifies that startupTimeout is emitted
// in the gateway JSON section when set on the MCPGatewayRuntimeConfig.
func TestRenderJSONMCPConfig_StartupTimeout(t *testing.T) {
	tests := []struct {
		name           string
		startupTimeout int
		wantField      bool
		wantValue      int
	}{
		{
			name:           "emits default startupTimeout of 120",
			startupTimeout: 120,
			wantField:      true,
			wantValue:      120,
		},
		{
			name:           "emits custom startupTimeout of 180",
			startupTimeout: 180,
			wantField:      true,
			wantValue:      180,
		},
		{
			name:           "omits startupTimeout when zero",
			startupTimeout: 0,
			wantField:      false,
		},
		{
			name:           "emits minimum startupTimeout of 1",
			startupTimeout: 1,
			wantField:      true,
			wantValue:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gatewayConfig := &MCPGatewayRuntimeConfig{
				Domain:         "localhost",
				AgentID:        "test-api-key",
				StartupTimeout: tt.startupTimeout,
			}

			workflowData := &WorkflowData{
				Name:            "test-workflow",
				FrontmatterHash: "abc123",
			}

			var output strings.Builder
			err := RenderJSONMCPConfig(
				&output,
				map[string]any{},
				[]string{},
				workflowData,
				JSONMCPConfigOptions{
					ConfigPath:    "/tmp/test/mcp-servers.json",
					GatewayConfig: gatewayConfig,
					Renderers:     MCPToolRenderers{},
				},
			)

			if err != nil {
				t.Fatalf("RenderJSONMCPConfig returned error: %v", err)
			}

			result := output.String()
			hasField := strings.Contains(result, `"startupTimeout":`)
			if hasField != tt.wantField {
				t.Errorf("startupTimeout field presence = %v, want %v\noutput:\n%s", hasField, tt.wantField, result)
			}

			if tt.wantField {
				expected := fmt.Sprintf(`"startupTimeout": %d`, tt.wantValue)
				if !strings.Contains(result, expected) {
					t.Errorf("expected %q in output\noutput:\n%s", expected, result)
				}
			}
		})
	}
}
