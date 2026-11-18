//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type githubBrokerTestStep struct {
	ID   string            `yaml:"id"`
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}

type githubBrokerTestJob struct {
	Permissions map[string]string      `yaml:"permissions"`
	Outputs     map[string]string      `yaml:"outputs"`
	Steps       []githubBrokerTestStep `yaml:"steps"`
}

func compileGitHubBrokerTest(t *testing.T, fields string) (*WorkflowData, string, map[string]githubBrokerTestJob) {
	t.Helper()
	compiler := NewCompiler()
	source := "---\non: workflow_dispatch\nstrict: false\nengine: claude\n" + fields + "---\nTest broker."
	data, err := compiler.ParseWorkflowString(source, "github-broker.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "github-broker.md")
	require.NoError(t, err)
	var compiled struct {
		Jobs map[string]githubBrokerTestJob `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
	return data, output, compiled.Jobs
}

func assertGitHubBrokerConsumer(t *testing.T, job githubBrokerTestJob, stepID string) {
	t.Helper()
	assert.Equal(t, "write", job.Permissions["id-token"])
	exchanges, invalidations := 0, 0
	for _, step := range job.Steps {
		if step.ID == stepID {
			exchanges++
			assert.Equal(t, tokenBrokerURLExpression, step.Env[tokenBrokerURLEnv])
			assert.Contains(t, step.With["script"], "getBrokerToken('github-app', { mask: token => core.setSecret(token) })")
			assert.NotContains(t, step.With["script"], "provider")
			assert.NotContains(t, step.With["script"], "scope")
			assert.NotContains(t, step.With["script"], "catch")
		}
		if strings.Contains(step.With["script"], "await invalidateBrokerTokens(") {
			invalidations++
			assert.Equal(t, "always()", step.If)
		}
	}
	assert.Equal(t, 1, exchanges)
	assert.Equal(t, 1, invalidations)
	for _, value := range job.Outputs {
		assert.NotContains(t, value, ".outputs.token")
	}
}

func TestTokenBrokerGitHubCompileConsumers(t *testing.T) {
	_, _, baselineJobs := compileGitHubBrokerTest(t, "safe-outputs:\n  threat-detection: false\n  create-issue:\n")
	for _, transport := range []string{"local", "remote", "gh-proxy"} {
		t.Run(transport, func(t *testing.T) {
			mode := "type: " + transport
			if transport == "gh-proxy" {
				mode = "mode: gh-proxy"
			}
			data, output, jobs := compileGitHubBrokerTest(t, "tools:\n  github:\n    "+mode+"\n    token-broker: true\nsafe-outputs:\n  token-broker: true\n  threat-detection: false\n  create-issue:\n")
			assert.True(t, data.TokenBrokerEnabled)
			assert.True(t, data.ParsedTools.GitHub.TokenBroker)
			assert.True(t, data.SafeOutputs.TokenBroker)
			assertGitHubBrokerConsumer(t, jobs["agent"], "github-mcp-app-token")
			assertGitHubBrokerConsumer(t, jobs["safe_outputs"], "safe-outputs-app-token")
			assertGitHubBrokerConsumer(t, jobs["conclusion"], "safe-outputs-app-token")
			assert.NotEqual(t, "write", jobs["safe_outputs"].Permissions["issues"])
			assert.Equal(t, baselineJobs["activation"].Permissions, jobs["activation"].Permissions)
			assert.Contains(t, output, "${{ steps.github-mcp-app-token.outputs.token }}")
			assert.Contains(t, output, "github-token: ${{ steps.safe-outputs-app-token.outputs.token }}")
			assert.NotContains(t, output, "steps.github-mcp-app-token.outputs.token ||")
			assert.NotContains(t, output, "steps.safe-outputs-app-token.outputs.token ||")
			exclusions := ComputeAWFExcludeEnvVarNames(data, nil)
			assert.Contains(t, exclusions, tokenBrokerURLEnv)
			assert.Contains(t, exclusions, "ACTIONS_ID_TOKEN_REQUEST_URL")
			assert.Contains(t, exclusions, "ACTIONS_ID_TOKEN_REQUEST_TOKEN")
		})
	}
}

func TestTokenBrokerGitHubOptInIsolation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fields      string
		agentBroker bool
		safeBroker  bool
	}{
		{"absent", "", false, false},
		{"false", "tools:\n  github:\n    token-broker: false\n", false, false},
		{"tools-only", "tools:\n  github:\n    token-broker: true\n", true, false},
		{"safe-only", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := tc.fields + "safe-outputs:\n  threat-detection: false\n  token-broker: " + map[bool]string{true: "true", false: "false"}[tc.safeBroker] + "\n  create-issue:\n"
			data, output, jobs := compileGitHubBrokerTest(t, fields)
			assert.Equal(t, tc.agentBroker || tc.safeBroker, data.TokenBrokerEnabled)
			if tc.agentBroker {
				assertGitHubBrokerConsumer(t, jobs["agent"], "github-mcp-app-token")
			} else {
				assert.NotContains(t, jobs["agent"].Permissions, "id-token")
			}
			if tc.safeBroker {
				assertGitHubBrokerConsumer(t, jobs["safe_outputs"], "safe-outputs-app-token")
			} else {
				assert.NotContains(t, jobs["safe_outputs"].Permissions, "id-token")
			}
			if !tc.agentBroker && !tc.safeBroker {
				assert.NotContains(t, output, "getBrokerToken('github-app'")
				assert.NotContains(t, output, "Invalidate brokered credentials")
			}
		})
	}
}

func TestTokenBrokerGitHubCredentialConflicts(t *testing.T) {
	for _, path := range []string{"tools", "safe-outputs"} {
		for _, other := range []string{"github-token: ${{ secrets.CUSTOM_TOKEN }}", "github-app:\n      client-id: ${{ vars.APP_ID }}\n      private-key: ${{ secrets.APP_KEY }}"} {
			t.Run(path+"/"+strings.Split(other, ":")[0], func(t *testing.T) {
				var fields string
				if path == "tools" {
					fields = "tools:\n  github:\n    token-broker: true\n    " + other + "\nsafe-outputs:\n  threat-detection: false\n"
				} else {
					other = strings.ReplaceAll(other, "\n      ", "\n    ")
					fields = "safe-outputs:\n  threat-detection: false\n  token-broker: true\n  " + other + "\n"
				}
				compiler := NewCompiler()
				data, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nstrict: false\nengine: claude\n"+fields+"---\nTest.", "broker.md")
				if err == nil {
					_, err = compiler.CompileToYAML(data, "broker.md")
				}
				require.ErrorContains(t, err, "mutually exclusive")
			})
		}
	}
}

func TestTokenBrokerGitHubTopLevelAppFallback(t *testing.T) {
	data, output, _ := compileGitHubBrokerTest(t, "github-app:\n  client-id: ${{ vars.APP_ID }}\n  private-key: ${{ secrets.APP_KEY }}\ntools:\n  github:\n    token-broker: true\nsafe-outputs:\n  token-broker: true\n  threat-detection: false\n  create-issue:\n")
	assert.Nil(t, data.ParsedTools.GitHub.GitHubApp)
	assert.Nil(t, data.SafeOutputs.GitHubApp)
	assert.NotContains(t, output, "id: github-mcp-app-token\n        uses: actions/create-github-app-token")
	assert.NotContains(t, output, "id: safe-outputs-app-token\n        uses: actions/create-github-app-token")
}

func TestTokenBrokerGitHubCheckoutAndOverrides(t *testing.T) {
	data, output, jobs := compileGitHubBrokerTest(t, "safe-outputs:\n  token-broker: true\n  threat-detection: false\n  create-pull-request:\n  update-issue:\n    github-token: ${{ secrets.ISSUE_TOKEN }}\n")
	assertGitHubBrokerConsumer(t, jobs["safe_outputs"], "safe-outputs-app-token")
	assert.Contains(t, output, "token: ${{ steps.safe-outputs-app-token.outputs.token }}")
	assert.Equal(t, "${{ secrets.ISSUE_TOKEN }}", handlerRegistry["update_issue"](data.SafeOutputs)["github-token"])
	token, custom := resolvePRCheckoutToken(data.SafeOutputs, nil)
	assert.True(t, custom)
	assert.Equal(t, "${{ steps.safe-outputs-app-token.outputs.token }}", token)
	data.SafeOutputs.CreatePullRequests.GitHubToken = "${{ secrets.PR_TOKEN }}"
	token, custom = resolvePRCheckoutToken(data.SafeOutputs, nil)
	assert.True(t, custom)
	assert.Equal(t, "${{ secrets.PR_TOKEN }}", token)
	assert.Equal(t, "${{ secrets.PR_TOKEN }}", resolveSafeOutputGitHubTokenForConfig(data, "${{ secrets.PR_TOKEN }}", resolveSafeOutputGitHubToken, true))
	assert.NotContains(t, resolveSafeOutputGitHubTokenForConfig(data, "", getEffectiveCopilotCodingAgentGitHubToken, false), "safe-outputs-app-token")
}

func TestTokenBrokerGitHubDerivedJobsAndCustomJobs(t *testing.T) {
	_, output, jobs := compileGitHubBrokerTest(t, "tools:\n  github:\n    token-broker: true\nsafe-outputs:\n  token-broker: true\n  create-issue:\n  jobs:\n    custom:\n      description: Custom test\n      runs-on: ubuntu-latest\n      steps:\n        - run: echo custom\n")
	assertGitHubBrokerConsumer(t, jobs["agent"], "github-mcp-app-token")
	assertGitHubBrokerConsumer(t, jobs["safe_outputs"], "safe-outputs-app-token")
	require.Contains(t, jobs, "detection")
	assert.NotContains(t, jobs["detection"].Permissions, "id-token")
	require.Contains(t, jobs, "custom")
	assert.NotContains(t, jobs["custom"].Permissions, "id-token")
	for _, name := range []string{"detection", "custom", "activation"} {
		for _, step := range jobs[name].Steps {
			assert.NotContains(t, step.With["script"], "getBrokerToken('github-app'")
			for _, value := range step.Env {
				assert.NotContains(t, value, "steps.github-mcp-app-token")
				assert.NotContains(t, value, "steps.safe-outputs-app-token")
			}
		}
	}
	assert.NotContains(t, output, "'github-app', { mask: token => core.setSecret(token) },")
}

func TestTokenBrokerGitHubStandaloneCoverage(t *testing.T) {
	_, _, jobs := compileGitHubBrokerTest(t, "safe-outputs:\n  token-broker: true\n  threat-detection: false\n  upload-code-coverage:\n")
	assertGitHubBrokerConsumer(t, jobs["upload_code_coverage"], "upload-code-coverage-app-token")
	assert.NotEqual(t, "write", jobs["upload_code_coverage"].Permissions["code-quality"])
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{TokenBroker: true, UploadCodeCoverage: &UploadCodeCoverageConfig{
		BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: "${{ secrets.COVERAGE_TOKEN }}"},
	}}}
	assert.False(t, usesTokenBrokerCodeCoverage(data))
}

func TestTokenBrokerGitHubSchemaAndRoundTrip(t *testing.T) {
	for _, value := range []any{"true", map[string]any{"provider": "github"}, nil} {
		assert.Error(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": "workflow_dispatch", "tools": map[string]any{"github": map[string]any{"token-broker": value}}}, "broker.md"))
		assert.Error(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{"on": "workflow_dispatch", "safe-outputs": map[string]any{"token-broker": value}}, "broker.md"))
	}
	var safeOutputs SafeOutputsConfig
	require.NoError(t, json.Unmarshal([]byte(`{"token-broker":true}`), &safeOutputs))
	assert.True(t, safeOutputs.TokenBroker)
	encoded, err := json.Marshal(safeOutputs)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"token-broker":true`)
	keys, err := parser.GetSafeOutputTypeKeys()
	require.NoError(t, err)
	assert.NotContains(t, keys, "token-broker")
}

func TestTokenBrokerGitHubImport(t *testing.T) {
	compiler := NewCompiler()
	for _, tc := range []struct {
		name     string
		explicit map[string]any
		want     bool
	}{
		{"inherited", nil, true},
		{"explicit-false", map[string]any{"token-broker": false}, false},
		{"explicit-true", map[string]any{"token-broker": true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := compiler.extractSafeOutputsConfig(map[string]any{"safe-outputs": tc.explicit})
			result, err := compiler.MergeSafeOutputs(base, []string{`{"token-broker":true,"create-issue":{}}`}, tc.explicit)
			require.NoError(t, err)
			assert.Equal(t, tc.want, result.TokenBroker)
			assert.NotNil(t, result.CreateIssues)
		})
	}
}

func TestTokenBrokerGitHubStrictCopilotAndModelRouting(t *testing.T) {
	baseline := compileDefaultRoutingSource(t, routingRuntimeFixture("none", "sandbox:\n  agent:\n    runtime: cloud-hypervisor\n"))
	require.NoError(t, baseline.err)
	compiled := compileDefaultRoutingSource(t, routingRuntimeFixture("none", "sandbox:\n  agent:\n    runtime: cloud-hypervisor\ntools:\n  github:\n    toolsets: [repos]\n    token-broker: true\nsafe-outputs:\n  token-broker: true\n  threat-detection: false\n  create-issue:\n"))
	require.NoError(t, compiled.err)
	assert.Equal(t, isModelRoutingImplicit(baseline.data), isModelRoutingImplicit(compiled.data))
	assert.True(t, isModelRoutingImplicit(compiled.data))
	assert.False(t, usesTokenBrokerInference(compiled.data))
	env := make(map[string]string)
	NewCopilotEngine().addCopilotGitHubToolEnv(env, compiled.data)
	assert.Equal(t, "${{ steps.github-mcp-app-token.outputs.token }}", env["GITHUB_MCP_SERVER_TOKEN"])
	assert.NotContains(t, compiled.lock, "getBrokerToken('copilot-request'")
}

func TestTokenBrokerGitHubRequiresBootstrapIsolation(t *testing.T) {
	for _, sandbox := range []string{
		"sandbox:\n  agent:\n    version: v0.25.2\n",
		"sandbox:\n  agent: false\n",
	} {
		compiler := NewCompiler()
		source := "---\non: workflow_dispatch\nstrict: false\nengine: claude\n" + sandbox + "tools:\n  github:\n    token-broker: true\nsafe-outputs:\n  threat-detection: false\n---\nTest."
		data, err := compiler.ParseWorkflowString(source, "broker.md")
		require.NoError(t, err)
		_, err = compiler.CompileToYAML(data, "broker.md")
		require.ErrorContains(t, err, "keep Actions OIDC credentials out of the agent")
	}
}

func TestTokenBrokerGitHubImportOmittedFlagPreservesChoice(t *testing.T) {
	compiler := NewCompiler()
	result, err := compiler.MergeSafeOutputs(nil, []string{
		`{"token-broker":true,"create-issue":{}}`,
		`{"update-issue":{}}`,
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.TokenBroker)
	assert.NotNil(t, result.CreateIssues)
	assert.NotNil(t, result.UpdateIssues)
	// Also exercise the single-config merger without the caller's accumulated raw keys.
	result, err = mergeSafeOutputConfig(result, map[string]any{"add-comment": map[string]any{}}, nil, nil, compiler)
	require.NoError(t, err)
	assert.True(t, result.TokenBroker)
}

func TestTokenBrokerGitHubHandlerActionsTokenPermissions(t *testing.T) {
	_, _, jobs := compileGitHubBrokerTest(t, "safe-outputs:\n  token-broker: true\n  threat-detection: false\n  create-issue:\n  update-issue:\n    github-token: ${{ secrets.GITHUB_TOKEN }}\n")
	assertGitHubBrokerConsumer(t, jobs["safe_outputs"], "safe-outputs-app-token")
	assert.Equal(t, "write", jobs["safe_outputs"].Permissions["issues"])
}

func TestTokenBrokerGitHubCommentOverrideTargetLookup(t *testing.T) {
	for _, token := range []string{"", "${{ secrets.GITHUB_TOKEN }}"} {
		data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{TokenBroker: true, AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: token},
			Issues:               new(false),
			PullRequests:         new(true),
		}}}
		permissions := ComputePermissionsForSafeOutputs(data.SafeOutputs)
		level, ok := permissions.Get(PermissionIssues)
		assert.True(t, ok)
		assert.Equal(t, PermissionRead, level)
	}
}

