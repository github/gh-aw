package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestBrokerProjectCredentialsStayPerHandler(t *testing.T) {
	const explicit = "${{ secrets.PROJECT_OVERRIDE }}"
	cfg := &SafeOutputsConfig{
		TokenBroker:                true,
		UpdateProjects:             &UpdateProjectConfig{Project: "https://github.com/orgs/example/projects/1", BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: explicit}},
		CreateProjects:             &CreateProjectsConfig{},
		CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{},
	}
	data := &WorkflowData{SafeOutputs: cfg}
	var env []string
	NewCompiler().addSafeOutputTokenEnvVars(&env, data)
	require.Contains(t, strings.Join(env, ""), "GH_AW_PROJECT_URL:")
	require.NotContains(t, strings.Join(env, ""), "GH_AW_PROJECT_GITHUB_TOKEN:")
	require.Equal(t, explicit, projectHandlerRegistry["update_project"](cfg)["github-token"])
	require.NotContains(t, projectHandlerRegistry["create_project"](cfg), "github-token")
	require.NotContains(t, projectHandlerRegistry["create_project_status_update"](cfg), "github-token")
	cfg.CreateProjects.GitHubApp = &GitHubAppConfig{}
	require.Equal(t, "${{ steps.create-project-app-token.outputs.token }}", projectHandlerRegistry["create_project"](cfg)["github-token"])

	cfg.TokenBroker = false
	cfg.UpdateProjects.GitHubToken = ""
	env = nil
	NewCompiler().addSafeOutputTokenEnvVars(&env, data)
	require.Contains(t, strings.Join(env, ""), "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}")
}

