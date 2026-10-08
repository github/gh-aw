//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestEnsureMCPConfig(t *testing.T) {
	tests := []struct {
		name            string
		existingConfig  *MCPConfig
		verbose         bool
		wantErr         bool
		validateContent func(*testing.T, *MCPConfig)
	}{
		{
			name:    "creates new mcp.json in empty directory",
			verbose: false,
			wantErr: false,
			validateContent: func(t *testing.T, config *MCPConfig) {
				if config.MCPServers == nil {
					t.Error("Expected mcpServers map to be initialized")
				}
				server, exists := config.MCPServers["github-agentic-workflows"]
				if !exists {
					t.Error("Expected github-agentic-workflows server to exist")
				}
				if server.Type != "local" {
					t.Errorf("Expected type 'local', got %q", server.Type)
				}
				if server.Command != "gh" {
					t.Errorf("Expected command 'gh', got %q", server.Command)
				}
				if len(server.Args) != 2 || server.Args[0] != "aw" || server.Args[1] != "mcp-server" {
					t.Errorf("Expected args ['aw', 'mcp-server'], got %v", server.Args)
				}
				expectedTools := []string{"compile", "audit", "logs", "inspect", "status", "audit-diff", "work-queue"}
				if len(server.Tools) != len(expectedTools) {
					t.Errorf("Expected %d tools, got %d (%v)", len(expectedTools), len(server.Tools), server.Tools)
				} else {
					for i, tool := range expectedTools {
						if server.Tools[i] != tool {
							t.Errorf("Expected tools %v, got %v", expectedTools, server.Tools)
							break
						}
					}
				}
			},
		},
		{
			name: "adds gh-aw server to existing config without gh-aw server",
			existingConfig: &MCPConfig{
				MCPServers: map[string]VSCodeMCPServer{
					"other-server": {
						Command: "node",
						Args:    []string{"server.js"},
					},
				},
			},
			verbose: true,
			wantErr: false,
			validateContent: func(t *testing.T, config *MCPConfig) {
				if len(config.MCPServers) != 2 {
					t.Errorf("Expected 2 servers, got %d", len(config.MCPServers))
				}
				if _, exists := config.MCPServers["other-server"]; !exists {
					t.Error("Expected existing other-server to be preserved")
				}
				server, exists := config.MCPServers["github-agentic-workflows"]
				if !exists {
					t.Fatal("Expected github-agentic-workflows server to be added")
				}
				if server.Type != "local" {
					t.Errorf("Expected type 'local', got %q", server.Type)
				}
			},
		},
		{
			name: "skips update when config is identical",
			existingConfig: &MCPConfig{
				MCPServers: map[string]VSCodeMCPServer{
					"github-agentic-workflows": {
						Type:    "local",
						Command: "gh",
						Args:    []string{"aw", "mcp-server"},
						Tools:   []string{"compile", "audit", "logs", "inspect", "status", "audit-diff", "work-queue"},
					},
				},
			},
			verbose: false,
			wantErr: false,
			validateContent: func(t *testing.T, config *MCPConfig) {
				if len(config.MCPServers) != 1 {
					t.Errorf("Expected 1 server, got %d", len(config.MCPServers))
				}
			},
		},
		{
			name: "updates existing config with missing required fields",
			existingConfig: &MCPConfig{
				MCPServers: map[string]VSCodeMCPServer{
					"github-agentic-workflows": {
						Command: "old-command",
						Args:    []string{"old-arg"},
						CWD:     "${workspaceFolder}",
					},
				},
			},
			verbose: false,
			wantErr: false,
			validateContent: func(t *testing.T, config *MCPConfig) {
				server := config.MCPServers["github-agentic-workflows"]
				if server.Type != "local" {
					t.Errorf("Expected type 'local', got %q", server.Type)
				}
				if server.Command != "gh" {
					t.Errorf("Expected command 'gh', got %q", server.Command)
				}
				expectedArgs := []string{"aw", "mcp-server"}
				if !reflect.DeepEqual(server.Args, expectedArgs) {
					t.Errorf("Expected args %v, got %v", expectedArgs, server.Args)
				}
				expectedTools := []string{"compile", "audit", "logs", "inspect", "status", "audit-diff", "work-queue"}
				if len(server.Tools) != len(expectedTools) {
					t.Errorf("Expected tools %v, got %v", expectedTools, server.Tools)
				}
				if server.CWD != "${workspaceFolder}" {
					t.Errorf("Expected cwd to be preserved, got %q", server.CWD)
				}
			},
		},
		{
			name: "updates legacy servers config in place",
			existingConfig: &MCPConfig{
				Servers: map[string]VSCodeMCPServer{
					"github-agentic-workflows": {
						Command: "gh",
						Args:    []string{"aw", "mcp-server"},
					},
				},
			},
			verbose: false,
			wantErr: false,
			validateContent: func(t *testing.T, config *MCPConfig) {
				if config.MCPServers != nil {
					t.Error("Expected mcpServers to remain nil for legacy-only config")
				}
				server, exists := config.Servers["github-agentic-workflows"]
				if !exists {
					t.Fatal("Expected github-agentic-workflows server in legacy servers map")
				}
				if server.Type != "local" {
					t.Errorf("Expected type 'local', got %q", server.Type)
				}
				if len(server.Tools) != 7 {
					t.Errorf("Expected 7 tools, got %d", len(server.Tools))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir := testutil.TempDir(t, "test-*")

			// Change to temp directory
			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get current directory: %v", err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(originalDir); err != nil {
					t.Logf("Failed to restore directory: %v", err)
				}
			})

			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("Failed to change to temp directory: %v", err)
			}

			// Create existing config if specified
			if tt.existingConfig != nil {
				data, err := json.MarshalIndent(tt.existingConfig, "", "  ")
				if err != nil {
					t.Fatalf("Failed to marshal existing config: %v", err)
				}

				if err := os.MkdirAll(filepath.Dir(mcpConfigFilePath), 0755); err != nil {
					t.Fatalf("Failed to create mcp config directory: %v", err)
				}
				if err := os.WriteFile(mcpConfigFilePath, data, 0644); err != nil {
					t.Fatalf("Failed to write existing config: %v", err)
				}
			}

			// Call the function
			err = ensureMCPConfig(tt.verbose)

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("ensureMCPConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Verify the file was created
			if _, err := os.Stat(mcpConfigFilePath); os.IsNotExist(err) {
				t.Error("Expected .github/mcp.json to exist")
				return
			}

			// Read and validate the content
			data, err := os.ReadFile(mcpConfigFilePath)
			if err != nil {
				t.Fatalf("Failed to read mcp.json: %v", err)
			}

			var config MCPConfig
			if err := json.Unmarshal(data, &config); err != nil {
				t.Fatalf("Failed to unmarshal mcp.json: %v", err)
			}

			// Run custom validation if provided
			if tt.validateContent != nil {
				tt.validateContent(t, &config)
			}
		})
	}
}

func TestMCPConfigParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		jsonData  string
		wantErr   bool
		wantValid bool
	}{
		{
			name: "valid config with single server",
			jsonData: `{
				"mcpServers": {
					"test-server": {
						"command": "node",
						"args": ["server.js"]
					}
				}
			}`,
			wantErr:   false,
			wantValid: true,
		},
		{
			name: "valid config with CWD",
			jsonData: `{
				"mcpServers": {
					"test-server": {
						"command": "gh",
						"args": ["aw", "mcp-server"],
						"cwd": "${workspaceFolder}"
					}
				}
			}`,
			wantErr:   false,
			wantValid: true,
		},
		{
			name:      "invalid JSON",
			jsonData:  `{"mcpServers": invalid}`,
			wantErr:   true,
			wantValid: false,
		},
		{
			name: "empty config",
			jsonData: `{
				"mcpServers": {}
			}`,
			wantErr:   false,
			wantValid: true,
		},
		{
			name: "legacy config key",
			jsonData: `{
				"servers": {
					"test-server": {
						"command": "node",
						"args": ["server.js"]
					}
				}
			}`,
			wantErr:   false,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config MCPConfig
			err := json.Unmarshal([]byte(tt.jsonData), &config)

			if (err != nil) != tt.wantErr {
				t.Errorf("json.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.wantValid {
				if tt.name == "legacy config key" {
					if config.Servers == nil {
						t.Error("Expected legacy servers map to be initialized")
					}
					if config.MCPServers != nil {
						t.Error("Expected mcpServers map to be nil for legacy-only config")
					}
				} else if config.MCPServers == nil {
					t.Error("Expected mcpServers map to be initialized")
				}
			}
		})
	}
}

func TestMCPConfigJSONMarshaling(t *testing.T) {
	t.Parallel()

	config := MCPConfig{
		MCPServers: map[string]VSCodeMCPServer{
			"github-agentic-workflows": {
				Type:    "local",
				Command: "gh",
				Args:    []string{"aw", "mcp-server"},
				Tools:   []string{"compile", "audit", "logs", "inspect", "status", "audit-diff"},
			},
		},
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Unmarshal back
	var unmarshaledConfig MCPConfig
	if err := json.Unmarshal(data, &unmarshaledConfig); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify structure
	if len(unmarshaledConfig.MCPServers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(unmarshaledConfig.MCPServers))
	}

	server, exists := unmarshaledConfig.MCPServers["github-agentic-workflows"]
	if !exists {
		t.Fatal("Expected github-agentic-workflows server to exist")
	}

	if server.Command != "gh" {
		t.Errorf("Expected command 'gh', got %q", server.Command)
	}

	if len(server.Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(server.Args))
	}

	if server.Type != "local" {
		t.Errorf("Expected type 'local', got %q", server.Type)
	}

	if len(server.Tools) != 6 {
		t.Errorf("Expected 6 tools, got %d", len(server.Tools))
	}
}

func TestEnsureMCPConfigDirectoryCreation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Call function when .github/mcp.json doesn't exist
	err = ensureMCPConfig(false)
	if err != nil {
		t.Fatalf("ensureMCPConfig() failed: %v", err)
	}

	// Verify .github/mcp.json was created
	if _, err := os.Stat(mcpConfigFilePath); os.IsNotExist(err) {
		t.Error("Expected .github/mcp.json to be created")
	}
}

func TestMCPConfigFilePermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	err = ensureMCPConfig(false)
	if err != nil {
		t.Fatalf("ensureMCPConfig() failed: %v", err)
	}

	// Check file permissions
	info, err := os.Stat(mcpConfigFilePath)
	if err != nil {
		t.Fatalf("Failed to stat .github/mcp.json: %v", err)
	}

	// Verify file is readable and writable (at minimum)
	mode := info.Mode()
	if mode.Perm()&0600 != 0600 {
		t.Errorf("Expected file to have at least 0600 permissions, got %o", mode.Perm())
	}
}
