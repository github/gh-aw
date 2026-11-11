//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func connectorGatewayWorkflow(t *testing.T, on string) string {
	t.Helper()
	var document map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(awConnectorSource(t)), &document))
	trigger := document["connectors"].(map[string]any)["tracker"].(map[string]any)["triggers"].(map[string]any)["issue-created"].(map[string]any)
	trigger["resourceOrigins"] = []string{"https://api.example.com"}
	trigger["resources"] = map[string]string{"/issue/id": "issues"}
	source, err := yaml.Marshal(document)
	require.NoError(t, err)
	header := strings.Replace(awConnectorHeader, "on: workflow_dispatch\n", on, 1)
	return header + string(source) + "---\nRead approved event references only.\n"
}

func parseConnectorGateway(t *testing.T, on string, enabled bool) (*Compiler, *WorkflowData, error) {
	t.Helper()
	compiler, root := awConnectorCompiler(t)
	compiler.repoConfig = &RepoConfig{GatewayDispatch: enabled}
	compiler.repoConfigLoaded = true
	data, err := compiler.ParseWorkflowString(connectorGatewayWorkflow(t, on), filepath.Join(root, ".github", "workflows", "connector-gateway.md"))
	return compiler, data, err
}

func TestConnectorGatewayCompilation(t *testing.T) {
	for _, on := range []string{
		"on:\n  gateway-dispatch:\n    tracker: [issue-created]\n",
		"on:\n  tracker-issue-created:\n",
	} {
		compiler, data, err := parseConnectorGateway(t, on, true)
		require.NoError(t, err)
		require.Len(t, data.WebhookSubscriptions, 1)
		subscription := data.WebhookSubscriptions[0]
		require.NotNil(t, subscription.Connector)
		require.Equal(t, "example.com/hmac-sha256", subscription.Connector.VerificationProfile)
		require.Equal(t, []string{"https://api.example.com"}, subscription.Connector.Origins)
		require.Len(t, subscription.Conditions, 2)
		require.Equal(t, "connector_scope_0", subscription.Conditions[1].Filter)
		require.Equal(t, []any{"ISSUE-1"}, subscription.Conditions[1].Values)
		require.Contains(t, data.On, "repository_dispatch")
		require.NotContains(t, data.On, "gateway-dispatch")
		output, err := compiler.CompileToYAML(data, "connector-gateway.md")
		require.NoError(t, err)
		require.Contains(t, output, "gateway_provenance:")
		require.Contains(t, output, "gateway_admission:")
		require.Contains(t, output, "needs.gateway_provenance.outputs.verified == 'true'")
		require.Contains(t, output, "needs.gateway_admission.outputs.verified == 'true'")
		require.Contains(t, output, "validate_webhook.cjs")
		require.NotContains(t, output, "${{ secrets.EXAMPLE_READ_TOKEN }}")
		require.NotContains(t, output, "${{ secrets.EXAMPLE_WRITE_TOKEN }}")
	}
}

func TestConnectorGatewayStillRequiresTrustedEnrollmentBoundary(t *testing.T) {
	on := "on:\n  gateway-dispatch:\n    tracker: [issue-created]\n"
	_, _, err := parseConnectorGateway(t, on, false)
	require.ErrorContains(t, err, "gateway-dispatch")
	for _, invalid := range []string{
		strings.Replace(on, "tracker:", "unregistered:", 1),
		strings.Replace(on, "issue-created", "missing", 1),
		strings.Replace(on, "on:\n", "on:\n  workflow_dispatch:\n", 1),
		strings.Replace(on, "[issue-created]", "[]", 1),
		strings.Replace(on, "[issue-created]", "['${{ inputs.event }}']", 1),
	} {
		_, _, err := parseConnectorGateway(t, invalid, true)
		require.Error(t, err)
	}
}

