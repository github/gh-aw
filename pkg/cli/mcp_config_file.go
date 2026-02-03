package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpConfigLog = logger.New("cli:mcp_config_file")

// VSCodeMCPServer represents a single MCP server configuration for VSCode mcp.json
type VSCodeMCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	CWD     string   `json:"cwd,omitempty"`
}

// MCPConfig represents the structure of mcp.json
type MCPConfig struct {
	Servers map[string]VSCodeMCPServer `json:"servers"`
}

// ensureMCPConfig creates or updates .vscode/mcp.json with gh-aw MCP server configuration
func ensureMCPConfig(verbose bool) error {
	mcpConfigLog.Print("Creating or updating .vscode/mcp.json")

	// Create .vscode directory if it doesn't exist
	vscodeDir := ".vscode"
	if err := os.MkdirAll(vscodeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .vscode directory: %w", err)
	}
	mcpConfigLog.Printf("Ensured directory exists: %s", vscodeDir)

	mcpConfigPath := filepath.Join(vscodeDir, "mcp.json")

	// Read existing config if it exists
	var config MCPConfig
	if data, err := os.ReadFile(mcpConfigPath); err == nil {
		mcpConfigLog.Printf("Reading existing config from: %s", mcpConfigPath)
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse existing mcp.json: %w", err)
		}
	} else {
		mcpConfigLog.Print("No existing config found, creating new one")
		config.Servers = make(map[string]VSCodeMCPServer)
	}

	// Add or update gh-aw MCP server configuration
	ghAwServerName := "github-agentic-workflows"
	ghAwConfig := VSCodeMCPServer{
		Command: "gh",
		Args:    []string{"aw", "mcp-server"},
		CWD:     "${workspaceFolder}",
	}

	// Check if the server is already configured
	if existingConfig, exists := config.Servers[ghAwServerName]; exists {
		mcpConfigLog.Printf("Server '%s' already exists in config", ghAwServerName)
		// Check if configuration is different
		existingJSON, _ := json.Marshal(existingConfig)
		newJSON, _ := json.Marshal(ghAwConfig)
		if string(existingJSON) == string(newJSON) {
			mcpConfigLog.Print("Configuration is identical, skipping update")
			if verbose {
				fmt.Fprintf(os.Stderr, "MCP server '%s' already configured in %s\n", ghAwServerName, mcpConfigPath)
			}
			return nil
		}
		mcpConfigLog.Print("Configuration differs, updating")
	}

	config.Servers[ghAwServerName] = ghAwConfig

	// Write config file with proper indentation
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mcp.json: %w", err)
	}

	if err := os.WriteFile(mcpConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}
	mcpConfigLog.Printf("Wrote config to: %s", mcpConfigPath)

	return nil
}
