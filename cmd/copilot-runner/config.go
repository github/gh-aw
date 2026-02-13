// This file defines the configuration structures for the copilot-runner binary.
//
// The runner reads a JSON config file that specifies all parameters for the
// Copilot SDK session, including model, tools, MCP servers, and output settings.

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// RunnerConfig represents the JSON configuration for the copilot-runner binary.
type RunnerConfig struct {
	// CLIPath is the path to the Copilot CLI binary
	CLIPath string `json:"cli_path"`

	// Model is the LLM model to use
	Model string `json:"model,omitempty"`

	// PromptFile is the path to the prompt text file
	PromptFile string `json:"prompt_file"`

	// SystemMessage is an optional system message
	SystemMessage string `json:"system_message,omitempty"`

	// AvailableTools lists tools to make available
	AvailableTools []string `json:"available_tools,omitempty"`

	// ExcludedTools lists tools to exclude
	ExcludedTools []string `json:"excluded_tools,omitempty"`

	// MCPConfigPath is the path to the MCP config JSON file
	MCPConfigPath string `json:"mcp_config_path,omitempty"`

	// WorkingDirectory is the working directory for the session
	WorkingDirectory string `json:"working_directory,omitempty"`

	// AddDirs is a list of directories to grant file access to
	AddDirs []string `json:"add_dirs,omitempty"`

	// LogDir is the directory for log output
	LogDir string `json:"log_dir"`

	// ShareFile is the path to write the conversation markdown
	ShareFile string `json:"share_file,omitempty"`

	// AutoApprovePermissions auto-approves all tool permission requests
	AutoApprovePermissions bool `json:"auto_approve_permissions"`

	// MaxTurns limits the number of turns (0 = unlimited)
	MaxTurns int `json:"max_turns,omitempty"`

	// TimeoutSeconds is the maximum execution time
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`

	// DisableBuiltinMCPs disables built-in MCP servers
	DisableBuiltinMCPs bool `json:"disable_builtin_mcps"`

	// AllowAllPaths allows write access to all paths
	AllowAllPaths bool `json:"allow_all_paths,omitempty"`

	// Agent is the agent identifier
	Agent string `json:"agent,omitempty"`
}

// LoadConfig reads and parses a JSON config file.
func LoadConfig(path string) (*RunnerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var config RunnerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Validate required fields
	if config.CLIPath == "" {
		return nil, fmt.Errorf("cli_path is required in config")
	}
	if config.PromptFile == "" {
		return nil, fmt.Errorf("prompt_file is required in config")
	}
	if config.LogDir == "" {
		return nil, fmt.Errorf("log_dir is required in config")
	}

	return &config, nil
}
