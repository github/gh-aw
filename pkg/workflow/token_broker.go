package workflow

import (
	"fmt"
	"slices"
)

const tokenBrokerURLEnv = "GH_AW_TOKEN_BROKER_URL"
const tokenBrokerURLExpression = "${{ vars.GH_AW_TOKEN_BROKER_URL || secrets.GH_AW_TOKEN_BROKER_URL }}"

func usesTokenBrokerInference(data *WorkflowData) bool {
	if data == nil || !data.TokenBrokerEnabled {
		return false
	}
	if slices.Contains(fallbackModelProviders(data), LLMProviderGitHub) {
		return true
	}
	engineID := ResolveEngineID(data)
	if engineID == "copilot" {
		return !isCopilotBYOKMode(data, isFirewallEnabled(data))
	}
	provider, known := resolveWorkflowEngineProvider(data, engineID)
	if !known || provider != LLMProviderGitHub {
		return false
	}
	return true
}

func usesTokenBrokerTasks(data *WorkflowData) bool {
	if data == nil || !data.TokenBrokerEnabled || data.SafeOutputs == nil {
		return false
	}
	outputs := data.SafeOutputs
	return outputs.AssignToAgent != nil || outputs.CreateAgentSessions != nil ||
		(outputs.CreateIssues != nil && hasCopilotAssignee(outputs.CreateIssues.Assignees)) ||
		(outputs.CreatePullRequests != nil && hasCopilotAssignee(outputs.CreatePullRequests.Assignees))
}

func (c *Compiler) validateTokenBrokerInference(data *WorkflowData) error {
	if data == nil || !data.TokenBrokerEnabled {
		return nil
	}
	unsupported := func(job string) error {
		return fmt.Errorf("token-broker: true does not support Copilot inference in the %s job: AWF requires a renewable token-broker credential provider, which is not supported by the current runtime. Use a non-Copilot inference provider (including threat detection and evals) for task-only broker authentication, or disable token-broker", job)
	}
	if usesTokenBrokerInference(data) {
		return unsupported("agent")
	}
	if IsDetectionJobEnabled(data.SafeOutputs) && !data.SafeOutputs.ThreatDetection.EngineDisabled {
		engineID := c.getThreatDetectionEngineID(data)
		detection := buildExternalDetectorWorkflowData(data, engineID)
		detection.Model = inheritedDetectionModel(data, engineID)
		if model := data.SafeOutputs.ThreatDetection.Model; model != "" {
			detection.Model = model
		}
		if usesTokenBrokerInference(detection) {
			return unsupported("detection")
		}
	}
	if data.Evals.HasEvals() {
		evals := *data
		evals.AI = c.getEvalsEngineID(data)
		evals.Model = c.resolveEvalsExecutionModel(data)
		if usesTokenBrokerInference(&evals) {
			return unsupported("evals")
		}
		for _, question := range data.Evals.Questions {
			if question.Model == "" {
				continue
			}
			evals.Model = question.Model
			if usesTokenBrokerInference(&evals) {
				return unsupported("evals")
			}
		}
	}
	return nil
}
