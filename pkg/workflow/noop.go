package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var noopLog = logger.New("workflow:noop")

// NoOpConfig holds configuration for no-op safe output (logging only)
type NoOpConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
}

// parseNoOpConfig handles noop configuration
func (c *Compiler) parseNoOpConfig(outputMap map[string]any) *NoOpConfig {
	if configData, exists := outputMap["noop"]; exists {
		noopLog.Print("Parsing noop configuration from safe-outputs")

		// Handle the case where configData is false (explicitly disabled)
		if configBool, ok := configData.(bool); ok && !configBool {
			noopLog.Print("Noop explicitly disabled")
			return nil
		}

		noopConfig := &NoOpConfig{}

		// Handle the case where configData is nil (noop: with no value)
		if configData == nil {
			// Set default max for noop messages
			noopConfig.Max = 1
			noopLog.Print("Noop enabled with default max=1")
			return noopConfig
		}

		if configMap, ok := configData.(map[string]any); ok {
			// Parse common base fields with default max of 1
			c.parseBaseSafeOutputConfig(configMap, &noopConfig.BaseSafeOutputConfig, 1)
			noopLog.Printf("Parsed noop configuration: max=%d", noopConfig.Max)
		}

		return noopConfig
	}

	noopLog.Print("No noop configuration found")
	return nil
}
