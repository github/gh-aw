package workflow

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/github/gh-aw/pkg/logger"
)

var safeInputsLog = logger.New("workflow:safe_inputs")

// safeUint64ToIntForTimeout safely converts uint64 to int for timeout values
// Returns 0 (which signals to use engine defaults) if overflow would occur
func safeUint64ToIntForTimeout(u uint64) int {
	if u > math.MaxInt {
		return 0 // Return 0 (engine default) if value would overflow
	}
	return int(u)
}

// SafeInputsConfig holds the configuration for safe-inputs custom tools
type SafeInputsConfig struct {
	Mode  string // Transport mode: "http" (default) or "stdio"
	Tools map[string]*SafeInputToolConfig
}

// SafeInputToolConfig holds the configuration for a single safe-input tool
type SafeInputToolConfig struct {
	Name        string                     // Tool name (key from the config)
	Description string                     // Required: tool description
	Inputs      map[string]*SafeInputParam // Optional: input parameters
	Script      string                     // JavaScript implementation (mutually exclusive with Run, Py, and Go)
	Run         string                     // Shell script implementation (mutually exclusive with Script, Py, and Go)
	Py          string                     // Python script implementation (mutually exclusive with Script, Run, and Go)
	Go          string                     // Go script implementation (mutually exclusive with Script, Run, and Py)
	Env         map[string]string          // Environment variables (typically for secrets)
	Timeout     int                        // Timeout in seconds for tool execution (default: 60)
}

// SafeInputParam holds the configuration for a tool input parameter
type SafeInputParam struct {
	Type        string // JSON schema type (string, number, boolean, array, object)
	Description string // Description of the parameter
	Required    bool   // Whether the parameter is required
	Default     any    // Default value
}

// SafeInputsMode constants define the available transport modes
const (
	SafeInputsModeHTTP = "http"
)

// SafeInputsDirectory is the directory where safe-inputs files are generated
const SafeInputsDirectory = "/opt/gh-aw/safe-inputs"

// HasSafeInputs checks if safe-inputs are configured
func HasSafeInputs(safeInputs *SafeInputsConfig) bool {
	return safeInputs != nil && len(safeInputs.Tools) > 0
}

// IsSafeInputsHTTPMode checks if safe-inputs is configured to use HTTP mode
// Note: All safe-inputs configurations now use HTTP mode exclusively
func IsSafeInputsHTTPMode(safeInputs *SafeInputsConfig) bool {
	return safeInputs != nil
}

// IsSafeInputsEnabled checks if safe-inputs are configured.
// Safe-inputs are enabled by default when configured in the workflow.
// The workflowData parameter is kept for backward compatibility but is not used.
func IsSafeInputsEnabled(safeInputs *SafeInputsConfig, workflowData *WorkflowData) bool {
	return HasSafeInputs(safeInputs)
}

