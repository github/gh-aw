//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/types"
)

func TestGetMCPConfig(t *testing.T) {
	tests := []struct {
		name       string
		toolConfig map[string]any
		expected   *parser.RegistryMCPServerConfig
		wantErr    bool
	}{
		{
			name: "direct fields",
			toolConfig: map[string]any{
				"type":    "stdio",
				"command": "python",
				"args":    []any{"-m", "test"},
			},
			expected: &parser.RegistryMCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Type:    "stdio",
					Command: "python",
					Args:    []string{"-m", "test"},
					Env:     make(map[string]string),
					Headers: make(map[string]string),
				},
				Name: "test",
			},
			wantErr: false,
		},
		{
			name: "inferred stdio type from command",
			toolConfig: map[string]any{
				"command": "python",
				"args":    []any{"-m", "test"},
			},
			expected: &parser.RegistryMCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Type:    "stdio",
					Command: "python",
					Args:    []string{"-m", "test"},
					Env:     make(map[string]string),
					Headers: make(map[string]string),
				},
				Name: "test",
			},
			wantErr: false,
		},
		{
			name: "inferred http type from url",
			toolConfig: map[string]any{
				"url": "https://example.com",
			},
			expected: &parser.RegistryMCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Type:    "http",
					URL:     "https://example.com",
					Env:     make(map[string]string),
					Headers: make(map[string]string),
				},
				Name: "test",
			},
			wantErr: false,
		},
		{
			name: "no MCP fields",
			toolConfig: map[string]any{
				"allowed": []any{"tool1"},
			},
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getMCPConfig(tt.toolConfig, "test")

			if tt.wantErr != (err != nil) {
				t.Errorf("getMCPConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Compare struct fields
				if result.Name != tt.expected.Name ||
					result.Type != tt.expected.Type ||
					result.Command != tt.expected.Command ||
					result.URL != tt.expected.URL ||
					len(result.Args) != len(tt.expected.Args) ||
					len(result.Env) != len(tt.expected.Env) ||
					len(result.Headers) != len(tt.expected.Headers) {
					t.Errorf("getMCPConfig() = %+v, want %+v", result, tt.expected)
				}

				// Check args array
				for i, arg := range result.Args {
					if i < len(tt.expected.Args) && arg != tt.expected.Args[i] {
						t.Errorf("getMCPConfig() args[%d] = %v, want %v", i, arg, tt.expected.Args[i])
					}
				}
			}
		})
	}
}

func TestGetMCPConfigWithAuth(t *testing.T) {
	t.Run("http server with auth type and audience round-trips", func(t *testing.T) {
		toolConfig := map[string]any{
			"type": "http",
			"url":  "https://my-server.example.com/mcp",
			"auth": map[string]any{
				"type":     "github-oidc",
				"audience": "https://my-server.example.com",
			},
		}

		result, err := getMCPConfig(toolConfig, "my-server")
		if err != nil {
			t.Fatalf("getMCPConfig() unexpected error: %v", err)
		}

		if result.Auth == nil {
			t.Fatal("expected Auth to be set, got nil")
		}
		if result.Auth.Type != "github-oidc" {
			t.Errorf("expected Auth.Type = 'github-oidc', got %q", result.Auth.Type)
		}
		if result.Auth.Audience != "https://my-server.example.com" {
			t.Errorf("expected Auth.Audience = 'https://my-server.example.com', got %q", result.Auth.Audience)
		}
	})

	t.Run("http server with auth type only (no audience)", func(t *testing.T) {
		toolConfig := map[string]any{
			"type": "http",
			"url":  "https://my-server.example.com/mcp",
			"auth": map[string]any{
				"type": "github-oidc",
			},
		}

		result, err := getMCPConfig(toolConfig, "my-server")
		if err != nil {
			t.Fatalf("getMCPConfig() unexpected error: %v", err)
		}

		if result.Auth == nil {
			t.Fatal("expected Auth to be set, got nil")
		}
		if result.Auth.Type != "github-oidc" {
			t.Errorf("expected Auth.Type = 'github-oidc', got %q", result.Auth.Type)
		}
		if result.Auth.Audience != "" {
			t.Errorf("expected Auth.Audience to be empty, got %q", result.Auth.Audience)
		}
	})

	t.Run("http server without auth has nil Auth", func(t *testing.T) {
		toolConfig := map[string]any{
			"type": "http",
			"url":  "https://my-server.example.com/mcp",
		}

		result, err := getMCPConfig(toolConfig, "my-server")
		if err != nil {
			t.Fatalf("getMCPConfig() unexpected error: %v", err)
		}

		if result.Auth != nil {
			t.Errorf("expected Auth to be nil, got %+v", result.Auth)
		}
	})
}

