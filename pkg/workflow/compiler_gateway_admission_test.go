//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayAdmissionIsTrustedAndProtectsEveryOtherJob(t *testing.T) {
	compiler := newWebhookCompiler()
	compiler.environmentOverride = "untrusted-environment"
	data, err := compiler.ParseWorkflowString(webhookWorkflow(`strict: false
env:
  GH_AW_GATEWAY_GET_TOKEN_URL: https://attacker.invalid/getToken
  GH_AW_GATEWAY_OIDC_AUDIENCE: https://attacker.invalid
  GH_AW_GATEWAY_WORKFLOW_SHA: forged
  GH_AW_GATEWAY_WORKFLOW_HASH: forged
  GH_AW_GATEWAY_WORKFLOW_PATH: forged
  GITHUB_API_URL: https://attacker.invalid
on:
  needs: [prepare]
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
      - run: echo custom
    outputs:
      activated: ${{ true }}
  activation:
    if: ${{ always() || true }}
`), "version-check.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "version-check.md")
	require.NoError(t, err)
	type gatewayStep struct {
		ID   string            `yaml:"id"`
		Env  map[string]string `yaml:"env"`
		With map[string]string `yaml:"with"`
	}
	var compiled struct {
		Jobs map[string]struct {
			Needs       any               `yaml:"needs"`
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			Environment string            `yaml:"environment"`
			Timeout     int               `yaml:"timeout-minutes"`
			Outputs     map[string]string `yaml:"outputs"`
			Env         map[string]string `yaml:"env"`
			Steps       []gatewayStep     `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
	require.Contains(t, compiled.Jobs, gatewayAdmissionJobName)
	admission := compiled.Jobs[gatewayAdmissionJobName]
	assert.Equal(t, gatewayProvenanceJobName, admission.Needs)
	assert.Empty(t, admission.Environment)
	assert.Empty(t, compiled.Jobs[gatewayProvenanceJobName].Environment)
	assert.Equal(t, "untrusted-environment", compiled.Jobs["agent"].Environment)
	assert.Equal(t, 5, admission.Timeout)
	assert.Equal(t, map[string]string{"contents": "read", "id-token": "write"}, admission.Permissions)
	assert.Equal(t, map[string]string{"verified": "${{ steps.verify-gateway-admission.outputs.verified }}"}, admission.Outputs)
	assert.Equal(t, map[string]string{"GITHUB_API_URL": "${{ github.api_url }}", "GITHUB_SERVER_URL": "${{ github.server_url }}"}, admission.Env)
	var verify *gatewayStep
	for i := range admission.Steps {
		if admission.Steps[i].ID == "verify-gateway-admission" {
			verify = &admission.Steps[i]
			break
		}
	}
	require.NotNil(t, verify)
	assert.Equal(t, "${{ secrets.GH_AW_GATEWAY_GET_TOKEN_URL || vars.GH_AW_GATEWAY_GET_TOKEN_URL }}", verify.Env["GH_AW_GATEWAY_GET_TOKEN_URL"])
	assert.Equal(t, "${{ secrets.GH_AW_GATEWAY_OIDC_AUDIENCE || vars.GH_AW_GATEWAY_OIDC_AUDIENCE }}", verify.Env["GH_AW_GATEWAY_OIDC_AUDIENCE"])
	assert.Equal(t, "${{ github.workflow_sha }}", verify.Env["GH_AW_GATEWAY_WORKFLOW_SHA"])
	assert.Equal(t, "${{ github.workflow_ref }}", verify.Env["GH_AW_GATEWAY_WORKFLOW_REF"])
	require.Regexp(t, "^[a-f0-9]{64}$", data.FrontmatterHash)
	assert.Equal(t, data.FrontmatterHash, verify.Env["GH_AW_GATEWAY_WORKFLOW_HASH"])
	assert.Equal(t, ".github/workflows/version-check.lock.yml", verify.Env["GH_AW_GATEWAY_WORKFLOW_PATH"])
	assert.Equal(t, "${{ github.run_id }}", verify.Env["GH_AW_GATEWAY_RUN_ID"])
	assert.Equal(t, "${{ github.run_attempt }}", verify.Env["GH_AW_GATEWAY_RUN_ATTEMPT"])
	assert.Equal(t, "${{ github.repository_id }}", verify.Env["GH_AW_GATEWAY_REPOSITORY_ID"])
	assert.Equal(t, "${{ github.repository_owner_id }}", verify.Env["GH_AW_GATEWAY_OWNER_ID"])
	assert.Equal(t, WebhookDispatchEventType("version-check"), verify.Env["GH_AW_GATEWAY_EVENT_TYPE"])
	assert.Equal(t, "${{ secrets.GITHUB_TOKEN }}", verify.With["github-token"])
	assert.Contains(t, verify.With["script"], "check_gateway_admission.cjs")
	for name, job := range compiled.Jobs {
		if name == gatewayProvenanceJobName {
			continue
		}
		assert.Contains(t, job.If, "needs.gateway_provenance.result == 'success' && needs.gateway_provenance.outputs.verified == 'true'", name)
		if name == gatewayAdmissionJobName {
			assert.NotContains(t, job.If, "needs.gateway_admission")
			continue
		}
		assert.Contains(t, job.Needs, gatewayAdmissionJobName, name)
		assert.Contains(t, job.Needs, gatewayProvenanceJobName, name)
		assert.Contains(t, job.If, "needs.gateway_admission.result == 'success' && needs.gateway_admission.outputs.verified == 'true'", name)
		assert.Equal(t, strings.TrimSpace(job.If), job.If, name)
	}
	for _, forbidden := range []string{"echo custom", "create-github-app-token", "private-key", "check_membership.cjs", "client_payload", "mint-otlp"} {
		assert.NotContains(t, extractJobSection(output, gatewayAdmissionJobName), forbidden)
	}
}

func TestGatewayAdmissionJobCannotBeCustomized(t *testing.T) {
	for _, settings := range []string{
		"{}", "{if: 'false'}", "{environment: untrusted}",
		"{outputs: {verified: '${{ true }}'}}",
		"{pre-steps: [{run: echo bypass}]}",
		"{steps: [{run: echo bypass}]}",
		"{uses: 'owner/repo/.github/workflows/bypass.yml@main'}",
	} {
		t.Run(settings, func(t *testing.T) {
			compiler := newWebhookCompiler()
			source := webhookWorkflow("strict: false\non:\n  gateway-dispatch:\n    projects-items: {project_id: PVT_example}\njobs:\n  gateway_admission: " + settings + "\n")
			data, err := compiler.ParseWorkflowString(source, "reserved.md")
			require.NoError(t, err)
			_, err = compiler.CompileToYAML(data, "reserved.md")
			require.ErrorContains(t, err, "jobs.gateway_admission is compiler-owned")
		})
	}
}

func TestGatewayAdmissionSetupDoesNotMintOtherCredentials(t *testing.T) {
	for _, mode := range []ActionMode{ActionModeRelease, ActionModeScript} {
		t.Run(string(mode), func(t *testing.T) {
			compiler := newWebhookCompiler()
			compiler.SetActionMode(mode)
			compiler.jobManager = NewJobManager()
			data := &WorkflowData{
				Name: "configured", WorkflowID: "configured",
				FrontmatterHash:      strings.Repeat("a", 64),
				WebhookSubscriptions: []WebhookSubscription{{Event: "projects_v2_item"}},
				ParsedFrontmatter: &FrontmatterConfig{
					Observability: &ObservabilityConfig{OTLP: &OTLPConfig{
						GitHubApp: &OTLPGitHubAppConfig{Audience: "https://example.com/collector"},
					}},
				},
			}

			require.NoError(t, compiler.addGatewayAdmissionJob(data))
			compiler.ensureOTLPOIDCJobPermissions(data)
			job, exists := compiler.jobManager.GetJob(gatewayAdmissionJobName)
			require.True(t, exists)
			steps := strings.Join(job.Steps, "")
			assert.NotContains(t, steps, "mint-otlp-oidc-token")
			assert.NotContains(t, steps, "https://example.com/collector")
			assert.NotContains(t, steps, "create-github-app-token")
			assert.Contains(t, job.Permissions, "id-token: write")
		})
	}
}

func TestGatewayAdmissionRequiresCompiledFrontmatterHash(t *testing.T) {
	compiler := newWebhookCompiler()
	err := compiler.addGatewayAdmissionJob(&WorkflowData{
		WorkflowID:           "missing-hash",
		WebhookSubscriptions: []WebhookSubscription{{Event: "projects_v2_item"}},
	})
	require.ErrorContains(t, err, "compiler-generated frontmatter hash")
}