func TestAssetGitCredentialSelection(t *testing.T) {
	DefaultScriptRegistry.mu.Lock()
	previous, existed := DefaultScriptRegistry.scripts["upload_assets"]
	DefaultScriptRegistry.mu.Unlock()
	t.Cleanup(func() {
		DefaultScriptRegistry.mu.Lock()
		defer DefaultScriptRegistry.mu.Unlock()
		if existed {
			DefaultScriptRegistry.scripts["upload_assets"] = previous
		} else {
			delete(DefaultScriptRegistry.scripts, "upload_assets")
		}
	})
	tests := []struct {
		name       string
		broker     bool
		token      string
		app        *GitHubAppConfig
		globalApp  *GitHubAppConfig
		want       string
		wantBroker bool
	}{
		{name: "broker", broker: true, want: "${{ steps.safe-outputs-app-token.outputs.token }}", wantBroker: true},
		{name: "handler token", broker: true, token: "${{ secrets.ASSET_TOKEN }}", want: "${{ secrets.ASSET_TOKEN }}"},
		{name: "handler app", broker: true, app: &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}, want: "${{ steps.safe-outputs-app-token.outputs.token }}"},
		{name: "optional handler app", app: &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}", IgnoreIfMissing: true}, want: combineTokenExpressions("${{ steps.safe-outputs-app-token.outputs.token }}", resolveSafeOutputGitHubToken(""))},
		{name: "global app", globalApp: &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}, want: "${{ steps.safe-outputs-app-token.outputs.token }}"},
		{name: "handler token overrides global app", globalApp: &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}, token: "${{ secrets.ASSET_TOKEN }}", want: "${{ secrets.ASSET_TOKEN }}"},
		{name: "default", want: resolveSafeOutputGitHubToken("")},
	}
	for _, mode := range []ActionMode{ActionModeScript, ActionModeDev} {
		DefaultScriptRegistry.mu.Lock()
		if mode == ActionModeScript {
			delete(DefaultScriptRegistry.scripts, "upload_assets")
		} else {
			DefaultScriptRegistry.scripts["upload_assets"] = &scriptEntry{actionPath: "./actions/upload-assets"}
		}
		DefaultScriptRegistry.mu.Unlock()
		for _, tt := range tests {
			t.Run(string(mode)+"/"+tt.name, func(t *testing.T) {
				data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{
					TokenBroker: tt.broker,
					GitHubApp:   tt.globalApp,
					UploadAssets: &UploadAssetsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{
						GitHubToken: tt.token, GitHubApp: tt.app,
					}},
				}}
				compiler := NewCompiler()
				compiler.SetActionMode(mode)
				job, err := compiler.buildUploadAssetsJob(data, "agent", false)
				require.NoError(t, err)
				steps := strings.Join(job.Steps, "")
				require.Contains(t, steps, "persist-credentials: false")
				require.Contains(t, steps, "GH_AW_ASSET_GIT_TOKEN: "+tt.want)
				if mode == ActionModeScript {
					require.Contains(t, steps, "github-token: "+tt.want)
				} else {
					require.Contains(t, steps, "uses: ./actions/upload-assets")
					require.Contains(t, steps, "token: "+tt.want)
				}
				require.Contains(t, steps, "GITHUB_TOKEN: ''")
				if tt.wantBroker {
					require.Less(t, strings.Index(steps, "Request brokered GitHub App credential"), strings.Index(steps, "GH_AW_ASSET_GIT_TOKEN:"))
				} else {
					require.NotContains(t, steps, "Request brokered GitHub App credential")
				}

				var parsed struct {
					Steps []struct {
						Name string            `yaml:"name"`
						Env  map[string]string `yaml:"env"`
					} `yaml:"steps"`
				}
				require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+steps), &parsed))
				var env map[string]string
				for _, step := range parsed.Steps {
					if step.Name == "Push assets" {
						env = step.Env
					}
				}
				require.NotNil(t, env)
				env["GH_AW_ASSET_GIT_TOKEN"] = "test-token"
				env["GITHUB_SERVER_URL"] = "https://github.com"
				env["GIT_CONFIG_KEY_3"] = "http.https://github.com/.extraheader"
				env["GIT_CONFIG_KEY_5"] = "http.https://github.com/example/repo.extraheader"
				env["GIT_CONFIG_KEY_6"] = "http.https://github.com/example/repo.git.extraheader"
				home := t.TempDir()
				fallbackConfig := "[credential]\n\thelper = \"!f() { echo username=fallback; echo " + "password" + "=fallback; }; f\"\n"
				fallbackConfig += "[http \"https://github.com/\"]\n\textraheader = AUTHORIZATION: stale\n"
				fallbackConfig += "[http \"https://github.com/example/repo.git\"]\n\textraheader = AUTHORIZATION: stale-specific\n"
				require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(fallbackConfig), 0600))
				cmd := exec.Command("git", "credential", "fill")
				cmd.Env = append(os.Environ(), "HOME="+home)
				for k, v := range env {
					cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
				}
				cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
				out, err := cmd.Output()
				require.NoError(t, err)
				require.Contains(t, string(out), "username=x-access-token\npassword=test-token\n")

				cmd = exec.Command("git", "config", "--get-urlmatch", "http.extraheader", "https://github.com/example/repo.git")
				cmd.Env = append(os.Environ(), "HOME="+home)
				for k, v := range env {
					cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
				}
				out, err = cmd.Output()
				require.NoError(t, err)
				require.NotContains(t, string(out), "stale")

				cmd = exec.Command("git", "credential", "fill")
				cmd.Env = append(os.Environ(), "HOME="+home)
				for k, v := range env {
					cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
				}
				cmd.Stdin = strings.NewReader("protocol=https\nhost=evil.example\n\n")
				out, err = cmd.Output()
				require.Error(t, err)
				require.NotContains(t, string(out), "test-token")
				require.NotContains(t, string(out), "fallback")

				// A missing selected token must not invoke a configured fallback helper.
				env["GH_AW_ASSET_GIT_TOKEN"] = ""
				cmd = exec.Command("git", "credential", "fill")
				cmd.Env = append(os.Environ(), "HOME="+home)
				for k, v := range env {
					cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
				}
				cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
				out, _ = cmd.Output()
				require.NotContains(t, string(out), "fallback")
			})
		}
	}
}
