package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpConfigLog = logger.New("cli:mcp_config_file")

// mcpConfigFilePath is the path to the MCP configuration file used by GitHub Copilot CLI.
const mcpConfigFilePath = ".github/mcp.json"

// VSCodeMCPServer represents a single MCP server configuration for VSCode mcp.json
type VSCodeMCPServer struct {
	Type    string   `json:"type,omitempty"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Tools   []string `json:"tools,omitempty"`
	CWD     string   `json:"cwd,omitempty"`
}

// MCPConfig represents the structure of .github/mcp.json for Claude Code.
type MCPConfig struct {
	MCPServers map[string]VSCodeMCPServer `json:"mcpServers,omitempty"`
	// Servers is a legacy key kept for backward-compatible reads of existing mcp config files.
	Servers map[string]VSCodeMCPServer `json:"servers,omitempty"`
}

// ensureMCPConfig creates or updates .github/mcp.json with gh-aw MCP server configuration.
func ensureMCPConfig(verbose bool) error {
	mcpConfigLog.Print("Creating .github/mcp.json")

	mcpConfigPath := mcpConfigFilePath

	// Add or update gh-aw MCP server configuration
	ghAwServerName := "github-agentic-workflows"
	ghAwConfig := VSCodeMCPServer{
		Type:    "local",
		Command: "gh",
		Args:    []string{"aw", "mcp-server"},
		Tools:   []string{"compile", "audit", "logs", "inspect", "status", "audit-diff", "work-queue"},
	}

	// Check if file already exists
	if data, err := os.ReadFile(mcpConfigPath); err == nil {
		mcpConfigLog.Printf("File already exists: %s", mcpConfigPath)

		var config MCPConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse existing mcp.json: %w", err)
		}

		servers := selectMCPServerMap(&config, ghAwServerName)
		existingConfig := (*servers)[ghAwServerName]
		updatedConfig, changed := mergeRequiredMCPServerFields(existingConfig, ghAwConfig)
		if !changed {
			mcpConfigLog.Print("Configuration is identical, skipping")
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("MCP server '%s' already configured in %s", ghAwServerName, mcpConfigPath)))
			}
			return nil
		}
		(*servers)[ghAwServerName] = updatedConfig

		if err := writeMCPConfigFile(mcpConfigPath, config); err != nil {
			return err
		}
		mcpConfigLog.Printf("Updated existing file: %s", mcpConfigPath)
		return nil
	}

	// File doesn't exist - create it
	mcpConfigLog.Print("No existing config found, creating new one")
	config := MCPConfig{
		MCPServers: make(map[string]VSCodeMCPServer),
	}
	config.MCPServers[ghAwServerName] = ghAwConfig

	if err := fileutil.EnsureParentDir(mcpConfigPath, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create mcp config directory: %w", err)
	}

	if err := writeMCPConfigFile(mcpConfigPath, config); err != nil {
		return err
	}
	mcpConfigLog.Printf("Created new file: %s", mcpConfigPath)

	return nil
}

func selectMCPServerMap(config *MCPConfig, serverName string) *map[string]VSCodeMCPServer {
	if config.MCPServers != nil {
		if _, exists := config.MCPServers[serverName]; exists {
			return &config.MCPServers
		}
	}
	if config.Servers != nil {
		if _, exists := config.Servers[serverName]; exists {
			return &config.Servers
		}
	}

	switch {
	case config.MCPServers != nil:
		return &config.MCPServers
	case config.Servers != nil:
		return &config.Servers
	default:
		config.MCPServers = make(map[string]VSCodeMCPServer)
		return &config.MCPServers
	}
}

func mergeRequiredMCPServerFields(existingConfig, requiredConfig VSCodeMCPServer) (VSCodeMCPServer, bool) {
	updatedConfig := VSCodeMCPServer{
		Type:    existingConfig.Type,
		Command: existingConfig.Command,
		Args:    append([]string(nil), existingConfig.Args...),
		Tools:   append([]string(nil), existingConfig.Tools...),
		CWD:     existingConfig.CWD,
	}
	changed := false

	if updatedConfig.Type != requiredConfig.Type {
		updatedConfig.Type = requiredConfig.Type
		changed = true
	}
	if updatedConfig.Command != requiredConfig.Command {
		updatedConfig.Command = requiredConfig.Command
		changed = true
	}
	if !reflect.DeepEqual(updatedConfig.Args, requiredConfig.Args) {
		updatedConfig.Args = append([]string(nil), requiredConfig.Args...)
		changed = true
	}
	if !reflect.DeepEqual(updatedConfig.Tools, requiredConfig.Tools) {
		updatedConfig.Tools = append([]string(nil), requiredConfig.Tools...)
		changed = true
	}

	return updatedConfig, changed
}

func writeMCPConfigFile(mcpConfigPath string, config MCPConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mcp.json: %w", err)
	}

	if err := os.WriteFile(mcpConfigPath, data, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write mcp.json: %w", err)
	}

	return nil
}
