//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestRenderSafeOutputsMCPConfigWithOptions verifies the shared Safe Outputs config helper
// works correctly with both Copilot and non-Copilot engines
func TestRenderSafeOutputsMCPConfigWithOptions(t *testing.T) {
	tests := []struct {
		name                 string
		isLast               bool
		includeCopilotFields bool
		expectedContent      []string
		unexpectedContent    []string
	}{
		{
			name:                 "Copilot with HTTP transport and escaped API key",
			isLast:               true,
			includeCopilotFields: true,
			expectedContent: []string{
				`"safeoutputs": {`,
				`"type": "http"`,
				`"url": "http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT"`,
				`"headers": {`,
				`"Authorization": "\${GH_AW_SAFE_OUTPUTS_API_KEY}"`,
				`              }`,
			},
			unexpectedContent: []string{
				`"container"`,
				`"entrypoint"`,
				`"entrypointArgs"`,
				`"env": {`,
				`"stdio"`,
			},
		},
		{
			name:                 "Claude/Custom with HTTP transport and shell variable",
			isLast:               false,
			includeCopilotFields: false,
			expectedContent: []string{
				`"safeoutputs": {`,
				`"type": "http"`,
				`"url": "http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT"`,
				`"headers": {`,
				`"Authorization": "$GH_AW_SAFE_OUTPUTS_API_KEY"`,
				`              },`,
			},
			unexpectedContent: []string{
				`"container"`,
				`"entrypoint"`,
				`"entrypointArgs"`,
				`"env": {`,
				`"stdio"`,
				`\\${`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			renderSafeOutputsMCPConfigWithOptions(&output, tt.isLast, tt.includeCopilotFields, nil)

			result := output.String()

			// Check expected content
			for _, expected := range tt.expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
				}
			}

			// Check unexpected content
			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
				}
			}
		})
	}
}

// TestRenderAgenticWorkflowsMCPConfigWithOptions verifies the shared Agentic Workflows config helper
// works correctly with both Copilot and non-Copilot engines
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
func TestRenderAgenticWorkflowsMCPConfigWithOptions(t *testing.T) {
	tests := []struct {
		name                 string
		isLast               bool
		includeCopilotFields bool
		expectedContent      []string
		unexpectedContent    []string
	}{
		{
			name:                 "Copilot with type and escaped env vars",
			isLast:               false,
			includeCopilotFields: true,
			expectedContent: []string{
				`"agentic_workflows": {`,
				`"type": "stdio"`,
				`"container": "alpine:latest"`,
				`"entrypoint": "/opt/gh-aw/gh-aw"`,
				`"entrypointArgs": ["mcp-server"]`,
				`"/opt/gh-aw:/opt/gh-aw:ro"`,                           // gh-aw binary mount (read-only)
				`"${{ github.workspace }}:${{ github.workspace }}:rw"`, // workspace mount (read-write)
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                           // temp directory mount (read-write)
				`"GITHUB_TOKEN": "\${GITHUB_TOKEN}"`,
				`              },`,
			},
			unexpectedContent: []string{
				`${{ secrets.`,
				`"command":`, // Should NOT use command - must use container
			},
		},
		{
			name:                 "Claude/Custom without type, with shell env vars",
			isLast:               true,
			includeCopilotFields: false,
			expectedContent: []string{
				`"agentic_workflows": {`,
				`"container": "alpine:latest"`,
				`"entrypoint": "/opt/gh-aw/gh-aw"`,
				`"entrypointArgs": ["mcp-server"]`,
				`"/opt/gh-aw:/opt/gh-aw:ro"`,                           // gh-aw binary mount (read-only)
				`"${{ github.workspace }}:${{ github.workspace }}:rw"`, // workspace mount (read-write)
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                           // temp directory mount (read-write)
				// Security fix: Now uses shell variable instead of GitHub secret expression
				`"GITHUB_TOKEN": "$GITHUB_TOKEN"`,
				`              }`,
			},
			unexpectedContent: []string{
				`"type"`,
				`\\${`,
				// Verify GitHub expressions are NOT in the output (security fix)
				`${{ secrets.`,
				`"command":`, // Should NOT use command - must use container
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			renderAgenticWorkflowsMCPConfigWithOptions(&output, tt.isLast, tt.includeCopilotFields)

			result := output.String()

			// Check expected content
			for _, expected := range tt.expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
				}
			}

			// Check unexpected content
			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
				}
			}
		})
	}
}

// TestRenderPlaywrightMCPConfigTOML verifies the TOML format helper for Codex engine
// TestRenderSafeOutputsMCPConfigTOML verifies the Safe Outputs TOML format helper
func TestRenderSafeOutputsMCPConfigTOML(t *testing.T) {
	var output strings.Builder

	renderSafeOutputsMCPConfigTOML(&output)

	result := output.String()

	expectedContent := []string{
		`[mcp_servers.safeoutputs]`,
		`type = "http"`,
		`url = "http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT"`,
		`[mcp_servers.safeoutputs.headers]`,
		`Authorization = "$GH_AW_SAFE_OUTPUTS_API_KEY"`,
	}

	unexpectedContent := []string{
		`container = "node:lts-alpine"`,
		`entrypoint = "node"`,
		`entrypointArgs = ["/opt/gh-aw/safeoutputs/mcp-server.cjs"]`,
		`mounts =`,
		`env_vars =`,
		`stdio`,
	}

	for _, expected := range expectedContent {
		if !strings.Contains(result, expected) {
			t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
		}
	}

	for _, unexpected := range unexpectedContent {
		if strings.Contains(result, unexpected) {
			t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
		}
	}
}

// TestRenderAgenticWorkflowsMCPConfigTOML verifies the Agentic Workflows TOML format helper
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
func TestRenderAgenticWorkflowsMCPConfigTOML(t *testing.T) {
	var output strings.Builder

	renderAgenticWorkflowsMCPConfigTOML(&output)

	result := output.String()

	expectedContent := []string{
		`[mcp_servers.agentic_workflows]`,
		`container = "alpine:latest"`,
		`entrypoint = "/opt/gh-aw/gh-aw"`,
		`entrypointArgs = ["mcp-server"]`,
		`"/opt/gh-aw:/opt/gh-aw:ro"`,                           // gh-aw binary mount (read-only)
		`"${{ github.workspace }}:${{ github.workspace }}:rw"`, // workspace mount (read-write)
		`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                           // temp directory mount (read-write)
		`env_vars = ["GITHUB_TOKEN"]`,
	}

	unexpectedContent := []string{
		`env = {`,        // Should use env_vars instead
		`command = "gh"`, // Should NOT use command - must use container
		`"aw"`,           // Old arg format
		`args = [`,       // Old args format
	}

	for _, expected := range expectedContent {
		if !strings.Contains(result, expected) {
			t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
		}
	}

	for _, unexpected := range unexpectedContent {
		if strings.Contains(result, unexpected) {
			t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
		}
	}
}
