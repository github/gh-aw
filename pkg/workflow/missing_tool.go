package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var missingToolLog = logger.New("workflow:missing_tool")

// MissingToolConfig holds configuration for reporting missing tools or functionality
type MissingToolConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	CreateIssue          bool     `yaml:"create-issue,omitempty"` // Whether to create/update issues for missing tools (default: true)
	TitlePrefix          string   `yaml:"title-prefix,omitempty"` // Prefix for issue titles (default: "[missing tool]")
	Labels               []string `yaml:"labels,omitempty"`       // Labels to add to created issues
}

// buildCreateOutputMissingToolJob creates the missing_tool job
func (c *Compiler) buildCreateOutputMissingToolJob(data *WorkflowData, mainJobName string) (*Job, error) {
	missingToolLog.Printf("Building missing_tool job for workflow: %s", data.Name)

	if data.SafeOutputs == nil || data.SafeOutputs.MissingTool == nil {
		return nil, fmt.Errorf("safe-outputs.missing-tool configuration is required")
	}

	// Build custom environment variables specific to missing-tool
	var customEnvVars []string
	if data.SafeOutputs.MissingTool.Max > 0 {
		missingToolLog.Printf("Setting max missing tools limit: %d", data.SafeOutputs.MissingTool.Max)
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_MISSING_TOOL_MAX: %d\n", data.SafeOutputs.MissingTool.Max))
	}

	// Add create-issue configuration
	if data.SafeOutputs.MissingTool.CreateIssue {
		customEnvVars = append(customEnvVars, "          GH_AW_MISSING_TOOL_CREATE_ISSUE: \"true\"\n")
		missingToolLog.Print("create-issue enabled for missing-tool")
	}

	// Add title-prefix configuration
	if data.SafeOutputs.MissingTool.TitlePrefix != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_MISSING_TOOL_TITLE_PREFIX: %q\n", data.SafeOutputs.MissingTool.TitlePrefix))
		missingToolLog.Printf("title-prefix: %s", data.SafeOutputs.MissingTool.TitlePrefix)
	}

	// Add labels configuration
	if len(data.SafeOutputs.MissingTool.Labels) > 0 {
		labelsJSON, err := json.Marshal(data.SafeOutputs.MissingTool.Labels)
		if err == nil {
			customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_MISSING_TOOL_LABELS: %q\n", string(labelsJSON)))
			missingToolLog.Printf("labels: %v", data.SafeOutputs.MissingTool.Labels)
		}
	}

	// Add workflow metadata for consistency
	customEnvVars = append(customEnvVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID)...)

	// Create outputs for the job
	outputs := map[string]string{
		"tools_reported": "${{ steps.missing_tool.outputs.tools_reported }}",
		"total_count":    "${{ steps.missing_tool.outputs.total_count }}",
	}

	// Build the job condition using BuildSafeOutputType
	jobCondition := BuildSafeOutputType("missing_tool")

	// Set permissions based on whether issue creation is enabled
	permissions := NewPermissionsContentsRead()
	if data.SafeOutputs.MissingTool.CreateIssue {
		// Add issues:write permission for creating/updating issues
		permissions.Set(PermissionIssues, PermissionWrite)
		missingToolLog.Print("Added issues:write permission for create-issue functionality")
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:       "missing_tool",
		StepName:      "Record Missing Tool",
		StepID:        "missing_tool",
		MainJobName:   mainJobName,
		CustomEnvVars: customEnvVars,
		Script:        "const { main } = require('/opt/gh-aw/actions/missing_tool.cjs'); await main();",
		Permissions:   permissions,
		Outputs:       outputs,
		Condition:     jobCondition,
		Token:         data.SafeOutputs.MissingTool.GitHubToken,
	})
}

// parseMissingToolConfig handles missing-tool configuration
func (c *Compiler) parseMissingToolConfig(outputMap map[string]any) *MissingToolConfig {
	if configData, exists := outputMap["missing-tool"]; exists {
		// Handle the case where configData is false (explicitly disabled)
		if configBool, ok := configData.(bool); ok && !configBool {
			missingToolLog.Print("Missing-tool configuration explicitly disabled")
			return nil
		}

		// Create config with no defaults - they will be applied in JavaScript
		missingToolConfig := &MissingToolConfig{}

		// Handle the case where configData is nil (missing-tool: with no value)
		if configData == nil {
			missingToolLog.Print("Missing-tool configuration enabled with defaults")
			// Set create-issue to true as default when missing-tool is enabled
			missingToolConfig.CreateIssue = true
			missingToolConfig.TitlePrefix = "[missing tool]"
			missingToolConfig.Labels = []string{}
			return missingToolConfig
		}

		if configMap, ok := configData.(map[string]any); ok {
			missingToolLog.Print("Parsing missing-tool configuration from map")
			// Parse common base fields with default max of 0 (no limit)
			c.parseBaseSafeOutputConfig(configMap, &missingToolConfig.BaseSafeOutputConfig, 0)

			// Parse create-issue field, default to true if not specified
			if createIssue, exists := configMap["create-issue"]; exists {
				if createIssueBool, ok := createIssue.(bool); ok {
					missingToolConfig.CreateIssue = createIssueBool
					missingToolLog.Printf("create-issue: %v", createIssueBool)
				}
			} else {
				// Default to true when config map exists but create-issue not specified
				missingToolConfig.CreateIssue = true
			}

			// Parse title-prefix field, default to "[missing tool]" if not specified
			if titlePrefix, exists := configMap["title-prefix"]; exists {
				if titlePrefixStr, ok := titlePrefix.(string); ok {
					missingToolConfig.TitlePrefix = titlePrefixStr
					missingToolLog.Printf("title-prefix: %s", titlePrefixStr)
				}
			} else {
				// Default title prefix
				missingToolConfig.TitlePrefix = "[missing tool]"
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
					missingToolConfig.Labels = labelStrings
					missingToolLog.Printf("labels: %v", labelStrings)
				}
			} else {
				// Default to empty labels
				missingToolConfig.Labels = []string{}
			}
		}

		return missingToolConfig
	}

	return nil
}
