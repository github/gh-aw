//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestValidateStrictMCPNetwork_NoMCPServers tests that validation passes when no mcp-servers are configured
func TestValidateStrictMCPNetwork_NoMCPServers(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for frontmatter without mcp-servers, got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_EmptyMCPServers tests that validation passes with empty mcp-servers map
func TestValidateStrictMCPNetwork_EmptyMCPServers(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on":          "push",
		"mcp-servers": map[string]any{},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for empty mcp-servers, got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_InvalidMCPServersType tests that validation skips invalid mcp-servers type
func TestValidateStrictMCPNetwork_InvalidMCPServersType(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on":          "push",
		"mcp-servers": "invalid-type",
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for invalid mcp-servers type (should skip), got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_ContainerWithTopLevelNetwork tests that validation passes with container + top-level network
func TestValidateStrictMCPNetwork_ContainerWithTopLevelNetwork(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
			},
		},
	}

	// Top-level network configuration provided
	networkPermissions := &NetworkPermissions{
		Allowed: []string{"example.com"},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, networkPermissions)
	if err != nil {
		t.Errorf("Expected no error for container with top-level network configuration, got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_ContainerWithoutNetwork tests that validation fails without top-level network config
func TestValidateStrictMCPNetwork_ContainerWithoutNetwork(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err == nil {
		t.Error("Expected error for container without top-level network configuration, got nil")
	}
	expectedMsg := "strict mode: custom MCP server 'my-server' with container must have top-level network configuration for security"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain %q, got: %q", expectedMsg, err.Error())
	}
}

// TestValidateStrictMCPNetwork_ExplicitStdioTypeContainerWithNetwork tests stdio type with container and top-level network
func TestValidateStrictMCPNetwork_ExplicitStdioTypeContainerWithNetwork(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"type":      "stdio",
				"container": "my-image",
			},
		},
	}

	// Top-level network configuration provided
	networkPermissions := &NetworkPermissions{
		Allowed: []string{"example.com"},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, networkPermissions)
	if err != nil {
		t.Errorf("Expected no error for explicit stdio type with container and top-level network, got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_ExplicitStdioTypeContainerWithoutNetwork tests stdio type with container but no network
func TestValidateStrictMCPNetwork_ExplicitStdioTypeContainerWithoutNetwork(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"type":      "stdio",
				"container": "my-image",
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err == nil {
		t.Error("Expected error for stdio type with container but no network, got nil")
	}
}

// TestValidateStrictMCPNetwork_LocalTypeContainerWithNetwork tests local type (converted to stdio) with top-level network
func TestValidateStrictMCPNetwork_LocalTypeContainerWithNetwork(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"type":      "local",
				"container": "my-image",
			},
		},
	}

	// Top-level network configuration provided
	networkPermissions := &NetworkPermissions{
		Allowed: []string{"example.com"},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, networkPermissions)
	if err != nil {
		t.Errorf("Expected no error for local type (converted to stdio) with container and top-level network, got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_LocalTypeContainerWithoutNetwork tests local type with container but no network
func TestValidateStrictMCPNetwork_LocalTypeContainerWithoutNetwork(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"type":      "local",
				"container": "my-image",
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err == nil {
		t.Error("Expected error for local type with container but no network, got nil")
	}
}

// TestValidateStrictMCPNetwork_HTTPTypeNoValidation tests that HTTP type servers don't require network validation
func TestValidateStrictMCPNetwork_HTTPTypeNoValidation(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"type": "http",
				"url":  "https://example.com",
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for HTTP type (no network validation required), got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_StdioWithCommandNoContainer tests stdio with command but no container (allowed)
func TestValidateStrictMCPNetwork_StdioWithCommandNoContainer(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"type":    "stdio",
				"command": "node",
				"args":    []string{"server.js"},
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for stdio with command but no container, got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_InvalidServerConfigType tests that invalid server config type is skipped
func TestValidateStrictMCPNetwork_InvalidServerConfigType(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": "invalid-config-type",
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for invalid server config type (should skip), got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_NonMCPServerSkipped tests that non-MCP server configs are skipped
func TestValidateStrictMCPNetwork_NonMCPServerSkipped(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"some-field": "some-value",
				// No type, command, url, or container - not recognized as MCP config
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for non-MCP server config (should skip), got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_MultipleServers tests validation with multiple MCP servers
func TestValidateStrictMCPNetwork_MultipleServers(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"server1": map[string]any{
				"container": "image1",
			},
			"server2": map[string]any{
				"type": "http",
				"url":  "https://example.com",
			},
			"server3": map[string]any{
				"command": "node",
				"args":    []string{"server.js"},
			},
		},
	}

	// Top-level network configuration provided
	networkPermissions := &NetworkPermissions{
		Allowed: []string{"example.com"},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, networkPermissions)
	if err != nil {
		t.Errorf("Expected no error for multiple valid servers, got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_MultipleServersOneFails tests that one failing server causes validation error
func TestValidateStrictMCPNetwork_MultipleServersOneFails(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"server1": map[string]any{
				"container": "image1",
				"network": map[string]any{
					"allowed": []string{"example.com"},
				},
			},
			"server2": map[string]any{
				"container": "image2",
				// Missing network configuration
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err == nil {
		t.Error("Expected error when one server missing network configuration, got nil")
	}
}

// TestValidateStrictMCPNetwork_InferredStdioFromContainer tests container field infers stdio type
func TestValidateStrictMCPNetwork_InferredStdioFromContainer(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				// Container field alone infers stdio type
				"container": "my-image",
			},
		},
	}

	// Top-level network configuration provided
	networkPermissions := &NetworkPermissions{
		Allowed: []string{"example.com"},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, networkPermissions)
	if err != nil {
		t.Errorf("Expected no error for inferred stdio from container with top-level network, got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_InferredHTTPFromURL tests URL field infers HTTP type (no validation needed)
func TestValidateStrictMCPNetwork_InferredHTTPFromURL(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				// URL field infers HTTP type
				"url": "https://example.com",
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for inferred HTTP type (no validation), got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_InferredStdioFromCommand tests command field infers stdio type (no container, allowed)
func TestValidateStrictMCPNetwork_InferredStdioFromCommand(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				// Command field infers stdio type
				"command": "node",
				"args":    []string{"server.js"},
			},
		},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err != nil {
		t.Errorf("Expected no error for inferred stdio from command (no container), got: %v", err)
	}
}

// TestValidateStrictMCPNetwork_ContainerWithNoNetworkConfig tests container without any network config fails
func TestValidateStrictMCPNetwork_ContainerWithNoNetworkConfig(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
			},
		},
	}

	// No top-level network configuration (nil)
	err := compiler.validateStrictMCPNetwork(frontmatter, nil)
	if err == nil {
		t.Error("Expected error for container without any network configuration, got nil")
	}
	expectedMsg := "strict mode: custom MCP server 'my-server' with container must have top-level network configuration for security"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain %q, got: %q", expectedMsg, err.Error())
	}
}

// TestValidateStrictMCPNetwork_ContainerWithEmptyTopLevelNetwork tests container with empty top-level network fails
func TestValidateStrictMCPNetwork_ContainerWithEmptyTopLevelNetwork(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"my-server": map[string]any{
				"container": "my-image",
				// No per-server network config
			},
		},
	}

	// Empty top-level network configuration
	networkPermissions := &NetworkPermissions{
		Allowed: []string{},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, networkPermissions)
	if err == nil {
		t.Error("Expected error for container with empty top-level network configuration, got nil")
	}
	expectedMsg := "strict mode: custom MCP server 'my-server' with container must have top-level network configuration for security"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error message to contain %q, got: %q", expectedMsg, err.Error())
	}
}

// TestValidateStrictMCPNetwork_MultipleServersWithTopLevelNetwork tests multiple servers with top-level network
func TestValidateStrictMCPNetwork_MultipleServersWithTopLevelNetwork(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"mcp-servers": map[string]any{
			"server1": map[string]any{
				"container": "image1",
				// No per-server network
			},
			"server2": map[string]any{
				"container": "image2",
				// No per-server network
			},
		},
	}

	// Top-level network configuration covers both servers
	networkPermissions := &NetworkPermissions{
		Allowed: []string{"github.com"},
	}

	err := compiler.validateStrictMCPNetwork(frontmatter, networkPermissions)
	if err != nil {
		t.Errorf("Expected no error for multiple servers with top-level network, got: %v", err)
	}
}
