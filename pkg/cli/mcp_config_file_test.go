//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
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
				if config.Servers == nil {
					t.Error("Expected servers map to be initialized")
				}
				server, exists := config.Servers["github-agentic-workflows"]
				if !exists {
					t.Error("Expected github-agentic-workflows server to exist")
				}
				if server.Command != "gh" {
					t.Errorf("Expected command 'gh', got %q", server.Command)
				}
				if len(server.Args) != 2 || server.Args[0] != "aw" || server.Args[1] != "mcp-server" {
					t.Errorf("Expected args ['aw', 'mcp-server'], got %v", server.Args)
				}
				if server.CWD != "${workspaceFolder}" {
					t.Errorf("Expected CWD '${workspaceFolder}', got %q", server.CWD)
				}
			},
		},
		{
			name: "renders instructions for existing config without gh-aw server",
			existingConfig: &MCPConfig{
				Servers: map[string]VSCodeMCPServer{
					"other-server": {
						Command: "node",
						Args:    []string{"server.js"},
					},
				},
			},
			verbose: true,
			wantErr: false,
			validateContent: func(t *testing.T, config *MCPConfig) {
				// File should NOT be modified - should remain with only 1 server
				if len(config.Servers) != 1 {
					t.Errorf("Expected 1 server (file should not be modified), got %d", len(config.Servers))
				}
				if _, exists := config.Servers["other-server"]; !exists {
					t.Error("Expected existing other-server to be preserved")
				}
				// gh-aw server should NOT be added (instructions rendered instead)
				if _, exists := config.Servers["github-agentic-workflows"]; exists {
					t.Error("Expected github-agentic-workflows server to NOT be added (instructions should be rendered)")
				}
			},
		},
		{
			name: "skips update when config is identical",
			existingConfig: &MCPConfig{
				Servers: map[string]VSCodeMCPServer{
					"github-agentic-workflows": {
						Command: "gh",
						Args:    []string{"aw", "mcp-server"},
						CWD:     "${workspaceFolder}",
					},
				},
			},
			verbose: false,
			wantErr: false,
			validateContent: func(t *testing.T, config *MCPConfig) {
				if len(config.Servers) != 1 {
					t.Errorf("Expected 1 server, got %d", len(config.Servers))
				}
			},
		},
		{
			name: "renders instructions for existing config with different settings",
			existingConfig: &MCPConfig{
				Servers: map[string]VSCodeMCPServer{
					"github-agentic-workflows": {
						Command: "old-command",
						Args:    []string{"old-arg"},
					},
				},
			},
			verbose: false,
			wantErr: false,
			validateContent: func(t *testing.T, config *MCPConfig) {
				// File should NOT be modified - old settings should remain
				server := config.Servers["github-agentic-workflows"]
				if server.Command != "old-command" {
					t.Errorf("Expected command to remain 'old-command' (file should not be modified), got %q", server.Command)
				}
				if len(server.Args) != 1 || server.Args[0] != "old-arg" {
					t.Errorf("Expected args to remain ['old-arg'] (file should not be modified), got %v", server.Args)
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

			// Create .vscode directory and existing config if specified
			if tt.existingConfig != nil {
				vscodeDir := ".vscode"
				if err := os.MkdirAll(vscodeDir, 0755); err != nil {
					t.Fatalf("Failed to create .vscode directory: %v", err)
				}

				data, err := json.MarshalIndent(tt.existingConfig, "", "  ")
				if err != nil {
					t.Fatalf("Failed to marshal existing config: %v", err)
				}

				mcpConfigPath := filepath.Join(vscodeDir, "mcp.json")
				if err := os.WriteFile(mcpConfigPath, data, 0644); err != nil {
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
			mcpConfigPath := filepath.Join(".vscode", "mcp.json")
			if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
				t.Error("Expected .vscode/mcp.json to exist")
				return
			}

			// Read and validate the content
			data, err := os.ReadFile(mcpConfigPath)
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
		{
			name: "valid config with CWD",
			jsonData: `{
				"servers": {
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
			jsonData:  `{"servers": invalid}`,
			wantErr:   true,
			wantValid: false,
		},
		{
			name: "empty config",
			jsonData: `{
				"servers": {}
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
				if config.Servers == nil {
					t.Error("Expected servers map to be initialized")
				}
			}
		})
	}
}

func TestMCPConfigJSONMarshaling(t *testing.T) {
	t.Parallel()

	config := MCPConfig{
		Servers: map[string]VSCodeMCPServer{
			"github-agentic-workflows": {
				Command: "gh",
				Args:    []string{"aw", "mcp-server"},
				CWD:     "${workspaceFolder}",
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
	if len(unmarshaledConfig.Servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(unmarshaledConfig.Servers))
	}

	server, exists := unmarshaledConfig.Servers["github-agentic-workflows"]
	if !exists {
		t.Fatal("Expected github-agentic-workflows server to exist")
	}

	if server.Command != "gh" {
		t.Errorf("Expected command 'gh', got %q", server.Command)
	}

	if len(server.Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(server.Args))
	}

	if server.CWD != "${workspaceFolder}" {
		t.Errorf("Expected CWD '${workspaceFolder}', got %q", server.CWD)
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

	// Call function when .vscode doesn't exist
	err = ensureMCPConfig(false)
	if err != nil {
		t.Fatalf("ensureMCPConfig() failed: %v", err)
	}

	// Verify .vscode directory was created
	vscodeDir := ".vscode"
	info, err := os.Stat(vscodeDir)
	if os.IsNotExist(err) {
		t.Error("Expected .vscode directory to be created")
		return
	}

	if !info.IsDir() {
		t.Error("Expected .vscode to be a directory")
	}

	// Verify mcp.json was created
	mcpConfigPath := filepath.Join(vscodeDir, "mcp.json")
	if _, err := os.Stat(mcpConfigPath); os.IsNotExist(err) {
		t.Error("Expected mcp.json to be created")
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
	mcpConfigPath := filepath.Join(".vscode", "mcp.json")
	info, err := os.Stat(mcpConfigPath)
	if err != nil {
		t.Fatalf("Failed to stat mcp.json: %v", err)
	}

	// Verify file is readable and writable (at minimum)
	mode := info.Mode()
	if mode.Perm()&0600 != 0600 {
		t.Errorf("Expected file to have at least 0600 permissions, got %o", mode.Perm())
	}
}
