package workflow

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const tokenBrokerURLEnv = "GH_AW_TOKEN_BROKER_URL"
const tokenBrokerURLExpression = "${{ secrets.GH_AW_TOKEN_BROKER_URL }}"
const tokenBrokerCopilotRequestOutput = "${{ steps.token_broker_copilot_request.outputs.token }}"

var (
	errTokenBrokerInferenceRequiresRenewal  = errors.New("token-broker.copilot-request does not support Copilot inference in the main agent job: AWF requires a renewable credential provider, which is not supported by the current runtime")
	errTokenBrokerRequestPermissionConflict = errors.New("token-broker.copilot-request is incompatible with permissions.copilot-requests: write")
)

func sectionTokenBrokerEnabled(config map[string]any) bool {
	value, present := config["token-broker"]
	return present && (value == nil || value == true)
}

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

func usesTokenBrokerGitHubTool(data *WorkflowData) bool {
	return data != nil && data.ParsedTools != nil && data.ParsedTools.GitHub != nil &&
		data.ParsedTools.GitHub.TokenBroker && data.Tools["github"] != false
}

func usesTokenBrokerSafeOutputs(data *WorkflowData) bool {
	return data != nil && data.SafeOutputs != nil && data.SafeOutputs.TokenBroker
}

func validateTokenBrokerGitHubConfig(data *WorkflowData) error {
	if usesTokenBrokerGitHubTool(data) {
		config := data.ParsedTools.GitHub
		if config.GitHubApp != nil || config.GitHubToken != "" {
			return errors.New("tools.github.token-broker: true is mutually exclusive with github-app and github-token; choose one credential source")
		}
		if !isFirewallEnabled(data) || !awfSupportsExcludeEnv(getFirewallConfig(data)) {
			return fmt.Errorf("tools.github.token-broker: true requires an enabled AWF sandbox at version %s or newer to keep Actions OIDC credentials out of the agent; enable sandbox.agent and update sandbox.agent.version", constants.AWFExcludeEnvMinVersion)
		}
	}
	if usesTokenBrokerSafeOutputs(data) && (data.SafeOutputs.GitHubApp != nil || data.SafeOutputs.GitHubToken != "") {
		return errors.New("safe-outputs.token-broker: true is mutually exclusive with github-app and github-token; choose one credential source")
	}
	return nil
}

// tokenBrokerSelfHostedJobs returns compiled jobs that exchange broker
// credentials on a self-hosted runner. A persistent runner can carry code
// planted by an earlier job, such as the agent job, into a credential job.
func tokenBrokerSelfHostedJobs(workflow map[string]any) []string {
	jobs, _ := workflow["jobs"].(map[string]any)
	var names []string
	for name, raw := range jobs {
		job, ok := raw.(map[string]any)
		if !ok || !usesSelfHostedRunner(job["runs-on"]) || !jobUsesTokenBroker(job) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func jobUsesTokenBroker(job map[string]any) bool {
	if env, ok := job["env"].(map[string]any); ok {
		if _, ok := env[tokenBrokerURLEnv]; ok {
			return true
		}
	}
	steps, _ := job["steps"].([]any)
	for _, raw := range steps {
		step, _ := raw.(map[string]any)
		if env, ok := step["env"].(map[string]any); ok {
			if _, ok := env[tokenBrokerURLEnv]; ok {
				return true
			}
		}
	}
	return false
}

func tokenBrokerSelfHostedWarning(jobs []string) string {
	return fmt.Sprintf("token-broker: jobs [%s] exchange broker credentials on self-hosted runners. Use only ephemeral runners that start clean for each job; a persistent runner lets code left by an earlier job, including the agent job, steal the job's credentials. The broker must also deny unenrolled runners.", strings.Join(jobs, ", "))
}

// The broker derives installation authority from backend policy, never caller permissions.
func (c *Compiler) buildGitHubBrokerTokenStep(data *WorkflowData, stepID string) []string {
	return []string{
		"      - name: Request brokered GitHub App credential\n",
		"        id: " + stepID + "\n",
		"        uses: " + getCachedActionPin("actions/github-script", data) + "\n",
		"        env:\n",
		"          " + tokenBrokerURLEnv + ": " + tokenBrokerURLExpression + "\n",
		"        with:\n",
		"          script: |\n",
		"            const { getBrokerToken } = require('" + SetupActionDestination + "/token_broker.cjs');\n",
		"            core.setOutput('token', await getBrokerToken('github-app', { mask: token => core.setSecret(token) }));\n",
	}
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
		"            const { getBrokerToken } = require('"+SetupActionDestination+"/token_broker.cjs');\n",
		"            core.setOutput('token', await getBrokerToken('copilot-request', { mask: token => core.setSecret(token) }, 'github'));\n",
	)
	return step
}

func (c *Compiler) buildTokenBrokerInvalidationStep(data *WorkflowData) []string {
	if !usesTokenBrokerInference(data) && !usesTokenBrokerTasks(data) {
		return nil
	}
	return c.buildBrokerInvalidationStep(data)
}

func (c *Compiler) buildBrokerInvalidationStep(data *WorkflowData) []string {
	name := "Invalidate brokered Copilot credentials"
	if usesTokenBrokerGitHubTool(data) || usesTokenBrokerSafeOutputs(data) {
		name = "Invalidate brokered credentials"
	}
	return []string{
		"      - name: " + name + "\n",
		"        if: always()\n",
		"        uses: " + getCachedActionPin("actions/github-script", data) + "\n",
		"        env:\n",
		"          " + tokenBrokerURLEnv + ": " + tokenBrokerURLExpression + "\n",
		"        with:\n",
		"          script: |\n",
		"            const { invalidateBrokerTokens } = require('" + SetupActionDestination + "/token_broker.cjs');\n",
		"            await invalidateBrokerTokens({ mask: token => core.setSecret(token) });\n",
	}
}
