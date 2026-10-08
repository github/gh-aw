package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueDispatchCredentialConfiguration(t *testing.T) {
	globalApp := &GitHubAppConfig{AppID: "${{ vars.PUBLISHER_APP_ID }}", PrivateKey: "${{ secrets.PUBLISHER_APP_KEY }}"}
	dispatchApp := &GitHubAppConfig{AppID: "${{ vars.WORKER_APP_ID }}", PrivateKey: "${{ secrets.WORKER_APP_KEY }}"}
	for _, entry := range []struct {
		name    string
		outputs *SafeOutputsConfig
		token   string
		kind    string
		app     *GitHubAppConfig
	}{
		{"default", &SafeOutputsConfig{}, "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", "${{ secrets.GH_AW_GITHUB_TOKEN != '' && 'authenticated' || 'github_token' }}", nil},
		{"explicit-default", &SafeOutputsConfig{GitHubToken: "${{ github.token }}"}, "${{ github.token }}", "github_token", nil},
		{"global-pat", &SafeOutputsConfig{GitHubToken: "${{ secrets.PUBLISHER_PAT }}"}, "${{ secrets.PUBLISHER_PAT }}", "authenticated", nil},
		{"handler-pat", &SafeOutputsConfig{GitHubToken: "${{ secrets.PUBLISHER_PAT }}", DispatchWorkflow: &DispatchWorkflowConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: "${{ secrets.WORKER_PAT }}"}}}, "${{ secrets.WORKER_PAT }}", "authenticated", nil},
		{"global-app", &SafeOutputsConfig{GitHubApp: globalApp}, "${{ steps.work-queue-dispatch-app-token.outputs.token }}", "github_app", globalApp},
		{"handler-app", &SafeOutputsConfig{GitHubApp: globalApp, DispatchWorkflow: &DispatchWorkflowConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubApp: dispatchApp}}}, "${{ steps.work-queue-dispatch-app-token.outputs.token }}", "github_app", dispatchApp},
		{"handler-pat-over-global-app", &SafeOutputsConfig{GitHubApp: globalApp, DispatchWorkflow: &DispatchWorkflowConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: "${{ secrets.WORKER_PAT }}"}}}, "${{ secrets.WORKER_PAT }}", "authenticated", nil},
	} {
		t.Run(entry.name, func(t *testing.T) {
			config := map[string]any{}
			require.Same(t, entry.app, configureWorkQueueDispatchCredential(&WorkflowData{SafeOutputs: entry.outputs}, config))
			require.Equal(t, entry.token, config["github-token"])
			credential := config["work_queue_dispatch_credential"].(map[string]any)
			require.Equal(t, entry.kind, credential["kind"])
			require.NotContains(t, credential, "principal")
			if entry.app != nil {
				require.Equal(t, "${{ steps.work-queue-dispatch-app-token.outputs.app-slug }}", credential["app_slug"])
			} else {
				require.NotContains(t, credential, "app_slug")
			}
			require.NotContains(t, config, "github.actor_id")
		})
	}
}

func TestWorkQueueDispatchAppMintPrecedesControlsWithExactPermissionsAndPreviewFence(t *testing.T) {
	staged := TemplatableBool("true")
	for _, preview := range []bool{false, true} {
		data := &WorkflowData{
			Tools: map[string]any{"work-queue": map[string]any{"worker": true}},
			SafeOutputs: &SafeOutputsConfig{GitHubApp: &GitHubAppConfig{
				AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}",
				Permissions: map[string]string{"contents": "write", "pull-requests": "write", "actions": "read"},
			}},
		}
		if preview {
			data.SafeOutputs.Staged = &staged
		}
		require.NoError(t, validateWorkQueueConfiguration(data))
		var steps []struct {
			ID   string            `yaml:"id"`
			If   string            `yaml:"if"`
			With map[string]string `yaml:"with"`
			Env  map[string]string `yaml:"env"`
		}
		controlSteps, err := NewCompiler().buildWorkQueueControlProcessingStep(data)
		require.NoError(t, err)
		require.NoError(t, yaml.Unmarshal([]byte(strings.Join(controlSteps, "")), &steps))
		require.Len(t, steps, 2)
		require.Equal(t, workQueueDispatchAppTokenStepID, steps[0].ID)
		require.Equal(t, "write", steps[0].With["permission-actions"])
		require.NotContains(t, steps[0].With, "permission-contents")
		require.NotContains(t, steps[0].With, "permission-pull-requests")
		require.Equal(t, "write", data.SafeOutputs.GitHubApp.Permissions["contents"], "do not mutate publisher App configuration")
		if preview {
			require.Equal(t, "false", steps[0].If)
		} else {
			require.Contains(t, steps[0].If, "GH_AW_SAFE_OUTPUTS_STAGED")
		}
		require.Equal(t, "work_queue_controls", steps[1].ID)
		require.NotEmpty(t, steps[1].With["github-token"], "publisher credential remains explicit and separate")
		require.Contains(t, steps[1].Env["GH_AW_WORK_QUEUE_CONTROL_CONFIG"], "work_queue_dispatch_credential")
		require.NotContains(t, steps[1].Env["GH_AW_WORK_QUEUE_CONTROL_CONFIG"], "github.actor_id")
		require.NotContains(t, steps[1].Env["GH_AW_WORK_QUEUE_CONTROL_CONFIG"], `\u0026`)
	}
}