func TestTokenBrokerGitHubStandaloneAssets(t *testing.T) {
	_, _, jobs := compileGitHubBrokerTest(t, "safe-outputs:\n  token-broker: true\n  threat-detection: false\n  upload-asset:\n")
	assertGitHubBrokerConsumer(t, jobs["upload_assets"], "safe-outputs-app-token")
	assert.NotEqual(t, "write", jobs["upload_assets"].Permissions["contents"])
	assertToken := func(t *testing.T, data *WorkflowData, config GitHubScriptStepConfig, want string) {
		t.Helper()
		var steps []string
		NewCompiler().addCustomActionGitHubToken(&steps, data, config)
		assert.Contains(t, strings.Join(steps, ""), "token: "+want)
	}
	data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{TokenBroker: true}}
	assertToken(t, data, GitHubScriptStepConfig{}, "${{ steps.safe-outputs-app-token.outputs.token }}")
	assertToken(t, data, GitHubScriptStepConfig{CustomToken: "${{ secrets.ASSET_TOKEN }}"}, "${{ secrets.ASSET_TOKEN }}")
	for _, step := range jobs["upload_assets"].Steps {
		if step.ID == "upload_assets" {
			assert.Equal(t, "${{ steps.safe-outputs-app-token.outputs.token }}", step.With["github-token"])
		}
	}
}
