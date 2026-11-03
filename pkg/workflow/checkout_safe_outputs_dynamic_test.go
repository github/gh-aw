//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputDynamicCheckoutTokenPriority(t *testing.T) {
	const globalAppToken = "${{ steps.safe-outputs-app-token.outputs.token }}"
	const defaultToken = "${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
	const readToken = "${{ secrets.READ_TOKEN }}"
	const writeToken = "${{ steps.checkout-safe-output-app-token-0.outputs.token }}"
	app := &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
	optionalApp := &GitHubAppConfig{AppID: app.AppID, PrivateKey: app.PrivateKey, IgnoreIfMissing: true}
	tests := []struct {
		name        string
		entry       CheckoutConfig
		globalApp   *GitHubAppConfig
		globalToken string
		createToken string
		pushToken   string
		staged      bool
		want        string
		agentWant   string
	}{
		{name: "global app", globalApp: app, want: globalAppToken, agentWant: defaultToken},
		{name: "global app overrides global PAT", globalApp: app, globalToken: "${{ secrets.GLOBAL_PAT }}", want: globalAppToken, agentWant: defaultToken},
		{name: "optional global app falls back to PAT", globalApp: optionalApp, globalToken: "${{ secrets.GLOBAL_PAT }}", want: "${{ steps.safe-outputs-app-token.outputs.token || secrets.GLOBAL_PAT }}", agentWant: defaultToken},
		{name: "optional global app falls back to defaults", globalApp: optionalApp, want: "${{ steps.safe-outputs-app-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", agentWant: defaultToken},
		{name: "create PAT overrides global app", globalApp: app, createToken: "${{ secrets.CREATE_PAT }}", pushToken: "${{ secrets.PUSH_PAT }}", want: "${{ secrets.CREATE_PAT }}", agentWant: defaultToken},
		{name: "push PAT overrides global app", globalApp: app, pushToken: "${{ secrets.PUSH_PAT }}", want: "${{ secrets.PUSH_PAT }}", agentWant: defaultToken},
		{name: "global PAT", globalToken: "${{ secrets.GLOBAL_PAT }}", want: "${{ secrets.GLOBAL_PAT }}", agentWant: defaultToken},
		{name: "default safe-output fallback", want: "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", agentWant: defaultToken},
		{name: "entry PAT overrides global app", entry: CheckoutConfig{GitHubToken: readToken}, globalApp: app, want: readToken, agentWant: readToken},
		{name: "entry read app overrides global app", entry: CheckoutConfig{GitHubApp: app}, globalApp: app, want: "${{ steps.checkout-app-token-0.outputs.token }}", agentWant: "${{ steps.checkout-app-token-0.outputs.token }}"},
		{name: "optional entry read app preserves default fallback", entry: CheckoutConfig{GitHubApp: optionalApp}, globalApp: app, want: "${{ steps.checkout-app-token-0.outputs.token || secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", agentWant: "${{ steps.checkout-app-token-0.outputs.token || secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"},
		{name: "entry write app overrides read PAT", entry: CheckoutConfig{GitHubToken: readToken, SafeOutputGitHubApp: app}, globalApp: app, want: writeToken, agentWant: readToken},
		{name: "optional write app falls back to read PAT", entry: CheckoutConfig{GitHubToken: readToken, SafeOutputGitHubApp: optionalApp}, globalApp: app, want: "${{ steps.checkout-safe-output-app-token-0.outputs.token || secrets.READ_TOKEN }}", agentWant: readToken},
		{name: "optional write app falls back to read app", entry: CheckoutConfig{GitHubApp: app, SafeOutputGitHubApp: optionalApp}, globalApp: app, want: "${{ steps.checkout-safe-output-app-token-0.outputs.token || steps.checkout-app-token-0.outputs.token }}", agentWant: "${{ steps.checkout-app-token-0.outputs.token }}"},
		{name: "optional write app falls back to global app", entry: CheckoutConfig{SafeOutputGitHubApp: optionalApp}, globalApp: app, want: "${{ steps.checkout-safe-output-app-token-0.outputs.token || steps.safe-outputs-app-token.outputs.token }}", agentWant: defaultToken},
		{name: "optional write app falls back to safe-output defaults", entry: CheckoutConfig{SafeOutputGitHubApp: optionalApp}, want: "${{ steps.checkout-safe-output-app-token-0.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", agentWant: defaultToken},
		{name: "current optional write app retains global fallback", entry: CheckoutConfig{SafeOutputGitHubApp: optionalApp, Current: true}, globalApp: app, want: "${{ steps.checkout-safe-output-app-token-0.outputs.token || steps.safe-outputs-app-token.outputs.token }}", agentWant: defaultToken},
		{name: "staged global app fallback", globalApp: app, staged: true, want: "${{ steps.safe-outputs-app-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", agentWant: defaultToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := tt.entry
			entry.Multi = true
			entry.Repository = "${{ inputs.repositories }}"
			entry.Path = "one/two/three/four/five/six/targets"
			entry.AllowedRepos = []string{"org/*"}
			safeOutputs := &SafeOutputsConfig{
				GitHubApp: tt.globalApp, GitHubToken: tt.globalToken,
				CreatePullRequests:      &CreatePullRequestsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: tt.createToken}},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: tt.pushToken}},
			}
			if tt.staged {
				staged := TemplatableBool("true")
				safeOutputs.Staged = &staged
			}
			manager, _ := prepareSharedPRCheckout(&WorkflowData{CheckoutConfigs: []*CheckoutConfig{&entry}, SafeOutputs: safeOutputs})
			pin := func(action string) string { return action + "@test" }
			safeSteps := strings.Join(manager.GenerateAdditionalCheckoutSteps(pin), "")
			require.Equal(t, 3, strings.Count(safeSteps, "GH_AW_DYNAMIC_TOKEN: "+tt.want+"\n"), "prepare, checkout and finalize must use identical auth")
			require.Contains(t, safeSteps, `GH_AW_DYNAMIC_PERSIST: "true"`)
			require.Contains(t, safeSteps, "await finish({ cleanCredentials: false });")

			agentSteps := strings.Join(NewCheckoutManager([]*CheckoutConfig{&entry}).GenerateAdditionalCheckoutSteps(pin), "")
			require.Equal(t, 3, strings.Count(agentSteps, "GH_AW_DYNAMIC_TOKEN: "+tt.agentWant+"\n"))
			require.Contains(t, agentSteps, `GH_AW_DYNAMIC_PERSIST: "false"`)
			require.Contains(t, agentSteps, "await finish({ cleanCredentials: true });")
			require.NotContains(t, agentSteps, "steps.safe-outputs-app-token")
			require.NotContains(t, agentSteps, "steps.checkout-safe-output-app-token")
		})
	}
}

