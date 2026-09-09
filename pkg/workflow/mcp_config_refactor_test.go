//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestRenderSafeOutputsMCPConfigWithOptions verifies the shared Safe Outputs config helper
// works correctly with both Copilot and non-Copilot engines
func TestRenderSafeOutputsMCPConfigWithOptions(t *testing.T) {
	pinnedGhAwNodeImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)

	tests := []struct {
		name                 string
		isLast               bool
		includeCopilotFields bool
		expectedContent      []string
		unexpectedContent    []string
	}{
		{
			name:                 "Copilot with stdio container and escaped env vars",
			isLast:               true,
			includeCopilotFields: true,
			expectedContent: []string{
				`"safeoutputs": {`,
				`"type": "stdio"`,
				`"container": "` + pinnedGhAwNodeImage + `"`,
				`"${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw"`,
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,
				`"entrypoint": "sh"`,
				`"entrypointArgs": ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`,
				`"env": {`,
				`"GH_AW_SAFE_OUTPUTS_CONFIG_PATH": "\${GH_AW_SAFE_OUTPUTS_CONFIG_PATH}"`,
				`"GITHUB_EVENT_NAME": "\${GITHUB_EVENT_NAME}"`,
				`"GITHUB_EVENT_PATH": "\${GITHUB_EVENT_PATH}"`,
				`              }`,
			},
			unexpectedContent: []string{
				`"url": "http://`,
				`"Authorization":`,
			},
		},
		{
			name:                 "Claude/Custom with stdio container and shell variables",
			isLast:               false,
			includeCopilotFields: false,
			expectedContent: []string{
				`"safeoutputs": {`,
				`"container": "` + pinnedGhAwNodeImage + `"`,
				`"${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw"`,
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,
				`"args": ["-w", "\${GITHUB_WORKSPACE}"]`,
				`"entrypoint": "sh"`,
				`"entrypointArgs": ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`,
				`"env": {`,
				`"GH_AW_SAFE_OUTPUTS": "\${GH_AW_SAFE_OUTPUTS}"`,
				`"RUNNER_TEMP": "\${RUNNER_TEMP}"`,
				`              },`,
			},
			unexpectedContent: []string{
				`"type": "stdio"`,
				`"url": "http://`,
				`"Authorization":`,
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
				`"container": "localhost/gh-aw:dev"`,                          // Dev mode uses locally built image
				`"\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw"`,              // workspace mount (read-write)
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                                  // temp directory mount (read-write)
				`"args": ["--network", "host", "-w", "\${GITHUB_WORKSPACE}"]`, // Network access + working directory
				`"DEBUG": "*"`,                                                // Literal value for debug logging
				`"GITHUB_TOKEN": "\${GITHUB_TOKEN}"`,
				`              },`,
			},
			unexpectedContent: []string{
				`--cmd`,
				`"entrypoint"`,     // Not needed in dev mode - uses container's ENTRYPOINT
				`"entrypointArgs"`, // Not needed in dev mode - uses container's CMD
				`${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro`, // Not needed in dev mode - binary is in image
				`/usr/bin/gh:/usr/bin/gh:ro`,                   // Not needed in dev mode - gh CLI is in image
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
				`"entrypoint": "${RUNNER_TEMP}/gh-aw/gh-aw"`,
				`"entrypointArgs": ["mcp-server", "--validate-actor"]`,
				`"${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`,              // gh-aw binary mount (read-only)
				`"/usr/bin/gh:/usr/bin/gh:ro"`,                                // gh CLI binary mount (read-only)
				`"\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw"`,              // workspace mount (read-write)
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                                  // temp directory mount (read-write)
				`"args": ["--network", "host", "-w", "\${GITHUB_WORKSPACE}"]`, // Network access + working directory
				`"DEBUG": "*"`,
				`"GITHUB_TOKEN": "\${GITHUB_TOKEN}"`,
				`"GITHUB_ACTOR": "\${GITHUB_ACTOR}"`,           // Actor for role-based access control
				`"GITHUB_REPOSITORY": "\${GITHUB_REPOSITORY}"`, // Repository context
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
				`"container": "localhost/gh-aw:dev"`,                          // Dev mode uses locally built image
				`"\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw"`,              // workspace mount (read-write)
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                                  // temp directory mount (read-write)
				`"args": ["--network", "host", "-w", "\${GITHUB_WORKSPACE}"]`, // Network access + working directory
				// Environment variables
				`"DEBUG": "*"`, // Literal value for debug logging
				`"GITHUB_TOKEN": "\${GITHUB_TOKEN}"`,
				`              }`,
			},
			unexpectedContent: []string{
				`"type"`,
				`\\${`,
				`--cmd`,
				`"entrypoint"`,     // Not needed in dev mode - uses container's ENTRYPOINT
				`"entrypointArgs"`, // Not needed in dev mode - uses container's CMD
				`${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro`, // Not needed in dev mode - binary is in image
				`/usr/bin/gh:/usr/bin/gh:ro`,                   // Not needed in dev mode - gh CLI is in image
				// Verify GitHub expressions are NOT in the output (security fix)
				`${{ secrets.`,
				`"command":`, // Should NOT use command - must use container
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			renderAgenticWorkflowsMCPConfigWithOptions(&output, tt.isLast, tt.includeCopilotFields, tt.actionMode, nil, nil)

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

// TestRenderSafeOutputsMCPConfigTOML verifies the Safe Outputs TOML format via the production MCPConfigRendererUnified path
func TestRenderSafeOutputsMCPConfigTOML(t *testing.T) {
	var output strings.Builder

	renderer := NewMCPConfigRenderer(MCPRendererOptions{
		Format: "toml",
		IsLast: true,
	})
	renderer.RenderSafeOutputsMCP(&output, nil)

	result := output.String()

	expectedContent := []string{
		`[mcp_servers.safeoutputs]`,
		`container = "` + resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil) + `"`,
		`mounts = ["\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw", "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "/tmp/gh-aw:/tmp/gh-aw:rw"]`,
		`args = ["-w", "$GITHUB_WORKSPACE"]`,
		`entrypoint = "sh"`,
		`entrypointArgs = ["-c", "sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh"]`,
		`env_vars = ["DEBUG", "DEFAULT_BRANCH", "GH_AW_ASSETS_ALLOWED_EXTS", "GH_AW_ASSETS_BRANCH", "GH_AW_ASSETS_MAX_SIZE_KB", "GH_AW_MCP_LOG_DIR", "GH_AW_SAFE_OUTPUTS", "GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH", "GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST", "GH_AW_PR_HEAD_BASE_BRANCH", "GH_AW_PR_HEAD_BASE_SHA", "GH_AW_PR_HEAD_BASE_REPO", "GH_AW_PR_HEAD_BASE_PR_NUMBER", "GH_AW_PR_HEAD_BASE_REF", "GH_AW_PR_HEAD_REPO", "GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH", "GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_TOKEN", "GITHUB_WORKSPACE", "RUNNER_TEMP"]`,
	}

	unexpectedContent := []string{
		`container = "node:lts-alpine"`,
		`entrypoint = "node"`,
		`entrypointArgs = ["${RUNNER_TEMP}/gh-aw/safeoutputs/mcp-server.cjs"]`,
		`type = "http"`,
		`url = "http://`,
		`Authorization = `,
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

// TestRenderSafeOutputsMCPConfigTOMLStableAcrossSandboxModes verifies the stdio container
// rendering does not vary by sandbox host rewriting modes.
func TestRenderSafeOutputsMCPConfigTOMLStableAcrossSandboxModes(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
	}{
		{
			name:         "nil workflowData",
			workflowData: nil,
		},
		{
			name: "agent enabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Disabled: false},
				},
			},
		},
		{
			name: "agent disabled",
			workflowData: &WorkflowData{
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Disabled: true},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				Format: "toml",
				IsLast: true,
			})
			renderer.RenderSafeOutputsMCP(&output, tt.workflowData)
			result := output.String()
			if !strings.Contains(result, `container = "`+resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, tt.workflowData)+`"`) {
				t.Errorf("Expected gh-aw node container not found in output:\n%s", result)
			}
			if !strings.Contains(result, `mounts = ["\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw", "${RUNNER_TEMP}/gh-aw/safeoutputs:${RUNNER_TEMP}/gh-aw/safeoutputs:rw", "/tmp/gh-aw:/tmp/gh-aw:rw"]`) {
				t.Errorf("Expected stable safe-outputs mounts not found in output:\n%s", result)
			}
			if strings.Contains(result, "host.docker.internal") || strings.Contains(result, "localhost") {
				t.Errorf("Did not expect HTTP host rewriting in stdio safe-outputs config:\n%s", result)
			}
		})
	}
}

