package workflow

import "strings"

const workQueueDispatchAppTokenStepID = "work-queue-dispatch-app-token"

func configureWorkQueueDispatchCredential(data *WorkflowData, config map[string]any) *GitHubAppConfig {
	var app *GitHubAppConfig
	var token string
	if data.SafeOutputs != nil {
		if dispatch := data.SafeOutputs.DispatchWorkflow; dispatch != nil {
			app = dispatch.GitHubApp
			token = dispatch.GitHubToken
		}
		if app == nil && token == "" {
			app = data.SafeOutputs.GitHubApp
			token = data.SafeOutputs.GitHubToken
		}
	}
	credential := map[string]any{"kind": "authenticated"}
	if app != nil {
		config["github-token"] = "${{ steps." + workQueueDispatchAppTokenStepID + ".outputs.token }}"
		credential["kind"] = "github_app"
		credential["app_slug"] = "${{ steps." + workQueueDispatchAppTokenStepID + ".outputs.app-slug }}"
	} else {
		config["github-token"] = resolveSafeOutputGitHubToken(token)
		if token == "" {
			credential["kind"] = "${{ secrets.GH_AW_GITHUB_TOKEN != '' && 'authenticated' || 'github_token' }}"
		} else if strings.TrimSpace(token) == "${{ secrets.GITHUB_TOKEN }}" || strings.TrimSpace(token) == "${{ github.token }}" {
			credential["kind"] = "github_token"
		}
	}
	config["work_queue_dispatch_credential"] = credential
	return app
}

func (c *Compiler) buildWorkQueueDispatchAppTokenSteps(data *WorkflowData, app *GitHubAppConfig) []string {
	dispatchApp := *app
	dispatchApp.Permissions = nil
	permissions := NewPermissions()
	permissions.Set(PermissionActions, PermissionWrite)
	lines := c.buildGitHubAppTokenMintStepWithMeta(
		&dispatchApp, permissions, "", inferSingleCheckoutRepositoryForGitHubAppOwner(data),
		"Mint trusted work queue dispatch token", workQueueDispatchAppTokenStepID,
	)
	condition := "env.GH_AW_SAFE_OUTPUTS_STAGED != 'true'"
	var staged []*string
	if data.SafeOutputs != nil {
		staged = append(staged, resolveSafeOutputsStagedValue(c.trialMode, data.SafeOutputs.Staged))
		if data.SafeOutputs.DispatchWorkflow != nil {
			staged = append(staged, resolveSafeOutputsStagedValue(false, data.SafeOutputs.DispatchWorkflow.Staged))
		}
	}
	for _, value := range staged {
		if value == nil {
			continue
		}
		if !isExpression(*value) {
			condition = "false"
			break
		}
		expression := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(*value, "${{"), "}}"))
		condition += " && format('{0}', (" + expression + ")) != 'true'"
	}
	var steps []string
	for _, line := range lines {
		if strings.HasPrefix(line, stepNameLinePrefix) {
			steps = append(steps, line)
		} else {
			steps[len(steps)-1] += line
		}
	}
	return injectStepCondition(steps, &ExpressionNode{Expression: condition})
}