func TestSafeOutputDynamicCheckoutKeepsHostingAndStaticAuth(t *testing.T) {
	configs := []*CheckoutConfig{
		{GitHubToken: "${{ secrets.HOST_TOKEN }}"},
		{Repository: "org/static", Path: "static"},
		{Repository: "${{ inputs.repositories }}", Path: "targets", Multi: true, AllowedRepos: []string{"org/*"}},
	}
	data := &WorkflowData{
		CheckoutConfigs: configs,
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp:          &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"},
			CreatePullRequests: &CreatePullRequestsConfig{},
		},
	}
	steps := strings.Join(NewCompiler().buildSharedPRCheckoutSteps(data), "")
	require.Contains(t, steps, "token: ${{ secrets.HOST_TOKEN }}")
	require.Equal(t, 2, strings.Count(steps, "token: ${{ secrets.HOST_TOKEN }}"), "hosting and static checkouts keep existing token priority")
	require.Equal(t, 3, strings.Count(steps, "GH_AW_DYNAMIC_TOKEN: ${{ steps.safe-outputs-app-token.outputs.token }}"))
}

func TestSafeOutputDynamicCheckoutDoesNotInheritAnotherEntryApp(t *testing.T) {
	app := &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
	data := &WorkflowData{
		CheckoutConfigs: []*CheckoutConfig{
			{Repository: "org/current", Path: "current", Current: true, SafeOutputGitHubApp: app},
			{Repository: "${{ inputs.repositories }}", Path: "targets", Multi: true, AllowedRepos: []string{"org/*"}},
		},
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp:          app,
			CreatePullRequests: &CreatePullRequestsConfig{},
		},
	}
	manager, prToken := prepareSharedPRCheckout(data)
	require.Equal(t, "${{ steps.checkout-safe-output-app-token-0.outputs.token }}", prToken, "static credentials still select the current checkout app")
	pin := func(action string) string { return action + "@test" }
	steps := strings.Join(manager.GenerateAdditionalCheckoutSteps(pin), "")
	require.Equal(t, 3, strings.Count(steps, "GH_AW_DYNAMIC_TOKEN: ${{ steps.safe-outputs-app-token.outputs.token }}"))
	require.NotContains(t, steps, "GH_AW_DYNAMIC_TOKEN: ${{ steps.checkout-safe-output-app-token-0.outputs.token }}", "entry-scoped write credentials must not leak to other runtime repositories")
}