// TestRenderAgenticWorkflowsMCPConfigTOML verifies the Agentic Workflows TOML format via the
// production MCPConfigRendererUnified path.
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
				`"\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw"`, // workspace mount
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                     // temp directory mount
			},
			unexpectedContent: []string{
				`--cmd`,
				`entrypoint =`,     // Not needed in dev mode - uses container's ENTRYPOINT
				`entrypointArgs =`, // Not needed in dev mode - uses container's CMD
				`${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro`, // Not needed in dev mode
				`/usr/bin/gh:/usr/bin/gh:ro`,                   // Not needed in dev mode
			},
		},
		{
			name:                 "release mode with entrypoint and mounts",
			actionMode:           ActionModeRelease,
			expectedContainer:    `container = "alpine:latest"`,
			shouldHaveEntrypoint: true,
			expectedMounts: []string{
				`entrypoint = "${RUNNER_TEMP}/gh-aw/gh-aw"`,           // Entrypoint needed in release mode
				`entrypointArgs = ["mcp-server", "--validate-actor"]`, // EntrypointArgs needed in release mode with validate-actor flag
				`"${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`,      // gh-aw binary mount
				`"/usr/bin/gh:/usr/bin/gh:ro"`,                        // gh CLI binary mount
				`"\${GITHUB_WORKSPACE}:\${GITHUB_WORKSPACE}:rw"`,      // workspace mount
				`"/tmp/gh-aw:/tmp/gh-aw:rw"`,                          // temp directory mount
			},
			unexpectedContent: []string{
				`--cmd`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder

			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				Format:     "toml",
				ActionMode: tt.actionMode,
			})
			renderer.RenderAgenticWorkflowsMCP(&output)

			result := output.String()

			expectedContent := []string{
				`[mcp_servers.agenticworkflows]`,
				tt.expectedContainer,
				`env_vars = ["DEBUG", "GH_TOKEN", "GITHUB_TOKEN", "GITHUB_ACTOR", "GITHUB_REPOSITORY"]`,
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