func TestHasMCPConfig(t *testing.T) {
	tests := []struct {
		name       string
		toolConfig map[string]any
		expected   bool
		mcpType    string
	}{
		{
			name: "direct type field with valid type",
			toolConfig: map[string]any{
				"type": "stdio",
			},
			expected: true,
			mcpType:  "stdio",
		},
		{
			name: "direct type field with invalid type",
			toolConfig: map[string]any{
				"type": "invalid",
			},
			expected: false,
			mcpType:  "",
		},
		{
			name: "no type field",
			toolConfig: map[string]any{
				"allowed": []any{"tool1"},
			},
			expected: false,
			mcpType:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasMcp, mcpType := hasMCPConfig(tt.toolConfig)

			if hasMcp != tt.expected {
				t.Errorf("hasMCPConfig() hasMcp = %v, want %v", hasMcp, tt.expected)
			}

			if mcpType != tt.mcpType {
				t.Errorf("hasMCPConfig() mcpType = %v, want %v", mcpType, tt.mcpType)
			}
		})
	}
}

func TestValidateMCPConfigs(t *testing.T) {
	tests := []struct {
		name    string
		tools   map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name: "new format: valid stdio with direct fields",
			tools: map[string]any{
				"test-server": map[string]any{
					"type":    "stdio",
					"command": "python",
					"args":    []any{"-m", "server"},
					"env": map[string]any{
						"DEBUG": "true",
					},
					"allowed": []any{"tool1", "tool2"},
				},
			},
			wantErr: false,
		},
		{
			name: "new format: valid http with direct fields",
			tools: map[string]any{
				"http-server": map[string]any{
					"type": "http",
					"url":  "https://api.example.com/mcp",
					"headers": map[string]any{
						"Authorization": "Bearer token123",
					},
					"allowed": []any{"query", "update"},
				},
			},
			wantErr: false,
		},
		{
			name: "new format: stdio with container",
			tools: map[string]any{
				"container-server": map[string]any{
					"type":      "stdio",
					"container": "mcp/server:latest",
					"env": map[string]any{
						"API_KEY": "secret",
					},
					"allowed": []any{"process"},
				},
			},
			wantErr: false,
		},
		{
			name: "new format: stdio with container and network config should fail",
			tools: map[string]any{
				"network-server": map[string]any{
					"type":      "stdio",
					"container": "mcp/network-server:latest",
					"network": map[string]any{
						"allowed":    []any{"example.com", "api.example.com"},
						"proxy-args": []any{"--proxy-test"},
					},
					"allowed": []any{"fetch", "post"},
				},
			},
			wantErr: true,
			errMsg:  "unknown property 'network'",
		},
		{
			name: "new format: missing type and no inferrable fields",
			tools: map[string]any{
				"no-type": map[string]any{
					"env":     map[string]any{"KEY": "value"},
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "unable to determine MCP type",
		},
		{
			name: "new format: invalid type value",
			tools: map[string]any{
				"bad-type": map[string]any{
					"type":    "invalid",
					"command": "python",
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "must be one of: stdio, http",
		},
		{
			name: "new format: http missing url",
			tools: map[string]any{
				"http-no-url": map[string]any{
					"type":    "http",
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "missing required property 'url'",
		},
		{
			name: "new format: stdio missing command and container",
			tools: map[string]any{
				"stdio-incomplete": map[string]any{
					"type":    "stdio",
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "must specify either 'command' or 'container'",
		},
		{
			name: "new format: both command and container specified",
			tools: map[string]any{
				"both-cmd-container": map[string]any{
					"type":      "stdio",
					"command":   "python",
					"container": "mcp/server",
					"allowed":   []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "cannot specify both 'container' and 'command'",
		},
		{
			name: "valid MCP configs",
			tools: map[string]any{
				"trelloApi": map[string]any{
					"type":    "stdio",
					"command": "python",
					"allowed": []any{"create_card"},
				},
				"notionApi": map[string]any{
					"type":    "http",
					"url":     "https://mcp.notion.com",
					"allowed": []any{"*"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid type value",
			tools: map[string]any{
				"badApi": map[string]any{
					"type":    "invalid",
					"command": "test",
					"allowed": []any{"*"},
				},
			},
			wantErr: true,
			errMsg:  "'type' must be one of",
		},

		{
			name: "invalid type in MCP config",
			tools: map[string]any{
				"invalidType": map[string]any{
					"type":    "invalid",
					"command": "python",
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "'type' must be one of",
		},
		{
			name: "non-string type in MCP config",
			tools: map[string]any{
				"nonStringType": map[string]any{
					"type":    123,
					"command": "python",
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "must be a string",
		},
		{
			name: "http type missing URL",
			tools: map[string]any{
				"httpMissingUrl": map[string]any{
					"type":    "http",
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "missing required property 'url'",
		},
		{
			name: "stdio type missing command",
			tools: map[string]any{
				"stdioMissingCommand": map[string]any{
					"type":    "stdio",
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "must specify either 'command' or 'container'",
		},
		{
			name: "http type with non-string URL",
			tools: map[string]any{
				"httpNonStringUrl": map[string]any{
					"type":    "http",
					"url":     123,
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "must be a string",
		},
		{
			name: "stdio type with non-string command",
			tools: map[string]any{
				"stdioNonStringCommand": map[string]any{
					"type":    "stdio",
					"command": []string{"python"},
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "must be a string",
		},
		{
			name: "valid tools without MCP",
			tools: map[string]any{
				"github": map[string]any{
					"allowed": []any{"list_issues"},
				},
			},
			wantErr: false,
		},
		{
			name: "mixed valid and invalid MCP configs",
			tools: map[string]any{
				"goodApi": map[string]any{
					"type":    "stdio",
					"command": "test",
					"allowed": []any{"tool1"},
				},
				"badApi": map[string]any{
					"type": "http",
					// missing url
					"allowed": []any{"tool2"},
				},
			},
			wantErr: true,
			errMsg:  "missing required property 'url'",
		},
		{
			name: "network field in tool config should fail (no longer supported)",
			tools: map[string]any{
				"toolWithNetworkField": map[string]any{
					"type":      "stdio",
					"container": "mcp/fetch",
					"network": map[string]any{
						"allowed": []any{"example.com"},
					},
					"allowed": []any{"tool1"},
				},
			},
			wantErr: true,
			errMsg:  "unknown property 'network'",
		},
		{
			name: "http server with valid auth config is accepted",
			tools: map[string]any{
				"oidc-server": map[string]any{
					"type": "http",
					"url":  "https://my-server.example.com/mcp",
					"auth": map[string]any{
						"type":     "github-oidc",
						"audience": "https://my-server.example.com",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "http server with auth type only (no audience) is accepted",
			tools: map[string]any{
				"oidc-server": map[string]any{
					"type": "http",
					"url":  "https://my-server.example.com/mcp",
					"auth": map[string]any{
						"type": "github-oidc",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "stdio server with auth is rejected",
			tools: map[string]any{
				"stdio-with-auth": map[string]any{
					"type":      "stdio",
					"container": "mcp/server:latest",
					"auth": map[string]any{
						"type": "github-oidc",
					},
				},
			},
			wantErr: true,
			errMsg:  "'auth' is only supported for HTTP servers",
		},
		{
			name: "auth without type field is rejected",
			tools: map[string]any{
				"bad-auth": map[string]any{
					"type": "http",
					"url":  "https://my-server.example.com/mcp",
					"auth": map[string]any{
						"audience": "https://my-server.example.com",
					},
				},
			},
			wantErr: true,
			errMsg:  "'auth.type' is required",
		},
		{
			name: "auth with unsupported type is rejected",
			tools: map[string]any{
				"bad-auth-type": map[string]any{
					"type": "http",
					"url":  "https://my-server.example.com/mcp",
					"auth": map[string]any{
						"type": "bearer-token",
					},
				},
			},
			wantErr: true,
			errMsg:  "not supported",
		},
		{
			name: "auth with empty type string is rejected",
			tools: map[string]any{
				"empty-auth-type": map[string]any{
					"type": "http",
					"url":  "https://my-server.example.com/mcp",
					"auth": map[string]any{
						"type": "",
					},
				},
			},
			wantErr: true,
			errMsg:  "must be a non-empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPConfigs(tt.tools)

			if tt.wantErr != (err != nil) {
				t.Errorf("ValidateMCPConfigs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateMCPConfigs() error = %v, expected to contain %v", err, tt.errMsg)
				}
			}
		})
	}
}

func TestValidateToolsSection(t *testing.T) {
	tests := []struct {
		name        string
		tools       map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil tools",
			tools:   nil,
			wantErr: false,
		},
		{
			name:    "empty tools",
			tools:   map[string]any{},
			wantErr: false,
		},
		{
			name: "built-in tools only - no error",
			tools: map[string]any{
				"github":     map[string]any{"mode": "local"},
				"playwright": map[string]any{"version": "v1.41.0"},
				"bash":       []any{"echo", "ls"},
				"ledger":     map[string]any{"findings": map[string]any{"schema": map[string]any{"type": "object"}}},
				"web-fetch":  nil,
			},
			wantErr: false,
		},
		{
			name: "unknown tool name with version-only config",
			tools: map[string]any{
				"nonexistent-tool-xyz": map[string]any{"version": "1.0"},
			},
			wantErr:     true,
			errContains: "tools.nonexistent-tool-xyz: unknown tool name",
		},
		{
			name: "typo of built-in tool name",
			tools: map[string]any{
				"playwrigjht": map[string]any{"version": "v1.41.0"},
			},
			wantErr:     true,
			errContains: "tools.playwrigjht: unknown tool name",
		},
		{
			// Custom MCP servers with command/url/container belong under mcp-servers, not tools
			name: "custom MCP server in tools section is an error",
			tools: map[string]any{
				"my-mcp-server": map[string]any{
					"command": "node",
					"args":    []any{"server.js"},
				},
			},
			wantErr:     true,
			errContains: "tools.my-mcp-server: unknown tool name",
		},
		{
			name: "unknown tool with allowed-only config",
			tools: map[string]any{
				"unknown-tool": map[string]any{
					"allowed": []any{"some-tool"},
				},
			},
			wantErr:     true,
			errContains: "tools.unknown-tool: unknown tool name",
		},
		{
			name: "unknown tool with nil value",
			tools: map[string]any{
				"bad-tool": nil,
			},
			wantErr:     true,
			errContains: "tools.bad-tool: unknown tool name",
		},
		{
			// Error message should direct users to mcp-servers:
			name: "error message references mcp-servers",
			tools: map[string]any{
				"my-custom-server": map[string]any{"command": "npx my-tool"},
			},
			wantErr:     true,
			errContains: "mcp-servers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateToolsSection(tt.tools)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateToolsSection() expected an error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateToolsSection() error = %q, expected to contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateToolsSection() unexpected error: %v", err)
				}
			}
		})
	}
}
