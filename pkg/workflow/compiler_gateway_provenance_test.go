//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayProvenanceProtectsAllJobs(t *testing.T) {
	source := webhookWorkflow(`strict: false
env:
  GH_AW_GATEWAY_APP_ID: "999"
  GH_AW_GATEWAY_APP_BOT_ID: "999"
  GITHUB_API_URL: https://attacker.invalid
  GITHUB_EVENT_PATH: /tmp/forged.json
on:
  needs: [prepare]
  bots: [unrelated-bot]
  gateway-dispatch:
    projects-items: {project_id: PVT_example}
jobs:
  prepare:
    runs-on: ubuntu-latest
    if: ${{ always() || true }}
    steps:
      - run: echo prepare
  pre_activation:
    pre-steps:
      - name: Author pre-step
        run: echo author
    outputs:
      activated: ${{ true }}
  activation:
    if: ${{ always() || true }}
  reusable:
    if: ${{ always() || true }}
    uses: owner/repo/.github/workflows/reusable.yml@main
    secrets: inherit
`)
	compiler := newWebhookCompiler()
	data, err := compiler.ParseWorkflowString(source, "provenance.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "provenance.md")
	require.NoError(t, err)
	var compiled struct {
		Jobs map[string]struct {
			Needs       any               `yaml:"needs"`
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			Env         map[string]string `yaml:"env"`
			Environment any               `yaml:"environment"`
			Outputs     map[string]string `yaml:"outputs"`
			Steps       []struct {
				ID   string            `yaml:"id"`
				Env  map[string]string `yaml:"env"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
	require.Contains(t, compiled.Jobs, gatewayProvenanceJobName)
	gate := compiled.Jobs[gatewayProvenanceJobName]
	assert.Empty(t, gate.Needs)
	assert.Empty(t, gate.If)
	assert.Nil(t, gate.Environment)
	assert.Equal(t, map[string]string{"contents": "read"}, gate.Permissions)
	assert.Equal(t, map[string]string{
		"GITHUB_API_URL": "${{ github.api_url }}", "GITHUB_SERVER_URL": "${{ github.server_url }}", "GITHUB_EVENT_PATH": "${{ github.event_path }}",
	}, gate.Env)
	assert.Equal(t, map[string]string{"verified": "${{ steps.verify-gateway-provenance.outputs.verified }}"}, gate.Outputs)
	require.NotEmpty(t, gate.Steps)
	verify := gate.Steps[len(gate.Steps)-1]
	assert.Equal(t, "verify-gateway-provenance", verify.ID)
	assert.Equal(t, map[string]string{
		"GH_AW_GATEWAY_APP_ID":        "${{ secrets.GH_AW_GATEWAY_APP_ID || vars.GH_AW_GATEWAY_APP_ID }}",
		"GH_AW_GATEWAY_APP_BOT_ID":    "${{ secrets.GH_AW_GATEWAY_APP_BOT_ID || vars.GH_AW_GATEWAY_APP_BOT_ID }}",
		"GH_AW_GATEWAY_EVENT_NAME":    "${{ github.event_name }}",
		"GH_AW_GATEWAY_EVENT_TYPE":    WebhookDispatchEventType("provenance"),
		"GH_AW_GATEWAY_ACTOR_ID":      "${{ github.actor_id }}",
		"GH_AW_GATEWAY_REPOSITORY_ID": "${{ github.repository_id }}",
		"GH_AW_GATEWAY_REPOSITORY":    "${{ github.repository }}",
	}, verify.Env)
	assert.Equal(t, "${{ secrets.GITHUB_TOKEN }}", verify.With["github-token"])
	assert.Contains(t, verify.With["script"], "check_gateway_provenance.cjs")
	for _, forbidden := range []string{"Author pre-step", "create-github-app-token", "private-key", "check_membership.cjs", "github.triggering_actor", "client_payload", "mint-otlp"} {
		assert.NotContains(t, extractJobSection(output, gatewayProvenanceJobName), forbidden)
	}
	require.Contains(t, compiled.Jobs, "prepare")
	require.Contains(t, compiled.Jobs, "reusable")
	for name, job := range compiled.Jobs {
		if name == gatewayProvenanceJobName {
			continue
		}
		if scalar, ok := job.Needs.(string); ok {
			assert.Equal(t, gatewayProvenanceJobName, scalar, name)
		} else {
			assert.Contains(t, job.Needs, gatewayProvenanceJobName, name)
		}
		if name != gatewayAdmissionJobName {
			assert.Contains(t, job.Needs, gatewayAdmissionJobName, name)
			assert.Contains(t, job.If, "needs.gateway_admission.result == 'success' && needs.gateway_admission.outputs.verified == 'true'", name)
		}
		assert.True(t, strings.HasPrefix(job.If, "${{ needs.gateway_provenance.result == 'success' && needs.gateway_provenance.outputs.verified == 'true'"), "%s: %s", name, job.If)
		assert.True(t, strings.HasSuffix(strings.TrimSpace(job.If), "}}"), "%s: %s", name, job.If)
		assert.Equal(t, strings.TrimSpace(job.If), job.If, "trailing scalar text must not turn the gate into a truthy string: %s", name)
	}
	assert.Contains(t, compiled.Jobs["prepare"].If, "&& (always() || true)")
	assert.Contains(t, compiled.Jobs["reusable"].If, "&& (always() || true)")
	assert.Contains(t, extractJobSection(output, "pre_activation"), "Author pre-step")
	assert.Contains(t, extractJobSection(output, "activation"), "validate_webhook.cjs")
	assert.NotContains(t, output, "check_membership.cjs")
}

func TestGatewayProvenanceJobCannotBeCustomized(t *testing.T) {
	for _, settings := range []string{
		"{}",
		"{if: 'false'}",
		"{outputs: {verified: '${{ true }}'}}",
		"{pre-steps: [{run: echo bypass}]}",
		"{steps: [{run: echo bypass}]}",
		"{uses: 'owner/repo/.github/workflows/bypass.yml@main'}",
	} {
		t.Run(settings, func(t *testing.T) {
			compiler := newWebhookCompiler()
			source := webhookWorkflow("strict: false\non:\n  gateway-dispatch:\n    projects-items: {project_id: PVT_example}\njobs:\n  gateway_provenance: " + settings + "\n")
			data, err := compiler.ParseWorkflowString(source, "reserved.md")
			require.NoError(t, err)
			_, err = compiler.CompileToYAML(data, "reserved.md")
			require.ErrorContains(t, err, "jobs.gateway_provenance is compiler-owned")
		})
	}
}

func TestGatewayProvenanceGuardsLoweredTypedAndStatusConditions(t *testing.T) {
	compiler := newWebhookCompiler()
	compiler.jobManager = NewJobManager()
	require.NoError(t, compiler.jobManager.AddJob(&Job{
		Name:    gatewayProvenanceJobName,
		Outputs: map[string]string{"verified": "${{ steps.verify.outputs.verified }}"},
	}))
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: gatewayAdmissionJobName, Needs: []string{gatewayProvenanceJobName}}))
	for name, condition := range map[string]string{
		"always": "always()", "failure": "${{ failure() }}", "cancelled": "cancelled()",
		"disjunction": "${{ always() || true }}", "unconditional": "",
	} {
		require.NoError(t, compiler.jobManager.AddJob(&Job{Name: name, If: condition}))
	}
	require.NoError(t, compiler.jobManager.AddJob(&Job{
		Name: "typed", Uses: "./.github/workflows/worker.yml",
		Actions: &actions.Job{If: actions.Binary{
			Left: actions.Literal("build: release"), Operator: "==", Right: actions.Literal("build: release"),
		}},
	}))
	require.NoError(t, compiler.jobManager.resolveActions())
	data := &WorkflowData{WebhookSubscriptions: []WebhookSubscription{{Event: "projects_v2_item"}}}
	require.NoError(t, compiler.guardGatewayProvenanceJobs(data))
	require.NoError(t, compiler.jobManager.ValidateDependencies())
	content := renderActionsASTJobs(compiler.jobManager)
	var parsed struct {
		Jobs map[string]struct {
			If    string `yaml:"if"`
			Needs any    `yaml:"needs"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(content), &parsed))
	for name, job := range parsed.Jobs {
		if name == gatewayProvenanceJobName {
			continue
		}
		if name == gatewayAdmissionJobName {
			assert.Equal(t, gatewayProvenanceJobName, job.Needs, name)
		} else {
			assert.Contains(t, job.Needs, gatewayProvenanceJobName, name)
			assert.Contains(t, job.Needs, gatewayAdmissionJobName, name)
			assert.Contains(t, job.If, "needs.gateway_admission.result == 'success' && needs.gateway_admission.outputs.verified == 'true'", name)
		}
		assert.Contains(t, job.If, "needs.gateway_provenance.result == 'success' && needs.gateway_provenance.outputs.verified == 'true'", name)
		assert.NotContains(t, job.If, "${{ ${{", name)
		assert.Equal(t, strings.TrimSpace(job.If), job.If, name)
	}
	assert.Contains(t, parsed.Jobs["typed"].If, "'build: release'")
	assert.Contains(t, parsed.Jobs["disjunction"].If, "&& (always() || true)")
}

func TestGatewayProvenanceRejectsInterpolatedConditions(t *testing.T) {
	for _, condition := range []string{
		"${{ false }} || ${{ true }}",
		"${{ false }} trailing",
		"false) }} trailing",
		"false) }}${{ true",
		"'unclosed",
		"'backslash\\' }} trailing",
		"true) || true || (true",
		"${{ false) || true || (false }}",
		"contains('value', 'value'",
		"${{ }}",
	} {
		t.Run(condition, func(t *testing.T) {
			compiler := newWebhookCompiler()
			compiler.jobManager = NewJobManager()
			require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "custom", If: condition}))
			err := compiler.guardGatewayProvenanceJobs(&WorkflowData{WebhookSubscriptions: []WebhookSubscription{{Event: "projects_v2_item"}}})
			require.ErrorContains(t, err, "jobs.custom.if must be one GitHub Actions expression")
		})
	}
	for _, condition := range []string{
		"always() || true",
		"contains('}}', '}}')",
		"contains('${{ text }}', 'text')",
		"contains('can''t}}', '}}')",
	} {
		assert.True(t, isGatewayProvenanceCondition(condition), condition)
	}
}

