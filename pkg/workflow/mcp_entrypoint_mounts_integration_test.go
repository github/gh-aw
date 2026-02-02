//go:build integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPServerEntrypointIntegration tests that entrypoint is correctly transformed to docker --entrypoint
func TestMCPServerEntrypointIntegration(t *testing.T) {
	config := map[string]any{
		"container":  "ghcr.io/example/server:latest",
		"entrypoint": "/custom/entrypoint.sh",
		"entrypointArgs": []any{
			"--verbose",
			"--port", "8080",
		},
		"env": map[string]any{
			"API_KEY": "test-key",
		},
	}

	result, err := parser.ParseMCPConfig("test-server", config, config)
	require.NoError(t, err, "Failed to parse MCP config")

	// Verify transformation to docker command
	assert.Equal(t, "docker", result.Command, "Command should be docker")
	assert.Contains(t, result.Args, "run", "Should contain run")
	assert.Contains(t, result.Args, "--rm", "Should contain --rm")
	assert.Contains(t, result.Args, "-i", "Should contain -i")
	assert.Contains(t, result.Args, "-e", "Should contain -e for env vars")
	assert.Contains(t, result.Args, "API_KEY", "Should contain env var name")
	assert.Contains(t, result.Args, "--entrypoint", "Should contain --entrypoint flag")
	assert.Contains(t, result.Args, "/custom/entrypoint.sh", "Should contain entrypoint value")
	assert.Contains(t, result.Args, "ghcr.io/example/server:latest", "Should contain container image")
	assert.Contains(t, result.Args, "--verbose", "Should contain entrypoint arg")
	assert.Contains(t, result.Args, "--port", "Should contain entrypoint arg")
	assert.Contains(t, result.Args, "8080", "Should contain entrypoint arg value")

	// Verify order: --entrypoint comes before container image, entrypointArgs come after
	entrypointIdx := -1
	containerIdx := -1
	verboseIdx := -1
	for i, arg := range result.Args {
		if arg == "--entrypoint" {
			entrypointIdx = i
		}
		if arg == "ghcr.io/example/server:latest" {
			containerIdx = i
		}
		if arg == "--verbose" {
			verboseIdx = i
		}
	}

	assert.Positive(t, entrypointIdx, "--entrypoint should be in args")
	assert.Greater(t, containerIdx, entrypointIdx, "Container should come after --entrypoint")
	assert.Greater(t, verboseIdx, containerIdx, "EntrypointArgs should come after container")

	// Note: parser.ParseMCPConfig preserves the Container/Entrypoint/EntrypointArgs fields
	// even after transforming them to docker command. This is different from workflow.ParseMCPConfigFromMap
	// which clears them. Both behaviors are acceptable - the important thing is the docker command is correct.
}

// TestMCPServerMountsIntegration tests that mounts are correctly transformed to docker -v flags
func TestMCPServerMountsIntegration(t *testing.T) {
	config := map[string]any{
		"container": "ghcr.io/example/server:latest",
		"mounts": []any{
			"/host/data:/container/data:ro",
			"/host/config:/container/config:rw",
			"/tmp/cache:/app/cache:rw",
		},
		"env": map[string]any{
			"LOG_LEVEL": "debug",
		},
	}

	result, err := parser.ParseMCPConfig("test-server", config, config)
	require.NoError(t, err, "Failed to parse MCP config")

	// Verify transformation to docker command
	assert.Equal(t, "docker", result.Command, "Command should be docker")

	// Check that all mounts are present as -v flags (they should be sorted)
	expectedMounts := []string{
		"/host/config:/container/config:rw",
		"/host/data:/container/data:ro",
		"/tmp/cache:/app/cache:rw",
	}

	// Count -v flags
	vFlagCount := 0
	for i, arg := range result.Args {
		if arg == "-v" {
			vFlagCount++
			// Check that the next arg is one of our expected mounts
			if i+1 < len(result.Args) {
				mountValue := result.Args[i+1]
				assert.Contains(t, expectedMounts, mountValue, "Mount value should be one of expected mounts")
			}
		}
	}

	assert.Equal(t, 3, vFlagCount, "Should have 3 -v flags for 3 mounts")

	// Verify mounts come after env vars but before container image
	firstVIdx := -1
	containerIdx := -1
	for i, arg := range result.Args {
		if arg == "-v" && firstVIdx == -1 {
			firstVIdx = i
		}
		if strings.HasPrefix(arg, "ghcr.io/example/server") {
			containerIdx = i
		}
	}

	assert.Positive(t, firstVIdx, "-v should be in args")
	assert.Greater(t, containerIdx, firstVIdx, "Container should come after -v flags")

	// Note: parser.ParseMCPConfig preserves the Container/Mounts fields
	// even after transforming them to docker command. This is different from workflow.ParseMCPConfigFromMap
	// which clears them. Both behaviors are acceptable - the important thing is the docker command is correct.
}

