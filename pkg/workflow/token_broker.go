package workflow

import (
	"errors"
	"slices"
)

const tokenBrokerURLEnv = "GH_AW_TOKEN_BROKER_URL"
const tokenBrokerURLExpression = "${{ vars.GH_AW_TOKEN_BROKER_URL || secrets.GH_AW_TOKEN_BROKER_URL }}"
const tokenBrokerCopilotRequestOutput = "${{ steps.token_broker_copilot_request.outputs.token }}"

var (
	errTokenBrokerInferenceRequiresRenewal  = errors.New("token-broker.copilot-request does not support Copilot inference in the main agent job: AWF requires a renewable credential provider, which is not supported by the current runtime")
	errTokenBrokerRequestPermissionConflict = errors.New("token-broker.copilot-request is incompatible with permissions.copilot-requests: write")
)

func usesTokenBrokerInference(data *WorkflowData) bool {
	if !tokenBrokerRequestEnabled(data) {
		return false
	}
	if slices.Contains(fallbackModelProviders(data), LLMProviderGitHub) {
		return true
	}
	engineID := ResolveEngineID(data)
	if engineID == "copilot" && isCopilotBYOKMode(data, isFirewallEnabled(data)) {
		return false
	}
	if provider, known := resolveWorkflowEngineProvider(data, engineID); known {
		return provider == LLMProviderGitHub
	}
	if engineID == "copilot" {
		return true
	}
	return false
}

func tokenBrokerRequestEnabled(data *WorkflowData) bool {
	return data != nil && data.TokenBrokerCopilotRequest
}

func usesTokenBrokerTasks(data *WorkflowData) bool {
	if data == nil || !data.TokenBrokerCopilotTask || data.SafeOutputs == nil {
		return false
	}
	outputs := data.SafeOutputs
	return outputs.AssignToAgent != nil || outputs.CreateAgentSessions != nil ||
		(outputs.CreateIssues != nil && hasCopilotAssignee(outputs.CreateIssues.Assignees)) ||
		(outputs.CreatePullRequests != nil && hasCopilotAssignee(outputs.CreatePullRequests.Assignees))
}

func (c *Compiler) validateTokenBrokerInference(data *WorkflowData) error {
	if !tokenBrokerRequestEnabled(data) {
		return nil
	}
	if usesTokenBrokerInference(data) {
		return errTokenBrokerInferenceRequiresRenewal
	}
	return nil
}

func validateTokenBrokerRequestPermissions(data *WorkflowData, permissions *Permissions) error {
	if !tokenBrokerRequestEnabled(data) || permissions == nil {
		return nil
	}
	if level, exists := permissions.GetExplicit(PermissionCopilotRequests); exists && level == PermissionWrite {
		return errTokenBrokerRequestPermissionConflict
	}
	return nil
}

func inheritTokenBrokerConfig(source, destination *WorkflowData) {
	destination.TokenBrokerEnabled = source.TokenBrokerEnabled
	destination.TokenBrokerCopilotRequest = source.TokenBrokerCopilotRequest
	destination.TokenBrokerCopilotTask = source.TokenBrokerCopilotTask
}

func (c *Compiler) buildTokenBrokerRequestStep(data *WorkflowData, condition string) []string {
	if !usesTokenBrokerInference(data) {
		return nil
	}
	var step []string
	step = append(step,
		"      - name: Request brokered Copilot inference credential\n",
		"        id: token_broker_copilot_request\n",
	)
	if condition != "" {
		step = append(step, "        if: "+condition+"\n")
	}
	step = append(step,
		"        uses: "+getCachedActionPin("actions/github-script", data)+"\n",
		"        env:\n",
		"          "+tokenBrokerURLEnv+": "+tokenBrokerURLExpression+"\n",
		"        with:\n",
		"          script: |\n",
		"            const { getBrokerToken } = require('"+SetupActionDestination+"/setup/js/token_broker.cjs');\n",
		"            core.setOutput('token', await getBrokerToken('copilot-request', { mask: token => core.setSecret(token) }, 'github'));\n",
	)
	return step
}

func (c *Compiler) buildTokenBrokerInvalidationStep(data *WorkflowData) []string {
	if !usesTokenBrokerInference(data) && !usesTokenBrokerTasks(data) {
		return nil
	}
	return []string{
		"      - name: Invalidate brokered Copilot credentials\n",
		"        if: always()\n",
		"        uses: " + getCachedActionPin("actions/github-script", data) + "\n",
		"        env:\n",
		"          " + tokenBrokerURLEnv + ": " + tokenBrokerURLExpression + "\n",
		"        with:\n",
		"          script: |\n",
		"            const { invalidateBrokerTokens } = require('" + SetupActionDestination + "/setup/js/token_broker.cjs');\n",
		"            await invalidateBrokerTokens({ mask: token => core.setSecret(token) });\n",
	}
}
