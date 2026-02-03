package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var engineLog = logger.New("workflow:engine")

// EngineConfig represents the parsed engine configuration
type EngineConfig struct {
	ID          string
	Version     string
	Model       string
	MaxTurns    string
	Concurrency string // Agent job-level concurrency configuration (YAML format)
	UserAgent   string
	Command     string // Custom executable path (when set, skip installation steps)
	Env         map[string]string
	Steps       []map[string]any
	Config      string
	Args        []string
	Firewall    *FirewallConfig // AWF firewall configuration
	Agent       string          // Agent identifier for copilot --agent flag (copilot engine only)
}

// NetworkPermissions represents network access permissions for workflow execution
// Controls which domains the workflow can access during execution.
//
// The Allowed field specifies which domains/ecosystems are permitted:
//   - nil/not set: Use default ecosystem domains (backwards compatibility)
//   - []: Empty list means deny all network access
//   - ["defaults"]: Use default ecosystem domains
//   - ["defaults", "github", "python"]: Expand and merge multiple ecosystems
//   - ["example.com"]: Allow specific domain only
//
// Examples:
//
//  1. String format - use default domains only:
//     network: defaults
//     Result: NetworkPermissions{Allowed: ["defaults"], ExplicitlyDefined: true}
//
//  2. Object format - specify allowed ecosystems/domains:
//     network:
//     allowed:
//     - defaults      # Expands to default ecosystem domains (certs, JSON schema, Ubuntu, etc.)
//     - github        # Expands to GitHub ecosystem domains (*.githubusercontent.com, etc.)
//     - example.com   # Literal domain
//     Result: NetworkPermissions{Allowed: ["defaults", "github", "example.com"], ExplicitlyDefined: true}
//
//  3. Empty object - deny all network access:
//     network: {}
//     Result: NetworkPermissions{Allowed: [], ExplicitlyDefined: true}
//
// Ecosystem identifiers in the Allowed list are expanded to their corresponding domain lists.
// See GetAllowedDomains() for the list of supported ecosystem identifiers.
type NetworkPermissions struct {
	Allowed           []string        `yaml:"allowed,omitempty"`  // List of allowed domains or ecosystem identifiers (e.g., "defaults", "github", "python")
	Blocked           []string        `yaml:"blocked,omitempty"`  // List of blocked domains (takes precedence over allowed)
	Firewall          *FirewallConfig `yaml:"firewall,omitempty"` // AWF firewall configuration (see firewall.go)
	ExplicitlyDefined bool            `yaml:"-"`                  // Internal flag: true if network field was explicitly set in frontmatter
}

// EngineNetworkConfig combines engine configuration with top-level network permissions
type EngineNetworkConfig struct {
	Engine  *EngineConfig
	Network *NetworkPermissions
}

