package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var dispatchWorkflowLog = logger.New("workflow:dispatch_workflow")

const defaultDispatchWorkflowAllowedRef = "refs/heads/${{ github.event.repository.default_branch }}"

// DispatchWorkflowConfig holds configuration for dispatching workflows from agent output
type DispatchWorkflowConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Workflows            []string          `yaml:"workflows,omitempty"`            // List of workflow names (without .md extension) to allow dispatching
	WorkflowFiles        map[string]string `yaml:"workflow_files,omitempty"`       // Map of workflow name to file extension (.lock.yml or .yml) - populated at compile time
	AwContextWorkflows   []string          `yaml:"aw_context_workflows,omitempty"` // Compiler-managed workflows that accept caller context - populated at compile time
	WorkQueueWorkflows   []string          `yaml:"work_queue_workflows,omitempty"` // Explicit queue workers that accept work_queue_claim - populated at compile time
	TargetRepoSlug       string            `yaml:"target-repo,omitempty"`          // Target repository for cross-repo dispatch (owner/repo or GitHub Actions expression)
	AllowedRepos         []string          `yaml:"allowed-repos,omitempty"`        // Allowlist for cross-repository dispatch targets
	AllowedRefs          []string          `yaml:"allowed-refs,omitempty"`         // Allowlist of ref globs for per-call message.ref overrides
	TargetRef            string            `yaml:"target-ref,omitempty"`           // Target ref for cross-repo dispatch; overrides the caller's GITHUB_REF
}

// parseDispatchWorkflowConfig handles dispatch-workflow configuration
func (c *Compiler) parseDispatchWorkflowConfig(outputMap map[string]any) *DispatchWorkflowConfig {
	dispatchWorkflowLog.Print("Parsing dispatch-workflow configuration")
	if configData, exists := outputMap["dispatch-workflow"]; exists {
		dispatchWorkflowConfig := &DispatchWorkflowConfig{}

		// Check if it's a list of workflow names (array format)
		if workflowsArray, ok := configData.([]any); ok {
			return parseDispatchWorkflowArray(workflowsArray)
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

			// Parse target-ref (optional ref for cross-repo dispatch)
			if targetRef, ok := configMap["target-ref"].(string); ok && targetRef != "" {
				dispatchWorkflowConfig.TargetRef = targetRef
			}

			// Parse target-repo (optional cross-repo dispatch target)
			dispatchWorkflowConfig.TargetRepoSlug = extractStringFromMap(configMap, "target-repo", dispatchWorkflowLog)
			dispatchWorkflowConfig.AllowedRepos = ParseStringArrayOrExprFromConfig(configMap, "allowed-repos", dispatchWorkflowLog)
			dispatchWorkflowConfig.AllowedRefs = ParseStringArrayOrExprFromConfig(configMap, "allowed-refs", dispatchWorkflowLog)
			if dispatchWorkflowConfig.AllowedRefs == nil {
				dispatchWorkflowConfig.AllowedRefs = []string{defaultDispatchWorkflowAllowedRef}
			}

			// Cap max at 50 (absolute maximum allowed) – only for literal integer values
			if maxVal := templatableIntValue(dispatchWorkflowConfig.Max); maxVal > 50 {
				dispatchWorkflowLog.Printf("Max value %d exceeds limit, capping at 50", maxVal)
				dispatchWorkflowConfig.Max = defaultIntStr(50)
			}

			dispatchWorkflowLog.Printf("Parsed dispatch-workflow config: max=%v, workflows=%v",
				dispatchWorkflowConfig.Max, dispatchWorkflowConfig.Workflows)
			return dispatchWorkflowConfig
		}
	}

	return nil
}

func parseDispatchWorkflowArray(workflows []any) *DispatchWorkflowConfig {
	dispatchWorkflowLog.Printf("Found dispatch-workflow as array with %d workflows", len(workflows))
	config := &DispatchWorkflowConfig{
		BaseSafeOutputConfig: BaseSafeOutputConfig{Max: defaultIntStr(1)},
		AllowedRefs:          []string{defaultDispatchWorkflowAllowedRef},
	}
	for _, workflow := range workflows {
		if workflowName, ok := workflow.(string); ok {
			config.Workflows = append(config.Workflows, workflowName)
		}
	}
	return config
}
