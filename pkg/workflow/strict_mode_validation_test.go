//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestValidateStrictPermissions tests the validateStrictPermissions function
func TestValidateStrictPermissions(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expectError bool
		errorMsg    string
	}{
		{
			name: "no permissions specified is allowed",
			frontmatter: map[string]any{
				"on": "push",
			},
			expectError: false,
		},
		{
			name: "read permissions are allowed",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents":      "read",
					"issues":        "read",
					"pull-requests": "read",
				},
			},
			expectError: false,
		},
		{
			name: "contents write permission is refused",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "write",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: write permission 'contents: write' is not allowed for security reasons. Use 'safe-outputs.create-issue', 'safe-outputs.create-pull-request', 'safe-outputs.add-comment', or 'safe-outputs.update-issue' to perform write operations safely",
		},
		{
			name: "issues write permission is refused",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"issues": "write",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: write permission 'issues: write' is not allowed for security reasons. Use 'safe-outputs.create-issue', 'safe-outputs.create-pull-request', 'safe-outputs.add-comment', or 'safe-outputs.update-issue' to perform write operations safely",
		},
		{
			name: "pull-requests write permission is refused",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"pull-requests": "write",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: write permission 'pull-requests: write' is not allowed for security reasons. Use 'safe-outputs.create-issue', 'safe-outputs.create-pull-request', 'safe-outputs.add-comment', or 'safe-outputs.update-issue' to perform write operations safely",
		},
		{
			name: "multiple write permissions fail on first one",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents":      "write",
					"issues":        "write",
					"pull-requests": "write",
				},
			},
			expectError: true,
			errorMsg:    "write permission",
		},
		{
			name: "mixed read and write permissions are refused",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents":      "read",
					"issues":        "write",
					"pull-requests": "read",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: write permission 'issues: write' is not allowed for security reasons. Use 'safe-outputs.create-issue', 'safe-outputs.create-pull-request', 'safe-outputs.add-comment', or 'safe-outputs.update-issue' to perform write operations safely",
		},
		{
			name: "other write permissions are allowed (not in sensitive scopes)",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"actions": "write",
					"checks":  "write",
				},
			},
			expectError: false,
		},
		{
			name: "shorthand read-all is allowed",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": "read-all",
			},
			expectError: false,
		},
		{
			name: "shorthand write-all is refused",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": "write-all",
			},
			expectError: true,
			errorMsg:    "strict mode: write permission 'contents: write' is not allowed for security reasons. Use 'safe-outputs.create-issue', 'safe-outputs.create-pull-request', 'safe-outputs.add-comment', or 'safe-outputs.update-issue' to perform write operations safely",
		},
		{
			name: "empty permissions map is allowed",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": map[string]any{},
			},
			expectError: false,
		},
		{
			name: "nil permissions value is skipped gracefully",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": nil,
			},
			expectError: false,
		},
		{
			name: "id-token write is allowed (safe permission for OIDC)",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"id-token": "write",
				},
			},
			expectError: false,
		},
		{
			name: "id-token write with read permissions is allowed",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "read",
					"id-token": "write",
					"issues":   "read",
				},
			},
			expectError: false,
		},
		{
			name: "id-token write with other safe write permissions is allowed",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"id-token":     "write",
					"attestations": "write",
					"actions":      "write",
				},
			},
			expectError: false,
		},
		{
			name: "id-token write with blocked write permissions fails on blocked permissions",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"id-token": "write",
					"contents": "write",
				},
			},
			expectError: true,
			errorMsg:    "strict mode: write permission 'contents: write' is not allowed for security reasons",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateStrictPermissions(tt.frontmatter)

			if tt.expectError && err == nil {
				t.Error("Expected validation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected validation to succeed but it failed: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

// TestValidateStrictNetwork tests the validateStrictNetwork function
func TestValidateStrictNetwork(t *testing.T) {
	tests := []struct {
		name               string
		networkPermissions *NetworkPermissions
		expectError        bool
		errorMsg           string
	}{
		{
			name:               "nil network permissions triggers internal error",
			networkPermissions: nil,
			expectError:        true,
			errorMsg:           "internal error: network permissions not initialized",
		},
		{
			name: "defaults mode is allowed",
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults"},
			},
			expectError: false,
		},
		{
			name: "specific allowed domains are allowed",
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com", "github.com"},
			},
			expectError: false,
		},
		{
			name: "wildcard in allowed domains is refused",
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"*"},
			},
			expectError: true,
			errorMsg:    "strict mode: wildcard '*' is not allowed in network.allowed domains to prevent unrestricted internet access",
		},
		{
			name: "wildcard among other domains is refused",
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com", "*", "github.com"},
			},
			expectError: true,
			errorMsg:    "strict mode: wildcard '*' is not allowed in network.allowed domains to prevent unrestricted internet access",
		},
		{
			name: "empty allowed list is allowed",
			networkPermissions: &NetworkPermissions{
				Allowed: []string{},
			},
			expectError: false,
		},
		{
			name: "domain patterns with wildcards are allowed (not exact *)",
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"*.example.com", "api.*.com"},
			},
			expectError: false,
		},
		{
			name: "single domain is allowed",
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.github.com"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateStrictNetwork(tt.networkPermissions)

			if tt.expectError && err == nil {
				t.Error("Expected validation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected validation to succeed but it failed: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

// TestValidateStrictMode tests the main validateStrictMode orchestrator function
func TestValidateStrictMode(t *testing.T) {
	tests := []struct {
		name               string
		strictMode         bool
		frontmatter        map[string]any
		networkPermissions *NetworkPermissions
		expectError        bool
		errorMsg           string
	}{
		{
			name:       "non-strict mode skips all validation",
			strictMode: false,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "write",
				},
			},
			networkPermissions: nil,
			expectError:        false,
		},
		{
			name:       "strict mode with valid configuration",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "read",
				},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: false,
		},
		{
			name:       "strict mode fails on write permissions",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "write",
				},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: true,
			errorMsg:    "strict mode: write permission 'contents: write' is not allowed for security reasons",
		},
		{
			name:       "strict mode with nil network triggers internal error (should not happen in production)",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "read",
				},
			},
			networkPermissions: nil,
			expectError:        true,
			errorMsg:           "internal error: network permissions not initialized",
		},
		{
			name:       "strict mode fails on wildcard network",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "read",
				},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"*"},
			},
			expectError: true,
			errorMsg:    "strict mode: wildcard '*' is not allowed in network.allowed domains to prevent unrestricted internet access",
		},
		{
			name:       "strict mode with container MCP requiring network",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "read",
				},
				"mcp-servers": map[string]any{
					"my-server": map[string]any{
						"container": "my-image",
					},
				},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{}, // Empty allowed list - no top-level network config
			},
			expectError: true,
			errorMsg:    "strict mode: custom MCP server 'my-server' with container must have top-level network configuration for security",
		},
		{
			name:       "strict mode with container MCP and network config",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "read",
				},
				"mcp-servers": map[string]any{
					"my-server": map[string]any{
						"container": "my-image",
						"network": map[string]any{
							"allowed": []string{"example.com"},
						},
					},
				},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: false,
		},
		{
			name:       "strict mode with defaults network mode",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "read",
				},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults"},
			},
			expectError: false,
		},
		{
			name:       "strict mode without explicit network declaration (defaults auto-applied)",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents": "read",
				},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults"}, // This is what the compiler orchestrator sets when network is not in frontmatter
			},
			expectError: false,
		},
		{
			name:       "strict mode with no permissions is allowed",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: false,
		},
		{
			name:       "strict mode with id-token write is allowed",
			strictMode: true,
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"id-token": "write",
					"contents": "read",
				},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetStrictMode(tt.strictMode)
			err := compiler.validateStrictMode(tt.frontmatter, tt.networkPermissions)

			if tt.expectError && err == nil {
				t.Error("Expected validation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected validation to succeed but it failed: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

// TestValidateStrictModeEdgeCases tests edge cases for the validateStrictMode function
func TestValidateStrictModeEdgeCases(t *testing.T) {
	tests := []struct {
		name               string
		frontmatter        map[string]any
		networkPermissions *NetworkPermissions
		expectError        bool
		errorMsg           string
	}{
		{
			name:        "empty frontmatter with valid network",
			frontmatter: map[string]any{},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: false,
		},
		{
			name:        "nil frontmatter map is handled",
			frontmatter: nil,
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: false,
		},
		{
			name: "invalid permissions type is handled gracefully",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": "invalid-string-value",
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: false,
		},
		{
			name: "permissions as array is handled gracefully",
			frontmatter: map[string]any{
				"on":          "push",
				"permissions": []string{"contents:read"},
			},
			networkPermissions: &NetworkPermissions{
				Allowed: []string{"api.example.com"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			err := compiler.validateStrictMode(tt.frontmatter, tt.networkPermissions)

			if tt.expectError && err == nil {
				t.Error("Expected validation to fail but it succeeded")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected validation to succeed but it failed: %v", err)
			} else if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}