// parseSafeInputsMap parses safe-inputs configuration from a map.
// This is the shared implementation used by both ParseSafeInputs and extractSafeInputsConfig.
// Returns the config and a boolean indicating whether any tools were found.
func parseSafeInputsMap(safeInputsMap map[string]any) (*SafeInputsConfig, bool) {
	config := &SafeInputsConfig{
		Mode:  "http", // Only HTTP mode is supported
		Tools: make(map[string]*SafeInputToolConfig),
	}

	// Mode field is ignored - only HTTP mode is supported
	// All safe-inputs configurations use HTTP transport

	for toolName, toolValue := range safeInputsMap {
		// Skip the "mode" field as it's not a tool definition
		if toolName == "mode" {
			continue
		}

		toolMap, ok := toolValue.(map[string]any)
		if !ok {
			continue
		}

		toolConfig := &SafeInputToolConfig{
			Name:    toolName,
			Inputs:  make(map[string]*SafeInputParam),
			Env:     make(map[string]string),
			Timeout: 60, // Default timeout: 60 seconds
		}

		// Parse description (required)
		if desc, exists := toolMap["description"]; exists {
			if descStr, ok := desc.(string); ok {
				toolConfig.Description = descStr
			}
		}

		// Parse inputs (optional)
		if inputs, exists := toolMap["inputs"]; exists {
			if inputsMap, ok := inputs.(map[string]any); ok {
				for paramName, paramValue := range inputsMap {
					if paramMap, ok := paramValue.(map[string]any); ok {
						param := &SafeInputParam{
							Type: "string", // default type
						}

						if t, exists := paramMap["type"]; exists {
							if tStr, ok := t.(string); ok {
								param.Type = tStr
							}
						}

						if desc, exists := paramMap["description"]; exists {
							if descStr, ok := desc.(string); ok {
								param.Description = descStr
							}
						}

						if req, exists := paramMap["required"]; exists {
							if reqBool, ok := req.(bool); ok {
								param.Required = reqBool
							}
						}

						if def, exists := paramMap["default"]; exists {
							param.Default = def
						}

						toolConfig.Inputs[paramName] = param
					}
				}
			}
		}

		// Parse script (JavaScript implementation)
		if script, exists := toolMap["script"]; exists {
			if scriptStr, ok := script.(string); ok {
				toolConfig.Script = scriptStr
			}
		}

		// Parse run (shell script implementation)
		if run, exists := toolMap["run"]; exists {
			if runStr, ok := run.(string); ok {
				toolConfig.Run = runStr
			}
		}

		// Parse py (Python script implementation)
		if py, exists := toolMap["py"]; exists {
			if pyStr, ok := py.(string); ok {
				toolConfig.Py = pyStr
			}
		}

		// Parse go (Go script implementation)
		if goScript, exists := toolMap["go"]; exists {
			if goStr, ok := goScript.(string); ok {
				toolConfig.Go = goStr
			}
		}

		// Parse env (environment variables)
		if env, exists := toolMap["env"]; exists {
			if envMap, ok := env.(map[string]any); ok {
				for envName, envValue := range envMap {
					if envStr, ok := envValue.(string); ok {
						toolConfig.Env[envName] = envStr
					}
				}
			}
		}

		// Parse timeout (optional, default is 60 seconds)
		if timeout, exists := toolMap["timeout"]; exists {
			switch t := timeout.(type) {
			case int:
				toolConfig.Timeout = t
			case uint64:
				toolConfig.Timeout = safeUint64ToIntForTimeout(t) // Safe conversion to prevent overflow (alert #414)
			case float64:
				toolConfig.Timeout = int(t)
			case string:
				// Try to parse string as integer
				_, _ = fmt.Sscanf(t, "%d", &toolConfig.Timeout)
			}
		}

		config.Tools[toolName] = toolConfig
	}

	return config, len(config.Tools) > 0
}

// ParseSafeInputs parses safe-inputs configuration from frontmatter (standalone function for testing)
func ParseSafeInputs(frontmatter map[string]any) *SafeInputsConfig {
	if frontmatter == nil {
		return nil
	}

	safeInputs, exists := frontmatter["safe-inputs"]
	if !exists {
		return nil
	}

	safeInputsMap, ok := safeInputs.(map[string]any)
	if !ok {
		return nil
	}

	config, _ := parseSafeInputsMap(safeInputsMap)
	return config
}

// extractSafeInputsConfig extracts safe-inputs configuration from frontmatter
func (c *Compiler) extractSafeInputsConfig(frontmatter map[string]any) *SafeInputsConfig {
	safeInputsLog.Print("Extracting safe-inputs configuration from frontmatter")

	safeInputs, exists := frontmatter["safe-inputs"]
	if !exists {
		return nil
	}

	safeInputsMap, ok := safeInputs.(map[string]any)
	if !ok {
		return nil
	}

	config, hasTools := parseSafeInputsMap(safeInputsMap)
	if !hasTools {
		return nil
	}

	safeInputsLog.Printf("Extracted %d safe-input tools", len(config.Tools))
	return config
}

