package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var missingDataLog = logger.New("workflow:missing_data")

// MissingDataConfig holds configuration for reporting missing data required to achieve goals
type MissingDataConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	CreateIssue          bool     `yaml:"create-issue,omitempty"` // Whether to create/update issues for missing data (default: true)
	TitlePrefix          string   `yaml:"title-prefix,omitempty"` // Prefix for issue titles (default: "[missing data]")
	Labels               []string `yaml:"labels,omitempty"`       // Labels to add to created issues
}

// buildCreateOutputMissingDataJob creates the missing_data job
func (c *Compiler) buildCreateOutputMissingDataJob(data *WorkflowData, mainJobName string) (*Job, error) {
	missingDataLog.Printf("Building missing_data job for workflow: %s", data.Name)

	if data.SafeOutputs == nil || data.SafeOutputs.MissingData == nil {
		return nil, fmt.Errorf("safe-outputs.missing-data configuration is required")
	}

	// Build custom environment variables specific to missing-data
	var customEnvVars []string
	if data.SafeOutputs.MissingData.Max > 0 {
		missingDataLog.Printf("Setting max missing data limit: %d", data.SafeOutputs.MissingData.Max)
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_MISSING_DATA_MAX: %d\n", data.SafeOutputs.MissingData.Max))
	}

	// Add create-issue configuration
	if data.SafeOutputs.MissingData.CreateIssue {
		customEnvVars = append(customEnvVars, "          GH_AW_MISSING_DATA_CREATE_ISSUE: \"true\"\n")
		missingDataLog.Print("create-issue enabled for missing-data")
	}

	// Add title-prefix configuration
	if data.SafeOutputs.MissingData.TitlePrefix != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_MISSING_DATA_TITLE_PREFIX: %q\n", data.SafeOutputs.MissingData.TitlePrefix))
		missingDataLog.Printf("title-prefix: %s", data.SafeOutputs.MissingData.TitlePrefix)
	}

	// Add labels configuration
	if len(data.SafeOutputs.MissingData.Labels) > 0 {
		labelsJSON, err := json.Marshal(data.SafeOutputs.MissingData.Labels)
		if err == nil {
			customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_MISSING_DATA_LABELS: %q\n", string(labelsJSON)))
			missingDataLog.Printf("labels: %v", data.SafeOutputs.MissingData.Labels)
		}
	}

	// Add workflow metadata for consistency
	customEnvVars = append(customEnvVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID)...)

	// Create outputs for the job
	outputs := map[string]string{
		"data_reported": "${{ steps.missing_data.outputs.data_reported }}",
		"total_count":   "${{ steps.missing_data.outputs.total_count }}",
	}

	// Build the job condition using BuildSafeOutputType
	jobCondition := BuildSafeOutputType("missing_data")

	// Set permissions based on whether issue creation is enabled
	permissions := NewPermissionsContentsRead()
	if data.SafeOutputs.MissingData.CreateIssue {
		// Add issues:write permission for creating/updating issues
		permissions.Set(PermissionIssues, PermissionWrite)
		missingDataLog.Print("Added issues:write permission for create-issue functionality")
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:       "missing_data",
		StepName:      "Record Missing Data",
		StepID:        "missing_data",
		MainJobName:   mainJobName,
		CustomEnvVars: customEnvVars,
		Script:        "const { main } = require('/opt/gh-aw/actions/missing_data.cjs'); await main();",
		Permissions:   permissions,
		Outputs:       outputs,
		Condition:     jobCondition,
		Token:         data.SafeOutputs.MissingData.GitHubToken,
	})
}

// parseMissingDataConfig handles missing-data configuration
func (c *Compiler) parseMissingDataConfig(outputMap map[string]any) *MissingDataConfig {
	if configData, exists := outputMap["missing-data"]; exists {
		// Handle the case where configData is false (explicitly disabled)
		if configBool, ok := configData.(bool); ok && !configBool {
			missingDataLog.Print("Missing-data configuration explicitly disabled")
			return nil
		}

		// Create config with no defaults - they will be applied in JavaScript
		missingDataConfig := &MissingDataConfig{}

		// Handle the case where configData is nil (missing-data: with no value)
		if configData == nil {
			missingDataLog.Print("Missing-data configuration enabled with defaults")
			// Set create-issue to true as default when missing-data is enabled
			missingDataConfig.CreateIssue = true
			missingDataConfig.TitlePrefix = "[missing data]"
			missingDataConfig.Labels = []string{}
			return missingDataConfig
		}

		if configMap, ok := configData.(map[string]any); ok {
			missingDataLog.Print("Parsing missing-data configuration from map")
			// Parse common base fields with default max of 0 (no limit)
			c.parseBaseSafeOutputConfig(configMap, &missingDataConfig.BaseSafeOutputConfig, 0)

			// Parse create-issue field, default to true if not specified
			if createIssue, exists := configMap["create-issue"]; exists {
				if createIssueBool, ok := createIssue.(bool); ok {
					missingDataConfig.CreateIssue = createIssueBool
					missingDataLog.Printf("create-issue: %v", createIssueBool)
				}
			} else {
				// Default to true when config map exists but create-issue not specified
				missingDataConfig.CreateIssue = true
			}

			// Parse title-prefix field, default to "[missing data]" if not specified
			if titlePrefix, exists := configMap["title-prefix"]; exists {
				if titlePrefixStr, ok := titlePrefix.(string); ok {
					missingDataConfig.TitlePrefix = titlePrefixStr
					missingDataLog.Printf("title-prefix: %s", titlePrefixStr)
				}
			} else {
				// Default title prefix
				missingDataConfig.TitlePrefix = "[missing data]"
			}

			// Parse labels field, default to empty array if not specified
			if labels, exists := configMap["labels"]; exists {
				if labelsArray, ok := labels.([]any); ok {
					var labelStrings []string
					for _, label := range labelsArray {
						if labelStr, ok := label.(string); ok {
							labelStrings = append(labelStrings, labelStr)
						}
					}
					missingDataConfig.Labels = labelStrings
					missingDataLog.Printf("labels: %v", labelStrings)
				}
			} else {
				// Default to empty labels
				missingDataConfig.Labels = []string{}
			}
		}

		return missingDataConfig
	}

	return nil
}
