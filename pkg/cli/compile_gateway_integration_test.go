//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileGatewayDispatchIntegration(t *testing.T) {
	for _, entry := range []struct {
		path   string
		events []string
	}{
		{"pkg/cli/workflows/test-gateway-github-notifications.md", []string{
			"issues", "pull_request", "discussion", "issue_comment", "pull_request_comment",
			"pull_request_review", "pull_request_review_comment", "pull_request_review_thread", "discussion_comment", "commit_comment",
		}},
		{"pkg/cli/workflows/test-gateway-resource-events.md", []string{"projects_v2_item", "code_scanning_alert", "dependabot_alert", "jira_issue", "azure_devops_issue"}},
		{"pkg/cli/workflows/test-gateway-commands.md", []string{"slash_command", "label_command"}},
		{".github/workflows/smoke-security-alerts.md", []string{"code_scanning_alert", "dependabot_alert"}},
	} {
		t.Run(filepath.Base(entry.path), func(t *testing.T) {
			setup := setupIntegrationTest(t)
			defer setup.cleanup()
			name := strings.TrimSuffix(filepath.Base(entry.path), ".md")
			source, err := os.ReadFile(filepath.Join(projectRoot, entry.path))
			require.NoError(t, err)
			path := filepath.Join(setup.workflowsDir, name+".md")
			require.NoError(t, os.WriteFile(path, source, 0o600))
			config := filepath.Join(setup.tempDir, workflow.RepoConfigFileName)
			require.NoError(t, os.WriteFile(config, []byte(`{"gateway-dispatch":true}`), 0o600))
			var stderr bytes.Buffer
			cmd := exec.Command(setup.binaryPath, "compile", "--json", path)
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			require.NoError(t, err, "%s\n%s", output, stderr.String())
			var results []ValidationResult
			require.NoError(t, json.Unmarshal(output, &results))
			require.Len(t, results, 1)
			require.True(t, results[0].Valid, "%+v", results[0].Errors)
			lock, err := os.ReadFile(filepath.Join(setup.workflowsDir, name+".lock.yml"))
			require.NoError(t, err)
			assertGatewayDispatchIntegrationLock(t, name, lock, results[0], entry.events)
			assertGatewayManifestWireContract(t, name, lock, output, entry.events)
		})
	}
}

