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
		actionMode           ActionMode
		expectedContent      []string
		unexpectedContent    []string
	}{
		{
			name:                 "Copilot dev mode without entrypoint/args",
			isLast:               false,
			includeCopilotFields: true,
			actionMode:           ActionModeDev,
			expectedContent: []string{
				`"agenticworkflows": {`,
				`"type": "stdio"`,
				`"container": "localhost/gh-aw:dev"`,                             // Dev mode uses locally built image
				`"${{ github.workspace }}:${{ github.workspace }}:rw"`,           // workspace mount (read-write)
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                                     // temp directory mount (read-write)
				`"args": ["--network", "host", "-w", "${{ github.workspace }}"]`, // Network access + working directory
				`"DEBUG": "*"`,                                                   // Literal value for debug logging
				`"GITHUB_TOKEN": "\${GITHUB_TOKEN}"`,
				`              },`,
			},
			unexpectedContent: []string{
				`--cmd`,
				`"entrypoint"`,               // Not needed in dev mode - uses container's ENTRYPOINT
				`"entrypointArgs"`,           // Not needed in dev mode - uses container's CMD
				`/opt/gh-aw:/opt/gh-aw:ro`,   // Not needed in dev mode - binary is in image
				`/usr/bin/gh:/usr/bin/gh:ro`, // Not needed in dev mode - gh CLI is in image
				`${{ secrets.`,
				`"command":`, // Should NOT use command - must use container
			},
		},
		{
			name:                 "Copilot release mode with entrypoint/args",
			isLast:               false,
			includeCopilotFields: true,
			actionMode:           ActionModeRelease,
			expectedContent: []string{
				`"agenticworkflows": {`,
				`"type": "stdio"`,
				`"container": "alpine:latest"`,
				`"entrypoint": "/opt/gh-aw/gh-aw"`,
				`"entrypointArgs": ["mcp-server"]`,
				`"/opt/gh-aw:/opt/gh-aw:ro"`,                                     // gh-aw binary mount (read-only)
				`"/usr/bin/gh:/usr/bin/gh:ro"`,                                   // gh CLI binary mount (read-only)
				`"${{ github.workspace }}:${{ github.workspace }}:rw"`,           // workspace mount (read-write)
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                                     // temp directory mount (read-write)
				`"args": ["--network", "host", "-w", "${{ github.workspace }}"]`, // Network access + working directory
				`"DEBUG": "*"`,
				`"GITHUB_TOKEN": "\${GITHUB_TOKEN}"`,
				`              },`,
			},
			unexpectedContent: []string{
				`--cmd`,
				`${{ secrets.`,
				`"command":`, // Should NOT use command - must use container
			},
		},
		{
			name:                 "Claude/Custom dev mode without entrypoint/args",
			isLast:               true,
			includeCopilotFields: false,
			actionMode:           ActionModeDev,
			expectedContent: []string{
				`"agenticworkflows": {`,
				`"container": "localhost/gh-aw:dev"`,                             // Dev mode uses locally built image
				`"${{ github.workspace }}:${{ github.workspace }}:rw"`,           // workspace mount (read-write)
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                                     // temp directory mount (read-write)
				`"args": ["--network", "host", "-w", "${{ github.workspace }}"]`, // Network access + working directory
				// Environment variables
				`"DEBUG": "*"`, // Literal value for debug logging
				`"GITHUB_TOKEN": "$GITHUB_TOKEN"`,
				`              }`,
			},
			unexpectedContent: []string{
				`"type"`,
				`\\${`,
				`--cmd`,
				`"entrypoint"`,               // Not needed in dev mode - uses container's ENTRYPOINT
				`"entrypointArgs"`,           // Not needed in dev mode - uses container's CMD
				`/opt/gh-aw:/opt/gh-aw:ro`,   // Not needed in dev mode - binary is in image
				`/usr/bin/gh:/usr/bin/gh:ro`, // Not needed in dev mode - gh CLI is in image
				// Verify GitHub expressions are NOT in the output (security fix)
				`${{ secrets.`,
				`"command":`, // Should NOT use command - must use container
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			renderAgenticWorkflowsMCPConfigWithOptions(&output, tt.isLast, tt.includeCopilotFields, tt.actionMode)

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
	tests := []struct {
		name                 string
		actionMode           ActionMode
		expectedContainer    string
		shouldHaveEntrypoint bool
		expectedMounts       []string
		unexpectedContent    []string
	}{
		{
			name:                 "dev mode without entrypoint/args",
			actionMode:           ActionModeDev,
			expectedContainer:    `container = "localhost/gh-aw:dev"`,
			shouldHaveEntrypoint: false, // Dev mode uses container's default ENTRYPOINT
			expectedMounts: []string{
				`"${{ github.workspace }}:${{ github.workspace }}:rw"`, // workspace mount
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                           // temp directory mount
			},
			unexpectedContent: []string{
				`--cmd`,
				`entrypoint =`,               // Not needed in dev mode - uses container's ENTRYPOINT
				`entrypointArgs =`,           // Not needed in dev mode - uses container's CMD
				`/opt/gh-aw:/opt/gh-aw:ro`,   // Not needed in dev mode
				`/usr/bin/gh:/usr/bin/gh:ro`, // Not needed in dev mode
			},
		},
		{
			name:                 "release mode with entrypoint and mounts",
			actionMode:           ActionModeRelease,
			expectedContainer:    `container = "alpine:latest"`,
			shouldHaveEntrypoint: true,
			expectedMounts: []string{
				`entrypoint = "/opt/gh-aw/gh-aw"`,                      // Entrypoint needed in release mode
				`entrypointArgs = ["mcp-server"]`,                      // EntrypointArgs needed in release mode
				`"/opt/gh-aw:/opt/gh-aw:ro"`,                           // gh-aw binary mount
				`"/usr/bin/gh:/usr/bin/gh:ro"`,                         // gh CLI binary mount
				`"${{ github.workspace }}:${{ github.workspace }}:rw"`, // workspace mount
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                           // temp directory mount
			},
			unexpectedContent: []string{
				`--cmd`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			renderAgenticWorkflowsMCPConfigTOML(&output, tt.actionMode)

			result := output.String()

			expectedContent := []string{
				`[mcp_servers.agenticworkflows]`,
				tt.expectedContainer,
				`args = ["--network", "host", "-w", "${{ github.workspace }}"]`, // Network access + working directory
				`env_vars = ["DEBUG", "GITHUB_TOKEN"]`,
			}
			expectedContent = append(expectedContent, tt.expectedMounts...)

			for _, expected := range expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
				}
			}

			// Verify entrypoint presence/absence based on shouldHaveEntrypoint flag
			hasEntrypoint := strings.Contains(result, `entrypoint =`)
			if tt.shouldHaveEntrypoint && !hasEntrypoint {
				t.Errorf("Expected entrypoint field in %s mode, but not found", tt.actionMode)
			}
			if !tt.shouldHaveEntrypoint && hasEntrypoint {
				t.Errorf("Did not expect entrypoint field in %s mode (uses container's ENTRYPOINT)", tt.actionMode)
			}

			// Verify entrypointArgs presence/absence
			hasEntrypointArgs := strings.Contains(result, `entrypointArgs =`)
			if tt.shouldHaveEntrypoint && !hasEntrypointArgs {
				t.Errorf("Expected entrypointArgs field in %s mode, but not found", tt.actionMode)
			}
			if !tt.shouldHaveEntrypoint && hasEntrypointArgs {
				t.Errorf("Did not expect entrypointArgs field in %s mode (uses container's CMD)", tt.actionMode)
			}

			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
				}
			}
		})
	}
}