// mergeSafeInputs merges safe-inputs configuration from imports into the main configuration
func (c *Compiler) mergeSafeInputs(main *SafeInputsConfig, importedConfigs []string) *SafeInputsConfig {
	if main == nil {
		main = &SafeInputsConfig{
			Mode:  "http", // Default to HTTP mode
			Tools: make(map[string]*SafeInputToolConfig),
		}
	}

	for _, configJSON := range importedConfigs {
		if configJSON == "" || configJSON == "{}" {
			continue
		}

		// Merge the imported JSON config
		var importedMap map[string]any
		if err := json.Unmarshal([]byte(configJSON), &importedMap); err != nil {
			safeInputsLog.Printf("Warning: failed to parse imported safe-inputs config: %v", err)
			continue
		}

		// Mode field is ignored - only HTTP mode is supported
		// All safe-inputs configurations use HTTP transport

		// Merge each tool from the imported config
		for toolName, toolValue := range importedMap {
			// Skip mode field as it's already handled
			if toolName == "mode" {
				continue
			}

			// Skip if tool already exists in main config (main takes precedence)
			if _, exists := main.Tools[toolName]; exists {
				safeInputsLog.Printf("Skipping imported tool '%s' - already defined in main config", toolName)
				continue
			}

			toolMap, ok := toolValue.(map[string]any)
			if !ok {
				continue
			}

			toolConfig := &SafeInputToolConfig{
				Name:    toolName,
				Inputs:  make(map[string]*SafeInputParam),
				Env:     make(map[string]string),
				Timeout: 60, // Default timeout: 60 seconds
			}

			// Parse description
			if desc, exists := toolMap["description"]; exists {
				if descStr, ok := desc.(string); ok {
					toolConfig.Description = descStr
				}
			}

			// Parse inputs
			if inputs, exists := toolMap["inputs"]; exists {
				if inputsMap, ok := inputs.(map[string]any); ok {
					for paramName, paramValue := range inputsMap {
						if paramMap, ok := paramValue.(map[string]any); ok {
							param := &SafeInputParam{
								Type: "string",
							}
							if t, exists := paramMap["type"]; exists {
								if tStr, ok := t.(string); ok {
									param.Type = tStr
								}
							}
							if desc, exists := paramMap["description"]; exists {
								if descStr, ok := desc.(string); ok {
									param.Description = descStr
								}
							}
							if req, exists := paramMap["required"]; exists {
								if reqBool, ok := req.(bool); ok {
									param.Required = reqBool
								}
							}
							if def, exists := paramMap["default"]; exists {
								param.Default = def
							}
							toolConfig.Inputs[paramName] = param
						}
					}
				}
			}

			// Parse script
			if script, exists := toolMap["script"]; exists {
				if scriptStr, ok := script.(string); ok {
					toolConfig.Script = scriptStr
				}
			}

			// Parse run
			if run, exists := toolMap["run"]; exists {
				if runStr, ok := run.(string); ok {
					toolConfig.Run = runStr
				}
			}

			// Parse py
			if py, exists := toolMap["py"]; exists {
				if pyStr, ok := py.(string); ok {
					toolConfig.Py = pyStr
				}
			}

			// Parse go
			if goScript, exists := toolMap["go"]; exists {
				if goStr, ok := goScript.(string); ok {
					toolConfig.Go = goStr
				}
			}

			// Parse env
			if env, exists := toolMap["env"]; exists {
				if envMap, ok := env.(map[string]any); ok {
					for envName, envValue := range envMap {
						if envStr, ok := envValue.(string); ok {
							toolConfig.Env[envName] = envStr
						}
					}
				}
			}

			// Parse timeout (optional, default is 60 seconds)
			if timeout, exists := toolMap["timeout"]; exists {
				switch t := timeout.(type) {
				case int:
					toolConfig.Timeout = t
				case uint64:
					toolConfig.Timeout = safeUint64ToIntForTimeout(t) // Safe conversion to prevent overflow (alert #413)
				case float64:
					toolConfig.Timeout = int(t)
				case string:
					// Try to parse string as integer
					_, _ = fmt.Sscanf(t, "%d", &toolConfig.Timeout)
				}
			}

			main.Tools[toolName] = toolConfig
			safeInputsLog.Printf("Merged imported safe-input tool: %s", toolName)
		}
	}

	return main
}