func TestCompileGatewayDispatchRejectsExpressionsIntegration(t *testing.T) {
	for name, trigger := range map[string]string{
		"notification": `issue: {filters: {labels: [triage, "${{ vars.LABEL }}"]}}`,
		"project":      `projects-items: {project_id: "${{ vars.PROJECT }}"}`,
		"command":      `label-command: {names: [review, "${{ vars.LABEL }}"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			setup := setupIntegrationTest(t)
			defer setup.cleanup()
			path := filepath.Join(setup.workflowsDir, "expression-"+name+".md")
			source := "---\nstrict: false\non:\n  gateway-dispatch:\n    " + trigger + "\npermissions:\n  contents: read\nengine: copilot\n---\nHandle notifications.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			config := filepath.Join(setup.tempDir, workflow.RepoConfigFileName)
			require.NoError(t, os.WriteFile(config, []byte(`{"gateway-dispatch":true}`), 0o600))
			lintCmd := exec.Command(setup.binaryPath, "lint", "--rules", "gateway-dispatch-literals", "--json", path)
			lintOutput, err := lintCmd.Output()
			require.Error(t, err)
			var diagnostics []workflowLintDiagnostic
			require.NoError(t, json.Unmarshal(lintOutput, &diagnostics))
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "gateway-dispatch-literals", diagnostics[0].RuleID)
			assert.Equal(t, "error", string(diagnostics[0].Severity))
			assert.Contains(t, diagnostics[0].Path, "on.gateway-dispatch.")
			var stderr bytes.Buffer
			cmd := exec.Command(setup.binaryPath, "compile", "--json", path)
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			require.Error(t, err, "%s\n%s", output, stderr.String())
			var results []ValidationResult
			require.NoError(t, json.Unmarshal(output, &results))
			require.Len(t, results, 1)
			require.False(t, results[0].Valid)
			require.Len(t, results[0].Errors, 1)
			assert.Contains(t, results[0].Errors[0].Message, "gateway-dispatch")
			if name != "notification" {
				assert.Contains(t, results[0].Errors[0].Message, "GitHub Actions expressions are not supported")
			}
			assert.Empty(t, results[0].WebhookSubscriptions)
			assert.NoFileExists(t, strings.TrimSuffix(path, ".md")+".lock.yml", "rejected expressions must not produce a lock file or manifest")
		})
	}
}

func TestCompileGatewayDispatchRejectsProvenanceOverridesIntegration(t *testing.T) {
	for name, tc := range map[string]struct {
		job     string
		message string
	}{
		"verifier-output":        {"gateway_provenance:\n    outputs: {verified: '${{ true }}'}", "jobs.gateway_provenance is compiler-owned"},
		"interpolated-condition": {"custom:\n    uses: owner/repo/.github/workflows/reusable.yml@main\n    if: '${{ false }} || ${{ true }}'", "must be one GitHub Actions expression"},
		"token-revocation":       {"activation:\n    pre-steps:\n      - uses: actions/create-github-app-token@v3\n        with:\n          client-id: ${{ secrets.GH_AW_APP_CLIENT_ID }}\n          private-key: ${{ secrets.GH_AW_APP_PRIVATE_KEY }}\n          repositories: ${{ github.repository }}\n          permission-contents: read\n          skip-token-revoke: true", "revoke its token"},
	} {
		t.Run(name, func(t *testing.T) {
			setup := setupIntegrationTest(t)
			defer setup.cleanup()
			path := filepath.Join(setup.workflowsDir, name+".md")
			source := "---\nstrict: false\non:\n  gateway-dispatch:\n    projects-items: {project_id: PVT_example}\njobs:\n  " + tc.job + "\npermissions:\n  contents: read\nengine: copilot\n---\nHandle notifications.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(setup.tempDir, workflow.RepoConfigFileName), []byte(`{"gateway-dispatch":true}`), 0o600))
			var stderr bytes.Buffer
			cmd := exec.Command(setup.binaryPath, "compile", "--json", path)
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			require.Error(t, err, "%s\n%s", output, stderr.String())
			var results []ValidationResult
			require.NoError(t, json.Unmarshal(output, &results))
			require.Len(t, results, 1)
			require.False(t, results[0].Valid)
			require.Len(t, results[0].Errors, 1)
			assert.Contains(t, results[0].Errors[0].Message, tc.message)
			assert.NoFileExists(t, strings.TrimSuffix(path, ".md")+".lock.yml")
		})
	}
}

func assertGatewayDispatchIntegrationLock(t *testing.T, name string, lock []byte, result ValidationResult, events []string) {
	t.Helper()
	route := workflow.WebhookDispatchEventType(name)
	var compiled struct {
		On   map[string]any `yaml:"on"`
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Needs       any               `yaml:"needs"`
			If          string            `yaml:"if"`
			Steps       []struct {
				ID  string            `yaml:"id"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(lock, &compiled))
	require.Equal(t, map[string]any{
		"repository_dispatch": map[string]any{"types": []any{route}},
	}, compiled.On, "gateway-dispatch must lower to only its workflow-specific repository_dispatch route")
	require.NotEmpty(t, compiled.Jobs)
	metadata, legacy, err := workflow.ExtractMetadataFromLockFile(string(lock))
	require.NoError(t, err)
	require.False(t, legacy)
	require.NotNil(t, metadata)
	require.Regexp(t, "^[a-f0-9]{64}$", metadata.FrontmatterHash)
	require.Contains(t, compiled.Jobs, "gateway_admission")
	admissionSteps := compiled.Jobs["gateway_admission"].Steps
	require.NotEmpty(t, admissionSteps)
	admission := admissionSteps[len(admissionSteps)-1]
	assert.Equal(t, "verify-gateway-admission", admission.ID)
	assert.Equal(t, metadata.FrontmatterHash, admission.Env["GH_AW_GATEWAY_WORKFLOW_HASH"])
	require.Contains(t, compiled.Jobs, "gateway_provenance")
	assert.Empty(t, compiled.Jobs["gateway_provenance"].Needs)
	assert.Equal(t, map[string]string{"contents": "read"}, compiled.Jobs["gateway_provenance"].Permissions)
	gateSteps := compiled.Jobs["gateway_provenance"].Steps
	require.NotEmpty(t, gateSteps)
	verify := gateSteps[len(gateSteps)-1]
	assert.Equal(t, "verify-gateway-provenance", verify.ID)
	assert.Equal(t, "${{ secrets.GH_AW_GATEWAY_APP_ID || vars.GH_AW_GATEWAY_APP_ID }}", verify.Env["GH_AW_GATEWAY_APP_ID"])
	assert.Equal(t, "${{ secrets.GH_AW_GATEWAY_APP_BOT_ID || vars.GH_AW_GATEWAY_APP_BOT_ID }}", verify.Env["GH_AW_GATEWAY_APP_BOT_ID"])
	for name, job := range compiled.Jobs {
		if name == "gateway_provenance" {
			continue
		}
		if scalar, ok := job.Needs.(string); ok {
			assert.Equal(t, "gateway_provenance", scalar, name)
		} else {
			assert.Contains(t, job.Needs, "gateway_provenance", name)
		}
		assert.Contains(t, job.If, "needs.gateway_provenance.result == 'success'", name)
		assert.Contains(t, job.If, "needs.gateway_provenance.outputs.verified == 'true'", name)
		assert.Equal(t, strings.TrimSpace(job.If), job.If, "provenance conditions cannot contain truthy trailing text: %s", name)
	}
	if name == "smoke-security-alerts" {
		permissions := compiled.Jobs["agent"].Permissions
		assert.Equal(t, "write", permissions["copilot-requests"])
		assert.Equal(t, "read", permissions["security-events"])
		assert.Equal(t, "read", permissions["vulnerability-alerts"])
	}
	manifest, err := workflow.ExtractGHAWManifestFromLockFile(string(lock))
	require.NoError(t, err)
	require.NotNil(t, manifest)
	assert.Contains(t, manifest.Secrets, "GH_AW_GATEWAY_APP_ID")
	assert.Contains(t, manifest.Secrets, "GH_AW_GATEWAY_APP_BOT_ID")
	require.Equal(t, result.WebhookSubscriptions, manifest.WebhookSubscriptions)
	actualEvents := make([]string, 0, len(manifest.WebhookSubscriptions))
	for _, subscription := range manifest.WebhookSubscriptions {
		actualEvents = append(actualEvents, subscription.Event)
		assert.Equal(t, "repository_dispatch", subscription.Dispatch.Method)
		assert.Equal(t, route, subscription.Dispatch.EventType)
		assert.Equal(t, 2, subscription.SchemaVersion)
		assert.Equal(t, "resource-url-types-filters/v2", subscription.Dispatch.PayloadProjection.Policy)
	}
	assert.ElementsMatch(t, events, actualEvents)
	assert.NotContains(t, string(lock), "check_command_position.cjs")
	assert.NotContains(t, string(lock), "workflow_dispatch:")
	assert.Contains(t, string(lock), "validate_webhook.cjs")
	assert.Contains(t, string(lock), "check_gateway_provenance.cjs")
	assert.Contains(t, string(lock), "toJSON(fromJSON(steps.generate_aw_info.outputs.aw_context || '{}').webhook.deliveries)")
}