// ExtractEngineConfig extracts engine configuration from frontmatter, supporting both string and object formats
func (c *Compiler) ExtractEngineConfig(frontmatter map[string]any) (string, *EngineConfig) {
	if engine, exists := frontmatter["engine"]; exists {
		engineLog.Print("Extracting engine configuration from frontmatter")

		// Handle string format (backwards compatibility)
		if engineStr, ok := engine.(string); ok {
			engineLog.Printf("Found engine in string format: %s", engineStr)
			return engineStr, &EngineConfig{ID: engineStr}
		}

		// Handle object format
		if engineObj, ok := engine.(map[string]any); ok {
			engineLog.Print("Found engine in object format, parsing configuration")
			config := &EngineConfig{}

			// Extract required 'id' field
			if id, hasID := engineObj["id"]; hasID {
				if idStr, ok := id.(string); ok {
					config.ID = idStr
				}
			}

			// Extract optional 'version' field
			if version, hasVersion := engineObj["version"]; hasVersion {
				config.Version = stringutil.ParseVersionValue(version)
			}

			// Extract optional 'model' field
			if model, hasModel := engineObj["model"]; hasModel {
				if modelStr, ok := model.(string); ok {
					config.Model = modelStr
				}
			}

			// Extract optional 'max-turns' field
			if maxTurns, hasMaxTurns := engineObj["max-turns"]; hasMaxTurns {
				if maxTurnsInt, ok := maxTurns.(int); ok {
					config.MaxTurns = fmt.Sprintf("%d", maxTurnsInt)
				} else if maxTurnsUint64, ok := maxTurns.(uint64); ok {
					config.MaxTurns = fmt.Sprintf("%d", maxTurnsUint64)
				} else if maxTurnsStr, ok := maxTurns.(string); ok {
					config.MaxTurns = maxTurnsStr
				}
			}

			// Extract optional 'concurrency' field (string or object format)
			if concurrency, hasConcurrency := engineObj["concurrency"]; hasConcurrency {
				if concurrencyStr, ok := concurrency.(string); ok {
					// Simple string format (group name)
					config.Concurrency = fmt.Sprintf("concurrency:\n  group: \"%s\"", concurrencyStr)
				} else if concurrencyObj, ok := concurrency.(map[string]any); ok {
					// Object format with group and optional cancel-in-progress
					var parts []string
					if group, hasGroup := concurrencyObj["group"]; hasGroup {
						if groupStr, ok := group.(string); ok {
							parts = append(parts, fmt.Sprintf("concurrency:\n  group: \"%s\"", groupStr))
						}
					}
					if cancel, hasCancel := concurrencyObj["cancel-in-progress"]; hasCancel {
						if cancelBool, ok := cancel.(bool); ok && cancelBool {
							if len(parts) > 0 {
								parts[0] += "\n  cancel-in-progress: true"
							}
						}
					}
					if len(parts) > 0 {
						config.Concurrency = parts[0]
					}
				}
			}

			// Extract optional 'user-agent' field
			if userAgent, hasUserAgent := engineObj["user-agent"]; hasUserAgent {
				if userAgentStr, ok := userAgent.(string); ok {
					config.UserAgent = userAgentStr
				}
			}

			// Extract optional 'command' field
			if command, hasCommand := engineObj["command"]; hasCommand {
				if commandStr, ok := command.(string); ok {
					config.Command = commandStr
				}
			}

			// Extract optional 'env' field (object/map of strings)
			if env, hasEnv := engineObj["env"]; hasEnv {
				if envMap, ok := env.(map[string]any); ok {
					config.Env = make(map[string]string)
					for key, value := range envMap {
						if valueStr, ok := value.(string); ok {
							config.Env[key] = valueStr
						}
					}
				}
			}

			// Extract optional 'steps' field (array of step objects)
			if steps, hasSteps := engineObj["steps"]; hasSteps {
				if stepsArray, ok := steps.([]any); ok {
					config.Steps = make([]map[string]any, 0, len(stepsArray))
					for _, step := range stepsArray {
						if stepMap, ok := step.(map[string]any); ok {
							config.Steps = append(config.Steps, stepMap)
						}
					}
				}
			}

			// Extract optional 'config' field (additional TOML configuration)
			if config_field, hasConfig := engineObj["config"]; hasConfig {
				if configStr, ok := config_field.(string); ok {
					config.Config = configStr
				}
			}

			// Extract optional 'args' field (array of strings)
			if args, hasArgs := engineObj["args"]; hasArgs {
				if argsArray, ok := args.([]any); ok {
					config.Args = make([]string, 0, len(argsArray))
					for _, arg := range argsArray {
						if argStr, ok := arg.(string); ok {
							config.Args = append(config.Args, argStr)
						}
					}
				} else if argsStrArray, ok := args.([]string); ok {
					config.Args = argsStrArray
				}
			}

			// Extract optional 'agent' field (string - copilot engine only)
			if agent, hasAgent := engineObj["agent"]; hasAgent {
				if agentStr, ok := agent.(string); ok {
					config.Agent = agentStr
					engineLog.Printf("Extracted agent identifier: %s", agentStr)
				}
			}

			// Extract optional 'firewall' field (object format)
			if firewall, hasFirewall := engineObj["firewall"]; hasFirewall {
				if firewallObj, ok := firewall.(map[string]any); ok {
					firewallConfig := &FirewallConfig{}

					// Extract enabled field (defaults to true for copilot)
					if enabled, hasEnabled := firewallObj["enabled"]; hasEnabled {
						if enabledBool, ok := enabled.(bool); ok {
							firewallConfig.Enabled = enabledBool
						}
					}

					// Extract version field (empty = latest)
					if version, hasVersion := firewallObj["version"]; hasVersion {
						if versionStr, ok := version.(string); ok {
							firewallConfig.Version = versionStr
						}
					}

					// Extract log_level field (default: "debug")
					if logLevel, hasLogLevel := firewallObj["log_level"]; hasLogLevel {
						if logLevelStr, ok := logLevel.(string); ok {
							firewallConfig.LogLevel = logLevelStr
						}
					}

					// Extract cleanup_script field (default: "./scripts/ci/cleanup.sh")
					if cleanupScript, hasCleanupScript := firewallObj["cleanup_script"]; hasCleanupScript {
						if cleanupScriptStr, ok := cleanupScript.(string); ok {
							firewallConfig.CleanupScript = cleanupScriptStr
						}
					}

					config.Firewall = firewallConfig
					engineLog.Print("Extracted firewall configuration")
				}
			}

			// Return the ID as the engineSetting for backwards compatibility
			engineLog.Printf("Extracted engine configuration: ID=%s", config.ID)
			return config.ID, config
		}
	}

	// No engine specified
	engineLog.Print("No engine configuration found in frontmatter")
	return "", nil
}

// getAgenticEngine returns the agentic engine for the given engine setting
func (c *Compiler) getAgenticEngine(engineSetting string) (CodingAgentEngine, error) {
	if engineSetting == "" {
		defaultEngine := c.engineRegistry.GetDefaultEngine()
		engineLog.Printf("Using default engine: %s", defaultEngine.GetID())
		return defaultEngine, nil
	}

	engineLog.Printf("Getting agentic engine for setting: %s", engineSetting)

	// First try exact match
	if c.engineRegistry.IsValidEngine(engineSetting) {
		engine, err := c.engineRegistry.GetEngine(engineSetting)
		if err == nil {
			engineLog.Printf("Found engine by exact match: %s", engine.GetID())
		}
		return engine, err
	}

	// Try prefix match for backward compatibility
	engine, err := c.engineRegistry.GetEngineByPrefix(engineSetting)
	if err == nil {
		engineLog.Printf("Found engine by prefix match: %s", engine.GetID())
	} else {
		engineLog.Printf("Failed to find engine for setting %s: %v", engineSetting, err)
	}
	return engine, err
}

// extractEngineConfigFromJSON parses engine configuration from JSON string (from included files)
func (c *Compiler) extractEngineConfigFromJSON(engineJSON string) (*EngineConfig, error) {
	if engineJSON == "" {
		return nil, nil
	}

	var engineData any
	if err := json.Unmarshal([]byte(engineJSON), &engineData); err != nil {
		return nil, fmt.Errorf("failed to parse engine JSON: %w", err)
	}

	// Use the existing ExtractEngineConfig function by creating a temporary frontmatter map
	tempFrontmatter := map[string]any{
		"engine": engineData,
	}

	_, config := c.ExtractEngineConfig(tempFrontmatter)
	return config, nil
}