func TestCompileSafeOutputDynamicCheckoutManifestAndAuth(t *testing.T) {
	for _, operation := range []string{"create-pull-request", "push-to-pull-request-branch"} {
		for _, staged := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/staged=%t", operation, staged), func(t *testing.T) {
				dir := t.TempDir()
				workflowPath := filepath.Join(dir, "checkout.md")
				content := fmt.Sprintf(`---
on:
  workflow_dispatch:
    inputs:
      repositories:
        type: string
        required: true
engine: copilot
checkout:
  - multi: true
    repository: ${{ inputs.repositories }}
    path: one/two/three/four/five/six/targets
    allowed-repos: ["org/*"]
safe-outputs:
  staged: %t
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_KEY }}
  %s:
---
Do nothing.
`, staged, operation)
				require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0o644))
				require.NoError(t, NewCompiler().CompileWorkflow(workflowPath))
				lock, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
				require.NoError(t, err)
				var workflow struct {
					Jobs map[string]struct {
						Steps []struct {
							Name string            `yaml:"name"`
							ID   string            `yaml:"id"`
							If   string            `yaml:"if"`
							Env  map[string]string `yaml:"env"`
						} `yaml:"steps"`
					} `yaml:"jobs"`
				}
				require.NoError(t, yaml.Unmarshal(lock, &workflow))
				for _, jobName := range []string{"agent", "safe_outputs"} {
					job, ok := workflow.Jobs[jobName]
					require.True(t, ok, "missing job %s", jobName)
					finalize, manifest, handler, appToken := -1, -1, -1, -1
					var checkoutCondition string
					for i, step := range job.Steps {
						switch step.Name {
						case "Finalize runtime checkouts (0)":
							finalize = i
							checkoutCondition = step.If
							require.Equal(t, "one/two/three/four/five/six/targets", step.Env["GH_AW_DYNAMIC_PREFIX"])
							if jobName == "safe_outputs" {
								require.Equal(t, "${{ steps.safe-outputs-app-token.outputs.token }}", step.Env["GH_AW_DYNAMIC_TOKEN"])
								require.Equal(t, "true", step.Env["GH_AW_DYNAMIC_PERSIST"])
							} else {
								require.Equal(t, "false", step.Env["GH_AW_DYNAMIC_PERSIST"])
								require.NotContains(t, step.Env["GH_AW_DYNAMIC_TOKEN"], "safe-outputs-app-token")
							}
						case "Build checkout manifest for safe-outputs handlers":
							manifest = i
							require.Equal(t, "0", step.Env["GH_AW_DYNAMIC_CHECKOUT_INDEXES"])
							require.Equal(t, checkoutCondition, step.If, "manifest must use the checkout gate")
						case "Process Safe Outputs":
							handler = i
						}
						if step.ID == "safe-outputs-app-token" {
							appToken = i
						}
					}
					if jobName == "safe_outputs" && staged {
						require.Equal(t, -1, finalize)
						require.Equal(t, -1, manifest)
						require.Equal(t, -1, appToken)
						continue
					}
					require.GreaterOrEqual(t, finalize, 0)
					require.Greater(t, manifest, finalize, "manifest must consume finalized runtime checkout records")
					if jobName == "safe_outputs" {
						require.GreaterOrEqual(t, appToken, 0)
						require.Less(t, appToken, finalize, "same-job global app token must be minted before runtime checkout")
						require.Greater(t, handler, manifest, "handler must see the manifest before repository lookup")
						require.NotEmpty(t, checkoutCondition)
					}
				}
			})
		}
	}
}
