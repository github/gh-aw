package workflow

import (
	"fmt"
	"strconv"
)

// RepoDoneConfig configures trusted completion reporting, not an agent-callable tool.
type RepoDoneConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Body                 string `yaml:"body,omitempty"`
	Disabled             bool   `yaml:"-"`
}

func (c *Compiler) parseRepoDoneConfig(outputMap map[string]any) *RepoDoneConfig {
	value, exists := outputMap["repo-done"]
	if !exists {
		return nil
	}
	config := &RepoDoneConfig{}
	if enabled, ok := value.(bool); ok {
		config.Disabled = !enabled
	}
	if fields, ok := value.(map[string]any); ok {
		c.parseBaseSafeOutputConfig(fields, &config.BaseSafeOutputConfig, 0)
		if body, ok := fields["body"].(string); ok {
			config.Body = body
		}
	}
	return config
}

func repoDoneEnabled(outputs *SafeOutputsConfig) bool {
	return outputs != nil && outputs.RepoDone != nil && !outputs.RepoDone.Disabled
}

// Only conclusion receives these permissions. Dispatch/call invocations can
// forward any supported context; native triggers need only their own scopes.
func addRepoDonePermissions(data *WorkflowData, permissions *Permissions) {
	if !repoDoneEnabled(data.SafeOutputs) || data.DryRun || data.TrialMode ||
		isHandlerStaged(templatableBoolIsTrue(data.SafeOutputs.Staged), data.SafeOutputs.RepoDone.Staged) {
		return
	}
	forwarded := data.On == "" || onSectionHasAnyTrigger(data.On, "workflow_dispatch", "repository_dispatch", "workflow_call")
	for _, entry := range []struct {
		scope    PermissionScope
		triggers []string
	}{
		{PermissionIssues, []string{"issues", "issue_comment"}},
		{PermissionPullRequests, []string{"pull_request", "pull_request_target", "pull_request_review", "pull_request_review_comment", "issue_comment"}},
		{PermissionDiscussions, []string{"discussion", "discussion_comment"}},
		{PermissionContents, []string{"push", "commit_comment"}},
	} {
		if forwarded || onSectionHasAnyTrigger(data.On, entry.triggers...) {
			permissions.Set(entry.scope, PermissionWrite)
		}
	}
}

func (c *Compiler) buildRepoDoneStep(data *WorkflowData, mainJobName string) []string {
	if !repoDoneEnabled(data.SafeOutputs) {
		return nil
	}
	config := data.SafeOutputs.RepoDone
	env := []string{
		fmt.Sprintf("          GH_AW_REPO_DONE_BODY: %q\n", config.Body),
		fmt.Sprintf("          GH_AW_REPO_DONE_AGENT_RESULT: ${{ needs.%s.result }}\n", mainJobName),
		"          GH_AW_REPO_DONE_ACTIVATION_RESULT: ${{ needs.activation.result }}\n",
		fmt.Sprintf("          GH_AW_REPO_DONE_DRY_RUN: %q\n", strconv.FormatBool(data.DryRun || c.dryRun)),
	}
	env = append(env, buildTemplatableBoolEnvVar("GH_AW_SAFE_OUTPUTS_STAGED", resolveSafeOutputsStagedValue(c.trialMode, data.SafeOutputs.Staged))...)
	env = append(env, buildTemplatableBoolEnvVar("GH_AW_REPO_DONE_STAGED", templatableBoolPtrToStringPtr(config.Staged))...)
	return c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Report repository workflow completion",
		StepID:        "repo_done",
		MainJobName:   mainJobName,
		ScriptFile:    "repo_done.cjs",
		CustomToken:   config.GitHubToken,
		CustomEnvVars: env,
		StepCondition: fmt.Sprintf("always() && needs.activation.result == 'success' && needs.%s.result != 'skipped' && needs.%s.result != ''", mainJobName, mainJobName),
	})
}
