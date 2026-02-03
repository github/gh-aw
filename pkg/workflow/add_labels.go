package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var addLabelsLog = logger.New("workflow:add_labels")

// AddLabelsConfig holds configuration for adding labels to issues/PRs from agent output
type AddLabelsConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	Allowed                []string `yaml:"allowed,omitempty"` // Optional list of allowed labels. Labels will be created if they don't already exist in the repository. If omitted, any labels are allowed (including creating new ones).
}

// parseAddLabelsConfig handles add-labels configuration
func (c *Compiler) parseAddLabelsConfig(outputMap map[string]any) *AddLabelsConfig {
	// Check if the key exists
	if _, exists := outputMap["add-labels"]; !exists {
		return nil
	}

	addLabelsLog.Print("Parsing add-labels configuration")

	// Unmarshal into typed config struct
	var config AddLabelsConfig
	if err := unmarshalConfig(outputMap, "add-labels", &config, addLabelsLog); err != nil {
		addLabelsLog.Printf("Failed to unmarshal config: %v", err)
		// Handle null case: create empty config (allows any labels)
		addLabelsLog.Print("Using empty configuration (allows any labels)")
		return &AddLabelsConfig{}
	}

	addLabelsLog.Printf("Parsed configuration: allowed_count=%d, target=%s", len(config.Allowed), config.Target)

	return &config
}

// buildAddLabelsJob creates the add_labels job
func (c *Compiler) buildAddLabelsJob(data *WorkflowData, mainJobName string) (*Job, error) {
	addLabelsLog.Printf("Building add_labels job for workflow: %s, main_job: %s", data.Name, mainJobName)

	if data.SafeOutputs == nil || data.SafeOutputs.AddLabels == nil {
		return nil, fmt.Errorf("safe-outputs configuration is required")
	}

	cfg := data.SafeOutputs.AddLabels

	// Build list job config
	listJobConfig := ListJobConfig{
		SafeOutputTargetConfig: cfg.SafeOutputTargetConfig,
		Allowed:                cfg.Allowed,
	}

	// Use shared builder for list-based safe-output jobs
	return c.BuildListSafeOutputJob(data, mainJobName, listJobConfig, cfg.BaseSafeOutputConfig, ListJobBuilderConfig{
		JobName:     "add_labels",
		StepName:    "Add Labels",
		StepID:      "add_labels",
		EnvPrefix:   "GH_AW_LABELS",
		OutputName:  "labels_added",
		Script:      getAddLabelsScript(),
		Permissions: NewPermissionsContentsReadIssuesWritePRWrite(),
		DefaultMax:  3,
	})
}
