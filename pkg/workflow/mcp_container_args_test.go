//go:build !integration

package workflow

import (
	"testing"
)

func TestContainerWithCustomArgs(t *testing.T) {
	// Test that custom args are preserved when using container field
	config := map[string]any{
		"container": "test",
		"version":   "latest",
		"args":      []any{"-v", "/tmp:/tmp:ro", "-w", "/tmp"},
		"env": map[string]any{
			"TEST_VAR": "value",
		},
		"allowed": []any{"*"},
	}

	result, err := getMCPConfig(config, "test-tool")
	if err != nil {
		t.Fatalf("getMCPConfig failed: %v", err)
	}

	// Check that container is set (MCP Gateway format)
	expectedContainer := "test:latest" // version should be appended
	if result.Container != expectedContainer {
		t.Errorf("Expected container '%s', got '%s'", expectedContainer, result.Container)
	}

	// Check that custom Docker runtime args are preserved
	expectedArgs := []string{"-v", "/tmp:/tmp:ro", "-w", "/tmp"}
	if len(result.Args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d: %v", len(expectedArgs), len(result.Args), result.Args)
	}

	// Check specific args
	hasVolume := false
	hasWorkdir := false
	for i, arg := range result.Args {
		if arg == "-v" && i+1 < len(result.Args) && result.Args[i+1] == "/tmp:/tmp:ro" {
			hasVolume = true
		}
		if arg == "-w" && i+1 < len(result.Args) && result.Args[i+1] == "/tmp" {
			hasWorkdir = true
		}
	}

	if !hasVolume {
		t.Error("Expected volume mount '-v /tmp:/tmp:ro' in args")
	}
	if !hasWorkdir {
		t.Error("Expected working directory '-w /tmp' in args")
	}
}

func TestContainerWithoutCustomArgs(t *testing.T) {
	// Test that container works without custom args (existing behavior)
	config := map[string]any{
		"container": "test:latest",
		"env": map[string]any{
			"TEST_VAR": "value",
		},
		"allowed": []any{"*"},
	}

	result, err := getMCPConfig(config, "test-tool")
	if err != nil {
		t.Fatalf("getMCPConfig failed: %v", err)
	}

	// Check that container is set (MCP Gateway format)
	if result.Container != "test:latest" {
		t.Errorf("Expected container 'test:latest', got '%s'", result.Container)
	}

	// Check that args are empty (no custom args)
	if len(result.Args) != 0 {
		t.Errorf("Expected 0 args, got %d: %v", len(result.Args), result.Args)
	}
}

func TestContainerWithVersionField(t *testing.T) {
	// Test that version field properly appends to container
	config := map[string]any{
		"container": "ghcr.io/test/image",
		"version":   "v1.2.3",
		"env": map[string]any{
			"TEST_VAR": "value",
		},
		"allowed": []any{"*"},
	}

	result, err := getMCPConfig(config, "test-tool")
	if err != nil {
		t.Fatalf("getMCPConfig failed: %v", err)
	}

	// Check that container includes the version
	expectedContainer := "ghcr.io/test/image:v1.2.3"
	if result.Container != expectedContainer {
		t.Errorf("Expected container '%s', got '%s'", expectedContainer, result.Container)
	}
}

func TestContainerWithEntrypointArgs(t *testing.T) {
	// Test that entrypointArgs are preserved in MCP Gateway format
	config := map[string]any{
		"container":      "test-image",
		"version":        "latest",
		"entrypointArgs": []any{"--config", "/app/config.json", "--verbose"},
		"env": map[string]any{
			"TEST_VAR": "value",
		},
		"allowed": []any{"*"},
	}

	result, err := getMCPConfig(config, "test-tool")
	if err != nil {
		t.Fatalf("getMCPConfig failed: %v", err)
	}

	// Check that container is set with version
	expectedContainer := "test-image:latest"
	if result.Container != expectedContainer {
		t.Errorf("Expected container '%s', got '%s'", expectedContainer, result.Container)
	}

	// Check that entrypointArgs are set
	expectedEntrypointArgs := []string{"--config", "/app/config.json", "--verbose"}
	if len(result.EntrypointArgs) != len(expectedEntrypointArgs) {
		t.Errorf("Expected %d entrypointArgs, got %d: %v", len(expectedEntrypointArgs), len(result.EntrypointArgs), result.EntrypointArgs)
	}

	// Verify each entrypoint arg
	for i, expectedArg := range expectedEntrypointArgs {
		if i >= len(result.EntrypointArgs) {
			t.Errorf("Missing entrypoint arg at index %d: expected '%s'", i, expectedArg)
			continue
		}
		if result.EntrypointArgs[i] != expectedArg {
			t.Errorf("Entrypoint arg %d: expected '%s', got '%s'", i, expectedArg, result.EntrypointArgs[i])
		}
	}
}

func TestContainerWithArgsAndEntrypointArgs(t *testing.T) {
	// Test that both args (before container) and entrypointArgs (after container) work together
	config := map[string]any{
		"container":      "test-image",
		"version":        "v1.0",
		"args":           []any{"-v", "/host:/container"},
		"entrypointArgs": []any{"serve", "--port", "8080"},
		"env": map[string]any{
			"ENV_VAR": "value",
		},
		"allowed": []any{"*"},
	}

	result, err := getMCPConfig(config, "test-tool")
	if err != nil {
		t.Fatalf("getMCPConfig failed: %v", err)
	}

	// Check that container is set with version
	expectedContainer := "test-image:v1.0"
	if result.Container != expectedContainer {
		t.Errorf("Expected container '%s', got '%s'", expectedContainer, result.Container)
	}

	// Check that Docker runtime args (before container) are preserved
	expectedArgs := []string{"-v", "/host:/container"}
	if len(result.Args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d: %v", len(expectedArgs), len(result.Args), result.Args)
	}

	// Verify volume mount is in args
	hasVolume := false
	for i, arg := range result.Args {
		if arg == "-v" && i+1 < len(result.Args) && result.Args[i+1] == "/host:/container" {
			hasVolume = true
			break
		}
	}
	if !hasVolume {
		t.Error("Expected volume mount args in Docker runtime args")
	}

	// Check that entrypointArgs are preserved
	expectedEntrypointArgs := []string{"serve", "--port", "8080"}
	if len(result.EntrypointArgs) != len(expectedEntrypointArgs) {
		t.Errorf("Expected %d entrypointArgs, got %d: %v", len(expectedEntrypointArgs), len(result.EntrypointArgs), result.EntrypointArgs)
	}

	// Verify entrypoint args
	for i, expectedArg := range expectedEntrypointArgs {
		if i >= len(result.EntrypointArgs) {
			t.Errorf("Missing entrypoint arg at index %d: expected '%s'", i, expectedArg)
			continue
		}
		if result.EntrypointArgs[i] != expectedArg {
			t.Errorf("Entrypoint arg %d: expected '%s', got '%s'", i, expectedArg, result.EntrypointArgs[i])
		}
	}
}