func TestConnectorGatewayFlatSelectionsAndNativeEvents(t *testing.T) {
	on := "on:\n  gateway-dispatch:\n    tracker: [issue-created]\n    projects-items:\n      project_id: PVT_example\n      types: [edited]\n"
	_, data, err := parseConnectorGateway(t, on, true)
	require.NoError(t, err)
	require.Len(t, data.WebhookSubscriptions, 2)
	require.Equal(t, "projects_v2_item", data.WebhookSubscriptions[0].Event)
	require.NotNil(t, data.WebhookSubscriptions[1].Connector)
}

func TestConnectorGatewayRejectsWrapperAndReservedAliases(t *testing.T) {
	_, _, err := parseConnectorGateway(t, "on:\n  gateway-dispatch:\n    connectors:\n      tracker: [issue-created]\n", true)
	require.ErrorContains(t, err, "on.gateway-dispatch.connectors is removed")
	for _, alias := range []string{"issue", "projects-items", "repos", "orgs", "connectors"} {
		t.Run(alias, func(t *testing.T) {
			compiler, root := awConnectorCompiler(t)
			source := connectorGatewayWorkflow(t, "on:\n  tracker-issue-created:\n")
			source = strings.Replace(source, "  tracker:\n", "  "+alias+":\n", 1)
			source = strings.Replace(source, "tracker-issue-created", alias+"-issue-created", 1)
			_, err := compiler.ParseWorkflowString(source, filepath.Join(root, "reserved.md"))
			require.ErrorContains(t, err, "reserved webhook gateway field")
		})
	}
}

func TestConnectorGatewayMultipleFlatAliases(t *testing.T) {
	compiler, root := awConnectorCompiler(t)
	source := connectorGatewayWorkflow(t, "on:\n  gateway-dispatch:\n    tracker: [issue-created]\n    another: [issue-created]\n")
	var document map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(strings.SplitN(source, "---", 3)[1]), &document))
	registrations := document["connectors"].(map[string]any)
	registrations["another"] = registrations["tracker"]
	content, err := yaml.Marshal(document)
	require.NoError(t, err)
	compiler.repoConfig, compiler.repoConfigLoaded = &RepoConfig{GatewayDispatch: true}, true
	data, err := compiler.ParseWorkflowString("---\n"+string(content)+"---\nInspect approved references.\n", filepath.Join(root, "two.md"))
	require.NoError(t, err)
	require.Len(t, data.WebhookSubscriptions, 2)
	require.NotEqual(t, data.WebhookSubscriptions[0].Event, data.WebhookSubscriptions[1].Event)
}

func TestConnectorGatewayGoToNodeRelay(t *testing.T) {
	_, data, err := parseConnectorGateway(t, "on:\n  gateway-dispatch:\n    tracker: [issue-created]\n", true)
	require.NoError(t, err)
	payload, err := json.Marshal(data.WebhookSubscriptions)
	require.NoError(t, err)
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	assets, err := filepath.Abs("../../actions/setup/js")
	require.NoError(t, err)
	driver := `
const assert = require("node:assert/strict");
const { validateWebhook } = require(process.argv[1] + "/validate_webhook.cjs");
const subscriptions = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const source = subscriptions[0];
const filters = Object.fromEntries(source.conditions.map(condition => [condition.filter, [condition.values[0]]]));
const delivery = {delivery_id:"signed-source-delivery",payload:{url:"https://api.example.com/issues/ISSUE-1",type:[source.connector.trigger],filters}};
const event = {action:source.dispatch.event_type,client_payload:{schema_version:2,aw_context:{webhook:{event:source.event,deliveries:[delivery]}}}};
const env = {GITHUB_EVENT_NAME:"repository_dispatch",GH_AW_WEBHOOK_SUBSCRIPTIONS:JSON.stringify(subscriptions)};
assert.equal(validateWebhook(event,env).skipped,false);
for (const mutate of [
  value => value.client_payload.aw_context.webhook.deliveries[0].payload.url = "https://evil.example/issues/ISSUE-1",
  value => value.client_payload.aw_context.webhook.deliveries[0].payload.filters.connector_scope_0 = ["OTHER"],
  value => value.client_payload.aw_context.webhook.deliveries[0].payload.type = ["other-trigger"],
  value => value.client_payload.aw_context.webhook.deliveries[0].payload.body = "unfiltered source content",
]) {
  const invalid = structuredClone(event); mutate(invalid);
  assert.throws(() => validateWebhook(invalid,env));
}
`
	command := exec.CommandContext(t.Context(), node, "-e", driver, assets)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Empty(t, output)
}