func TestGatewayProvenanceDoesNotChangeNativeAuthorization(t *testing.T) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString(webhookWorkflow("on: push\n"), "native.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "native.md")
	require.NoError(t, err)
	assert.NotContains(t, output, "gateway_provenance")
	assert.NotContains(t, output, "gateway_admission")
	assert.Contains(t, output, "check_membership.cjs")
}

func TestGatewayProvenanceSetupDoesNotMintConfiguredCredentials(t *testing.T) {
	for _, mode := range []ActionMode{ActionModeRelease, ActionModeScript} {
		t.Run(string(mode), func(t *testing.T) {
			compiler := newWebhookCompiler()
			compiler.SetActionMode(mode)
			compiler.jobManager = NewJobManager()
			data := &WorkflowData{
				Name: "configured", WorkflowID: "configured",
				WebhookSubscriptions: []WebhookSubscription{{Event: "projects_v2_item"}},
				ParsedFrontmatter: &FrontmatterConfig{
					Observability: &ObservabilityConfig{
						OTLP: &OTLPConfig{GitHubApp: &OTLPGitHubAppConfig{Audience: "https://example.com/collector"}},
					},
				},
			}
			require.NoError(t, compiler.addGatewayProvenanceJob(data))
			compiler.ensureOTLPOIDCJobPermissions(data)
			gate, exists := compiler.jobManager.GetJob(gatewayProvenanceJobName)
			require.True(t, exists)
			assert.NotContains(t, strings.Join(gate.Steps, ""), "mint-otlp-oidc-token")
			assert.NotContains(t, strings.Join(gate.Steps, ""), "https://example.com/collector")
			assert.NotContains(t, gate.Permissions, "id-token")
			if mode.IsScript() {
				assert.Contains(t, strings.Join(gate.Steps, ""), "if: always()\n        run: |\n          bash \"${GH_AW_ACTIONS_SOURCE_DIR}/actions/setup/clean.sh\"")
			} else {
				assert.NotContains(t, strings.Join(gate.Steps, ""), "Clean Scripts")
			}
		})
	}
}

