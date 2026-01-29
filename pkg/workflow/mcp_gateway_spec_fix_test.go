//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPServerEntrypointField tests that MCP servers support optional entrypoint field
func TestMCPServerEntrypointField(t *testing.T) {
	tests := []struct {
		name                 string
		mcpConfig            map[string]any
		expectEntrypoint     string
		expectEntrypointArgs []string
		expectError          bool
	}{
		{
			name: "entrypoint with entrypointArgs",
			mcpConfig: map[string]any{
				"container":      "ghcr.io/example/server:latest",
				"entrypoint":     "/custom/entrypoint.sh",
				"entrypointArgs": []any{"--verbose", "--port", "8080"},
			},
			expectEntrypoint:     "/custom/entrypoint.sh",
			expectEntrypointArgs: []string{"--verbose", "--port", "8080"},
			expectError:          false,
		},
		{
			name: "entrypoint without entrypointArgs",
			mcpConfig: map[string]any{
				"container":  "ghcr.io/example/server:latest",
				"entrypoint": "/bin/sh",
			},
			expectEntrypoint:     "/bin/sh",
			expectEntrypointArgs: nil,
			expectError:          false,
		},
		{
			name: "entrypointArgs without entrypoint (existing behavior)",
			mcpConfig: map[string]any{
				"container":      "ghcr.io/example/server:latest",
				"entrypointArgs": []any{"--config", "/etc/config.json"},
			},
			expectEntrypoint:     "",
			expectEntrypointArgs: []string{"--config", "/etc/config.json"},
			expectError:          false,
		},
		{
			name: "no entrypoint or entrypointArgs",
			mcpConfig: map[string]any{
				"container": "ghcr.io/example/server:latest",
			},
			expectEntrypoint:     "",
			expectEntrypointArgs: nil,
			expectError:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := &Compiler{}
			extracted := compiler.extractMCPGatewayConfig(tt.mcpConfig)

			if tt.expectError {
				// For now, we don't expect errors, but this is for future validation
				return
			}

			require.NotNil(t, extracted, "Extraction should not return nil")

			// Verify entrypoint extraction
			assert.Equal(t, tt.expectEntrypoint, extracted.Entrypoint, "Entrypoint mismatch")
			assert.ElementsMatch(t, tt.expectEntrypointArgs, extracted.EntrypointArgs, "EntrypointArgs mismatch")
		})
	}
}

// TestMCPServerMountsInServerConfig tests that mounts can be configured per MCP server
func TestMCPServerMountsInServerConfig(t *testing.T) {
	tests := []struct {
		name         string
		mcpConfig    map[string]any
		expectMounts []string
		expectError  bool
	}{
		{
			name: "mcp server with mounts",
			mcpConfig: map[string]any{
				"container": "ghcr.io/example/server:latest",
				"mounts": []any{
					"/host/data:/container/data:ro",
					"/host/config:/container/config:rw",
				},
			},
			expectMounts: []string{"/host/data:/container/data:ro", "/host/config:/container/config:rw"},
			expectError:  false,
		},
		{
			name: "mcp server without mounts",
			mcpConfig: map[string]any{
				"container": "ghcr.io/example/simple:latest",
			},
			expectMounts: nil,
			expectError:  false,
		},
		{
			name: "mcp server with single mount",
			mcpConfig: map[string]any{
				"container": "ghcr.io/example/server:latest",
				"mounts": []any{
					"/tmp/data:/app/data:ro",
				},
			},
			expectMounts: []string{"/tmp/data:/app/data:ro"},
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := &Compiler{}
			extracted := compiler.extractMCPGatewayConfig(tt.mcpConfig)

			if tt.expectError {
				// For now, we don't expect errors, but this is for future validation
				return
			}

			require.NotNil(t, extracted, "Extraction should not return nil")

			// Verify mounts extraction
			assert.ElementsMatch(t, tt.expectMounts, extracted.Mounts, "Mounts mismatch")
		})
	}
}

// TestMCPServerEntrypointAndMountsCombined tests entrypoint and mounts together in extraction
func TestMCPServerEntrypointAndMountsCombinedExtraction(t *testing.T) {
	mcpConfig := map[string]any{
		"container":      "ghcr.io/example/server:latest",
		"entrypoint":     "/usr/bin/custom-start",
		"entrypointArgs": []any{"--config", "/etc/app.conf"},
		"mounts": []any{
			"/var/data:/app/data:rw",
			"/etc/secrets:/app/secrets:ro",
		},
	}

	compiler := &Compiler{}
	extracted := compiler.extractMCPGatewayConfig(mcpConfig)

	require.NotNil(t, extracted, "Extraction should not return nil")

	// Verify all fields are extracted correctly
	assert.Equal(t, "/usr/bin/custom-start", extracted.Entrypoint, "Entrypoint mismatch")
	assert.ElementsMatch(t, []string{"--config", "/etc/app.conf"}, extracted.EntrypointArgs, "EntrypointArgs mismatch")
	assert.ElementsMatch(t, []string{"/var/data:/app/data:rw", "/etc/secrets:/app/secrets:ro"}, extracted.Mounts, "Mounts mismatch")
}
