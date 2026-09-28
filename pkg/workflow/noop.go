package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var noopLog = logger.New("workflow:noop")

// NoOpConfig holds configuration for no-op safe output (logging only)
type NoOpConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	ReportAsIssue        *string `yaml:"report-as-issue,omitempty"` // Controls whether noop runs are reported as issue comments (default: true)
	// Implicit is true when this configuration was injected by default because
	// `safe-outputs` was present but no `noop` entry was authored. Implicit noop
	// configurations do not require the agentics-maintenance workflow, so that
	// compilation stays lazy and does not add maintenance files to repositories
	// that never opted into expiring safe outputs.
	Implicit bool `yaml:"-"`
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
			noopConfig.Max = defaultIntStr(2)
			// Set default report-as-issue to true
			trueVal := "true"
			noopConfig.ReportAsIssue = &trueVal
			noopLog.Print("Noop enabled with default max=2, report-as-issue=true")
			return noopConfig
		}

		if configMap, ok := configData.(map[string]any); ok {
			// Pre-process report-as-issue as a templatable bool
			if err := preprocessBoolFieldAsString(configMap, "report-as-issue", noopLog); err != nil {
				noopLog.Printf("Invalid report-as-issue value: %v", err)
				return nil
			}

			// Parse common base fields with default max of 2
			c.parseBaseSafeOutputConfig(configMap, &noopConfig.BaseSafeOutputConfig, 2)

			// Parse report-as-issue field with default of true
			if reportAsIssue, ok := configMap["report-as-issue"].(string); ok {
				noopConfig.ReportAsIssue = &reportAsIssue
				noopLog.Printf("report-as-issue explicitly set to: %s", reportAsIssue)
			} else {
				// Default to true
				trueVal := "true"
				noopConfig.ReportAsIssue = &trueVal
				noopLog.Print("report-as-issue not specified, defaulting to true")
			}

			if noopConfig.Max != nil {
				noopLog.Printf("Parsed noop configuration: max=%s, report-as-issue=%s", *noopConfig.Max, *noopConfig.ReportAsIssue)
			}
		}

		return noopConfig
	}

	noopLog.Print("No noop configuration found")
	return nil
}