func assertGatewayManifestWireContract(t *testing.T, name string, lock, output []byte, events []string) {
	t.Helper()
	var manifest map[string]json.RawMessage
	const prefix = "# gh-aw-manifest: "
	var headers []string
	for line := range strings.SplitSeq(string(lock), "\n") {
		if jsonText, found := strings.CutPrefix(line, prefix); found {
			headers = append(headers, jsonText)
		}
	}
	require.Len(t, headers, 1, "downstream discovery requires one single-line manifest")
	require.NoError(t, json.Unmarshal([]byte(headers[0]), &manifest))
	var subscriptions []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(manifest["webhook_subscriptions"], &subscriptions))
	require.Len(t, subscriptions, len(events))
	var cliResults []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(output, &cliResults))
	require.Len(t, cliResults, 1)
	assert.JSONEq(t, string(cliResults[0]["webhook_subscriptions"]), string(manifest["webhook_subscriptions"]))
	for _, subscription := range subscriptions {
		var event string
		require.NoError(t, json.Unmarshal(subscription["event"], &event))
		require.Contains(t, events, event)
		assert.JSONEq(t, `2`, string(subscription["schema_version"]))
		var types []string
		require.NoError(t, json.Unmarshal(subscription["types"], &types))
		require.NotEmpty(t, types)
		var conditions []map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(subscription["conditions"], &conditions))
		require.NotNil(t, conditions, "conditions must serialize as an array, including when empty")
		assertGatewayManifestDispatch(t, name, subscription["dispatch"])
		assertGatewayManifestSourceMetadata(t, event, subscription)
		if name == "test-gateway-resource-events" {
			switch event {
			case "code_scanning_alert":
				assert.JSONEq(t, `["appeared_in_branch"]`, string(subscription["types"]))
				assert.JSONEq(t, `[{"filter":"severity","path":["alert","rule","severity"],"operator":"equals","value_type":"string","values":["error"]}]`, string(subscription["conditions"]))
			case "dependabot_alert":
				assert.JSONEq(t, `["dismissed"]`, string(subscription["types"]))
				assert.JSONEq(t, `[{"filter":"severity","path":["alert","security_advisory","severity"],"operator":"equals","value_type":"string","values":["critical"]}]`, string(subscription["conditions"]))
			}
		}
		assert.NotContains(t, subscription, "sources")
		if name == "test-gateway-github-notifications" {
			assert.JSONEq(t, `["github/*"]`, string(subscription["repos"]))
			assert.JSONEq(t, `["github"]`, string(subscription["orgs"]))
		} else if name == "test-gateway-commands" {
			assert.JSONEq(t, `["github/*"]`, string(subscription["repos"]))
		} else if name == "smoke-security-alerts" {
			assert.JSONEq(t, `["github/gh-aw"]`, string(subscription["repos"]))
		} else {
			assert.NotContains(t, subscription, "repos")
			assert.NotContains(t, subscription, "orgs")
		}
	}
}