func TestGatewayProvenanceWithConditionalRuntimeApp(t *testing.T) {
	compiler := newWebhookCompiler()
	source := webhookWorkflow(`strict: false
on:
  gateway-dispatch:
    projects-items: {project_id: PVT_example}
github-app:
  client-id: ${{ secrets.GH_AW_APP_CLIENT_ID }}
  private-key: ${{ secrets.GH_AW_APP_PRIVATE_KEY }}
  ignore-if-missing: true
tools:
  github:
    toolsets: [repos]
safe-outputs:
  create-issue:
`)
	data, err := compiler.ParseWorkflowString(source, "runtime-app.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "runtime-app.md")
	require.NoError(t, err)
	var compiled struct {
		Jobs map[string]struct {
			If      string            `yaml:"if"`
			Outputs map[string]string `yaml:"outputs"`
			Steps   []struct {
				Uses string         `yaml:"uses"`
				If   string         `yaml:"if"`
				With map[string]any `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
	gate := extractJobSection(output, gatewayProvenanceJobName)
	assert.NotContains(t, gate, "create-github-app-token")
	assert.NotContains(t, gate, "GH_AW_APP_PRIVATE_KEY")
	admission := extractJobSection(output, gatewayAdmissionJobName)
	assert.NotContains(t, admission, "create-github-app-token")
	assert.NotContains(t, admission, "GH_AW_APP_PRIVATE_KEY")
	var mintedJobs []string
	for name, job := range compiled.Jobs {
		for _, value := range job.Outputs {
			assert.NotContains(t, value, ".outputs.token", name)
		}
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/create-github-app-token@") {
				continue
			}
			mintedJobs = append(mintedJobs, name)
			assert.Regexp(t, `^actions/create-github-app-token@[a-f0-9]{40}$`, step.Uses)
			assert.Equal(t, false, step.With["skip-token-revoke"], name)
			assert.Equal(t, "${{ secrets.GH_AW_APP_CLIENT_ID }}", step.With["client-id"], name)
			assert.Equal(t, "${{ secrets.GH_AW_APP_PRIVATE_KEY }}", step.With["private-key"], name)
			assert.Contains(t, step.If, "env.GH_AW_IGNORE_IF_MISSING_APP_ID != ''", name)
			assert.Contains(t, step.If, "env.GH_AW_IGNORE_IF_MISSING_PRIVATE_KEY != ''", name)
			assert.Contains(t, job.If, "needs.gateway_provenance.outputs.verified == 'true'", name)
			assert.Contains(t, job.If, "needs.gateway_admission.outputs.verified == 'true'", name)
		}
	}
	assert.Contains(t, mintedJobs, "agent")
	assert.Contains(t, mintedJobs, "safe_outputs")
}

func TestGatewayProvenanceRejectsDisabledTokenRevocation(t *testing.T) {
	for _, validation := range []struct {
		name string
		skip bool
	}{{"validated", false}, {"skip-schema", true}} {
		for _, tc := range []struct {
			job, section string
		}{{"agent", "setup-steps"}, {"activation", "pre-steps"}, {"activation", "steps"}, {"custom", "steps"}} {
			t.Run(validation.name+"/"+tc.job+"/"+tc.section, func(t *testing.T) {
				compiler := newWebhookCompiler()
				compiler.skipValidation = validation.skip
				source := webhookWorkflow("strict: false\non:\n  gateway-dispatch:\n    projects-items: {project_id: PVT_example}\njobs:\n  " + tc.job + ":\n    runs-on: ubuntu-latest\n    " + tc.section + `:
      - uses: actions/create-github-app-token@v3
        with:
          client-id: ${{ secrets.GH_AW_APP_CLIENT_ID }}
          private-key: ${{ secrets.GH_AW_APP_PRIVATE_KEY }}
          repositories: ${{ github.repository }}
          permission-contents: read
          skip-token-revoke: true
`)
				data, err := compiler.ParseWorkflowString(source, "no-revocation.md")
				require.NoError(t, err)
				_, err = compiler.CompileToYAML(data, "no-revocation.md")
				require.ErrorContains(t, err, "gateway-dispatch requires actions/create-github-app-token")
				assert.Contains(t, err.Error(), "revoke its token")
			})
		}
	}
}