// TestMCPServerEntrypointAndMountsCombined tests both entrypoint and mounts together
func TestMCPServerEntrypointAndMountsCombined(t *testing.T) {
	config := map[string]any{
		"container":  "ghcr.io/example/server:v1.2.3",
		"entrypoint": "/bin/bash",
		"entrypointArgs": []any{
			"-c",
			"exec /app/start.sh",
		},
		"mounts": []any{
			"/host/data:/data:ro",
		},
		"env": map[string]any{
			"DEBUG": "true",
		},
	}

	result, err := parser.ParseMCPConfig("combined-server", config, config)
	require.NoError(t, err, "Failed to parse MCP config")

	// Verify order: run -> -e env -> -v mount -> --entrypoint -> image -> entrypointArgs
	assert.Equal(t, "docker", result.Command)

	// Find indices
	runIdx := -1
	eIdx := -1
	vIdx := -1
	entrypointIdx := -1
	containerIdx := -1
	bashCIdx := -1

	for i, arg := range result.Args {
		switch {
		case arg == "run" && runIdx == -1:
			runIdx = i
		case arg == "-e" && eIdx == -1:
			eIdx = i
		case arg == "-v" && vIdx == -1:
			vIdx = i
		case arg == "--entrypoint" && entrypointIdx == -1:
			entrypointIdx = i
		case strings.HasPrefix(arg, "ghcr.io/example/server") && containerIdx == -1:
			containerIdx = i
		case arg == "-c" && bashCIdx == -1:
			bashCIdx = i
		}
	}

	// Verify correct ordering
	assert.Greater(t, eIdx, runIdx, "-e should come after run")
	assert.Greater(t, vIdx, eIdx, "-v should come after -e")
	assert.Greater(t, entrypointIdx, vIdx, "--entrypoint should come after -v")
	assert.Greater(t, containerIdx, entrypointIdx, "container should come after --entrypoint")
	assert.Greater(t, bashCIdx, containerIdx, "entrypointArgs should come after container")
}

// TestMCPServerWithoutEntrypointOrMounts tests that servers without entrypoint/mounts work correctly
func TestMCPServerWithoutEntrypointOrMounts(t *testing.T) {
	config := map[string]any{
		"container": "ghcr.io/example/simple:latest",
		"entrypointArgs": []any{
			"--config", "/etc/config.json",
		},
	}

	result, err := parser.ParseMCPConfig("simple-server", config, config)
	require.NoError(t, err, "Failed to parse MCP config")

	// Should not have --entrypoint flag
	assert.NotContains(t, result.Args, "--entrypoint", "Should not have --entrypoint when entrypoint not specified")

	// Should not have -v flags
	vCount := 0
	for _, arg := range result.Args {
		if arg == "-v" {
			vCount++
		}
	}
	assert.Equal(t, 0, vCount, "Should not have -v flags when mounts not specified")

	// Should still have entrypointArgs after container
	assert.Contains(t, result.Args, "--config", "Should contain entrypoint arg")
	assert.Contains(t, result.Args, "/etc/config.json", "Should contain entrypoint arg value")
}