func TestConnectorGatewayDeclarationOrigins(t *testing.T) {
	source := connectorGatewayWorkflow(t, "on:\n  tracker-issue-created:\n")
	for _, replacement := range []string{"http://api.example.com", "https://user:pass@api.example.com", "https://api.example.com/path", "https://*.example.com"} {
		compiler, root := awConnectorCompiler(t)
		_, err := compiler.ParseWorkflowString(strings.ReplaceAll(source, "https://api.example.com", replacement), filepath.Join(root, "origin.md"))
		require.Error(t, err)
	}

}

func TestConnectorGatewayRejectsConditionExpressions(t *testing.T) {
	err := connectorGatewaySourceLiterals(&ConnectorWebhookSource{}, []WebhookCondition{{
		Filter: "connector_match_0", Path: []string{"event"}, Operator: "equals", ValueType: "string",
		Values: []any{"${{ inputs.untrusted }}"},
	}})
	require.ErrorContains(t, err, "expressions")
}

func TestConnectorGatewayImportedProviderSubscriptions(t *testing.T) {
	files, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	for _, file := range files {
		provider := strings.TrimSuffix(filepath.Base(file), ".md")
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		var metadata struct {
			Connectors map[string]struct {
				Triggers map[string]any `yaml:"triggers"`
			} `yaml:"connectors"`
		}
		frontmatter, _, found := strings.Cut(strings.TrimPrefix(string(source), "---\n"), "\n---\n")
		require.True(t, found)
		require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &metadata))
		if len(metadata.Connectors[provider].Triggers) == 0 {
			continue
		}
		t.Run(provider, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			installConnectorTestSources(t, root)
			c.repoConfig, c.repoConfigLoaded = &RepoConfig{GatewayDispatch: true}, true
			// Use approved fixture defaults where lists default empty, not discovery.
			var declaration map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &declaration))
			inputs := map[string]any{}
			connector := declaration["connectors"].(map[string]any)[provider].(map[string]any)
			resourceBindings := connector["resources"].(map[string]any)
			required := map[string]bool{}
			for _, rawTrigger := range metadata.Connectors[provider].Triggers {
				if resources, ok := rawTrigger.(map[string]any)["resources"].(map[string]any); ok {
					for _, resource := range resources {
						binding, err := json.Marshal(resourceBindings[resource.(string)])
						require.NoError(t, err)
						required[string(binding)] = true
					}
				}
			}
			for key, raw := range declaration["import-schema"].(map[string]any) {
				if definition := raw.(map[string]any); definition["type"] == "array" {
					if key == "closed-states" {
						inputs[key] = []string{"1"}
					}
					for binding := range required {
						if strings.Contains(binding, "github.aw.import-inputs."+key+" ") {
							inputs[key] = []string{"1"}
						}
					}
				}
			}
			triggers := make([]string, 0, len(metadata.Connectors[provider].Triggers))
			for trigger := range metadata.Connectors[provider].Triggers {
				triggers = append(triggers, trigger)
			}
			consumer, err := yaml.Marshal(map[string]any{
				"on":     map[string]any{"gateway-dispatch": map[string]any{provider: triggers}},
				"engine": "copilot", "permissions": map[string]any{"contents": "read"},
				"imports": []any{map[string]any{"uses": ".github/workflows/connectors/" + provider + ".md", "with": inputs}},
			})
			require.NoError(t, err)
			data, err := c.ParseWorkflowString("---\n"+string(consumer)+"---\nRead only event references.\n", filepath.Join(root, ".github", "workflows", "gateway.md"))
			require.NoError(t, err)
			require.Len(t, data.WebhookSubscriptions, len(triggers))
			require.Nil(t, data.Connectors[provider].Agent)
			require.Nil(t, data.Connectors[provider].SafeOutputs)
		})
	}
}
