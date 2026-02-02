package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var dispatchWorkflowLog = logger.New("workflow:dispatch_workflow")

// DispatchWorkflowConfig holds configuration for dispatching workflows from agent output
type DispatchWorkflowConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Workflows            []string          `yaml:"workflows,omitempty"`      // List of workflow names (without .md extension) to allow dispatching
	WorkflowFiles        map[string]string `yaml:"workflow_files,omitempty"` // Map of workflow name to file extension (.lock.yml or .yml) - populated at compile time
}

// parseDispatchWorkflowConfig handles dispatch-workflow configuration
func (c *Compiler) parseDispatchWorkflowConfig(outputMap map[string]any) *DispatchWorkflowConfig {
	dispatchWorkflowLog.Print("Parsing dispatch-workflow configuration")
	if configData, exists := outputMap["dispatch-workflow"]; exists {
		dispatchWorkflowConfig := &DispatchWorkflowConfig{}

		// Check if it's a list of workflow names (array format)
		if workflowsArray, ok := configData.([]any); ok {
			dispatchWorkflowLog.Printf("Found dispatch-workflow as array with %d workflows", len(workflowsArray))
			for _, workflow := range workflowsArray {
				if workflowStr, ok := workflow.(string); ok {
					dispatchWorkflowConfig.Workflows = append(dispatchWorkflowConfig.Workflows, workflowStr)
				}
			}
			// Set default max to 1
			dispatchWorkflowConfig.Max = 1
			return dispatchWorkflowConfig
		}

		// Check if it's a map with configuration options
		if configMap, ok := configData.(map[string]any); ok {
			dispatchWorkflowLog.Print("Found dispatch-workflow config map")

			// Parse workflows list
			if workflows, exists := configMap["workflows"]; exists {
				if workflowsArray, ok := workflows.([]any); ok {
					for _, workflow := range workflowsArray {
						if workflowStr, ok := workflow.(string); ok {
							dispatchWorkflowConfig.Workflows = append(dispatchWorkflowConfig.Workflows, workflowStr)
						}
					}
				}
			}

			// Parse common base fields with default max of 1
			c.parseBaseSafeOutputConfig(configMap, &dispatchWorkflowConfig.BaseSafeOutputConfig, 1)

			// Cap max at 50 (absolute maximum allowed)
			if dispatchWorkflowConfig.Max > 50 {
				dispatchWorkflowLog.Printf("Max value %d exceeds limit, capping at 50", dispatchWorkflowConfig.Max)
				dispatchWorkflowConfig.Max = 50
			}

			dispatchWorkflowLog.Printf("Parsed dispatch-workflow config: max=%d, workflows=%v",
				dispatchWorkflowConfig.Max, dispatchWorkflowConfig.Workflows)
			return dispatchWorkflowConfig
		}
	}

	return nil
}
