package workflow

import "fmt"

const steeringIssueAppTokenStepID = "steering-issue-app-token"

func isSteeringIssueEnabled(data *WorkflowData) bool {
	if data == nil || data.SafeOutputs == nil || !data.SafeOutputs.Steer {
		return false
	}
	return !isSteeringIssueStaged(data)
}

func isSteeringIssueStaged(data *WorkflowData) bool {
	return templatableBoolIsTrue(data.SafeOutputs.Staged)
}

func steeringIssueApp(data *WorkflowData) *GitHubAppConfig {
	if !isSteeringIssueEnabled(data) {
		return nil
	}
	return data.SafeOutputs.GitHubApp
}

func steeringIssueFallbackToken(data *WorkflowData) string {
	return resolveSafeOutputGitHubToken(data.SafeOutputs.GitHubToken)
}

func (c *Compiler) buildSteeringIssueTokenSteps(data *WorkflowData, app *GitHubAppConfig, permissions *Permissions, stepName string, stepID string) ([]string, string) {
	token := steeringIssueFallbackToken(data)
	if app == nil {
		return nil, token
	}

	var steps []string
	if stepName != "" {
		steps = c.buildGitHubAppTokenMintStepWithMeta(app, permissions, "", "", stepName, stepID)
	}
	appToken := fmt.Sprintf("${{ steps.%s.outputs.token }}", stepID)
	if app.shouldIgnoreMissingKey() {
		return steps, combineTokenExpressions(appToken, token)
	}
	return steps, appToken
}

func (c *Compiler) addActivationSteeringIssueStep(ctx *activationJobBuildContext) {
	if !isSteeringIssueEnabled(ctx.data) {
		return
	}

	permissions := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite})
	tokenSteps, token := c.buildSteeringIssueTokenSteps(
		ctx.data,
		steeringIssueApp(ctx.data),
		permissions,
		"Generate GitHub App token (create steering issue)",
		steeringIssueAppTokenStepID,
	)
	ctx.steps = append(ctx.steps, tokenSteps...)

	ctx.steps = append(ctx.steps,
		"      - name: Create steering issue\n",
		"        id: create-steering-issue\n",
		fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", ctx.data)),
		"        env:\n",
		fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", ctx.data.Name),
		"        with:\n",
		fmt.Sprintf("          github-token: %s\n", token),
		"          script: |\n",
		generateGitHubScriptWithRequire("create_steering_issue.cjs"),
	)
	ctx.outputs["steering_issue_number"] = "${{ steps.create-steering-issue.outputs.issue_number }}"
	ctx.outputs["steering_issue_url"] = "${{ steps.create-steering-issue.outputs.issue_url }}"
}

func (c *Compiler) buildConclusionSteeringIssueTokenSteps(data *WorkflowData) ([]string, string) {
	if !isSteeringIssueEnabled(data) {
		return nil, ""
	}

	app := steeringIssueApp(data)
	stepID := "safe-outputs-app-token"
	stepName := ""
	return c.buildSteeringIssueTokenSteps(data, app, NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionIssues: PermissionWrite}), stepName, stepID)
}

func (c *Compiler) buildConclusionSteeringIssueStep(data *WorkflowData, mainJobName, token string) []string {
	if !isSteeringIssueEnabled(data) {
		return nil
	}

	return c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Complete steering issue",
		StepID:        "complete_steering_issue",
		MainJobName:   mainJobName,
		CustomToken:   token,
		Script:        "const { main } = require('${{ runner.temp }}/gh-aw/actions/complete_steering_issue.cjs'); await main();",
		ScriptFile:    "complete_steering_issue.cjs",
		StepCondition: "always()",
		CustomEnvVars: []string{
			"          GH_AW_STEERING_ISSUE_NUMBER: ${{ needs.activation.outputs.steering_issue_number }}\n",
			"          GH_AW_FAILURE_ISSUE_NUMBER: ${{ steps.handle_agent_failure.outputs.failure_issue_number }}\n",
			"          GH_AW_CREATED_PR_NUMBER: ${{ needs.safe_outputs.outputs.created_pr_number }}\n",
			"          GH_AW_CREATED_PR_URL: ${{ needs.safe_outputs.outputs.created_pr_url }}\n",
			"          GH_AW_NEEDS: ${{ toJSON(needs) }}\n",
		},
	})
}