func assertGatewayManifestDispatch(t *testing.T, name string, raw json.RawMessage) {
	t.Helper()
	expected, err := json.Marshal(map[string]any{
		"repository": "${{ github.repository }}",
		"method":     "repository_dispatch",
		"event_type": workflow.WebhookDispatchEventType(name),
		"workflow":   ".github/workflows/" + name + ".lock.yml",
		"payload_projection": map[string]string{
			"policy": "resource-url-types-filters/v2",
		},
		"context": map[string]string{
			"envelope":   "github.event.client_payload",
			"aw_context": "github.event.client_payload.aw_context",
			"event":      "github.event.client_payload.aw_context.webhook.event",
			"deliveries": "github.event.client_payload.aw_context.webhook.deliveries",
			"items":      "aw_context.items",
		},
		"batch":             map[string]string{"mode": "same-event", "deduplicate_by": "delivery_id"},
		"max_payload_bytes": 65535,
		"max_request_bytes": 65535,
	})
	require.NoError(t, err)
	assert.JSONEq(t, string(expected), string(raw))
}

func assertGatewayManifestSourceMetadata(t *testing.T, event string, subscription map[string]json.RawMessage) {
	t.Helper()
	expected := map[string]string{
		"issues":                      `{"native_events":["issues"],"conditions":[{"filter":"labels","path":["issue","labels","*","name"],"operator":"equals","value_type":"string","values":["triage"]}]}`,
		"pull_request":                `{"native_events":["pull_request"],"conditions":[{"filter":"base","path":["pull_request","base","ref"],"operator":"equals","value_type":"string","values":["main"]},{"filter":"draft","path":["pull_request","draft"],"operator":"equals","value_type":"boolean","values":[false]}]}`,
		"discussion":                  `{"native_events":["discussion"]}`,
		"issue_comment":               `{"native_events":["issue_comment"]}`,
		"pull_request_comment":        `{"native_events":["issue_comment"]}`,
		"pull_request_review":         `{"native_events":["pull_request_review"],"conditions":[{"filter":"state","path":["review","state"],"operator":"equals","value_type":"string","values":["approved"]}]}`,
		"pull_request_review_comment": `{"native_events":["pull_request_review_comment"]}`,
		"pull_request_review_thread":  `{"native_events":["pull_request_review_thread"]}`,
		"discussion_comment":          `{"native_events":["discussion_comment"]}`,
		"commit_comment":              `{"native_events":["commit_comment"]}`,
		"projects_v2_item":            `{"conditions":[{"filter":"field.name","path":["changes","field_value","field_name"],"operator":"equals","value_type":"string","values":["Status"]},{"filter":"field.type","path":["changes","field_value","field_type"],"operator":"equals","value_type":"string","values":["single_select"]},{"filter":"project_id","path":["projects_v2_item","project_node_id"],"operator":"equals","value_type":"string","values":["PVT_example"]}],"any_of":[[{"filter":"field.from.name","path":["changes","field_value","from","name"],"operator":"equals","value_type":"string","values":["Todo"]},{"filter":"field.to.name","path":["changes","field_value","to","name"],"operator":"equals","value_type":"string","values":["Ready"]}]]}`,
		"code_scanning_alert":         `{}`,
		"dependabot_alert":            `{}`,
		"jira_issue":                  `{"provider":"jira","native_events":["jira:issue_created","jira:issue_updated"],"activity_normalization":{"policy":"jira-issue/v1"},"conditions":[{"filter":"project","path":["issue","fields","project","key"],"operator":"equals","value_type":"string","values":["APP"]}]}`,
		"azure_devops_issue":          `{"provider":"azure-devops","native_events":["workitem.updated"],"activity_normalization":{"policy":"azure-devops-issue/v1","closed_states":["Closed","Done"]},"conditions":[{"filter":"project_id","path":["resourceContainers","project","id"],"operator":"equals","value_type":"string","values":["project-uuid"]}]}`,
		"slash_command":               `{"native_events":["issue_comment"],"command_filter":{"policy":"slash-command/v1","names":["fix*","review"],"events":["issue_comment","pull_request_comment"],"strategy":"inline"},"conditions":[]}`,
		"label_command":               `{"native_events":["issues","pull_request"],"command_filter":{"policy":"label-command/v1","names":["review"],"events":["issues","pull_request"],"strategy":"inline","remove_label":true},"conditions":[]}`,
	}
	require.Contains(t, expected, event)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(expected[event]), &fields))
	for key, value := range fields {
		assert.JSONEq(t, string(value), string(subscription[key]), "%s.%s", event, key)
	}
	for _, key := range []string{"provider", "native_events", "activity_normalization", "command_filter", "any_of"} {
		if _, present := fields[key]; !present {
			assert.NotContains(t, subscription, key, "%s must not emit unrelated metadata", event)
		}
	}
}
