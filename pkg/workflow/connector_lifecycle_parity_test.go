//go:build !integration

package workflow

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func lifecycleConnector(t *testing.T, provider string) *DevelopmentConnector {
	t.Helper()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	inputs := map[string]any{}
	switch provider {
	case "linear":
		inputs["organizations"], inputs["teams"] = []any{"org"}, []any{"team"}
	case "jira":
		inputs["site"], inputs["projects"], inputs["issue-ids"] = "https://approved.atlassian.net", []any{"APP"}, []any{"10001"}
	case "azure-devops":
		inputs["closed-states"] = []any{"Done", "Closed"}
	}
	connector, err := LoadDevelopmentConnector(root, provider, "", inputs, nil)
	require.NoError(t, err)
	return connector
}

func lifecyclePayload(provider string) map[string]any {
	switch provider {
	case "linear":
		return map[string]any{"action": "update", "type": "Issue", "organizationId": "org",
			"data":        map[string]any{"id": "issue", "teamId": "team", "completedAt": nil, "canceledAt": nil, "labelIds": []any{}},
			"updatedFrom": map[string]any{}, "url": "https://linear.app/team/issue/ENG-123"}
	case "jira":
		return map[string]any{"webhookEvent": "jira:issue_updated",
			"issue": map[string]any{"id": "10001", "key": "APP-1", "self": "https://approved.atlassian.net/rest/api/3/issue/10001",
				"fields": map[string]any{"project": map[string]any{"key": "APP"}, "labels": []any{}}},
			"changelog": map[string]any{"items": []any{}}}
	default:
		return map[string]any{"eventType": "workitem.updated", "resourceVersion": "1.0",
			"resource": map[string]any{"id": 999, "workItemId": 123, "fields": map[string]any{}},
			"resourceContainers": map[string]any{"collection": map[string]any{"id": "11111111-1111-4111-8111-111111111111"},
				"project": map[string]any{"id": "22222222-2222-4222-8222-222222222222"}}}
	}
}

func TestConnectorLifecycleParity(t *testing.T) {
	for _, provider := range []string{"linear", "jira", "azure-devops"} {
		t.Run(provider, func(t *testing.T) {
			connector := lifecycleConnector(t, provider)
			var names []string
			for name := range connector.Registration.Spec.Triggers {
				names = append(names, name)
			}
			slices.Sort(names)
			expected := 7
			if provider == "azure-devops" {
				expected = 8
			}
			require.Len(t, names, expected)
			plan, err := connector.Service.Select(connectors.Selection{Triggers: names})
			require.NoError(t, err)
			for _, name := range names {
				t.Run(name, func(t *testing.T) {
					trigger := connector.Registration.Spec.Triggers[name]
					payload := lifecyclePayload(provider)
					switch provider {
					case "linear":
						payload["action"] = trigger.Match["/action"]
						current, before := payload["data"].(map[string]any), payload["updatedFrom"].(map[string]any)
						switch name {
						case "issue-closed":
							current["completedAt"], before["completedAt"] = "2026-11-02T23:00:00Z", nil
						case "issue-reopened":
							before["completedAt"] = "2026-11-02T23:00:00Z"
						case "issue-labeled":
							current["labelIds"], before["labelIds"] = []any{"bug"}, []any{}
						case "issue-unlabeled":
							before["labelIds"] = []any{"bug"}
						}
						if name == "issue-unlabeled" {
							current["labelIds"] = []any{}
						}
					case "jira":
						payload["webhookEvent"] = trigger.NativeEvents[0]
						fields := payload["issue"].(map[string]any)["fields"].(map[string]any)
						change := map[string]any{"fieldtype": "jira"}
						switch name {
						case "issue-closed":
							change["field"], change["from"], change["to"] = "resolution", nil, "1"
						case "issue-reopened":
							change["field"], change["from"], change["to"] = "resolution", "1", nil
						case "issue-labeled":
							change["field"], change["fromString"], change["toString"], fields["labels"] = "labels", nil, "bug", []any{"bug"}
						case "issue-unlabeled":
							change["field"], change["fromString"], change["toString"] = "labels", "bug", nil
						}
						payload["changelog"] = map[string]any{"items": []any{change}}
					case "azure-devops":
						payload["eventType"] = trigger.NativeEvents[0]
						resource := payload["resource"].(map[string]any)
						fields := resource["fields"].(map[string]any)
						switch name {
						case "work-item-closed":
							fields["System.State"] = map[string]any{"oldValue": "Active", "newValue": "Done"}
						case "work-item-reopened":
							fields["System.State"] = map[string]any{"oldValue": "Done", "newValue": "Active"}
						case "work-item-labeled":
							fields["System.Tags"] = map[string]any{"oldValue": "", "newValue": "bug"}
						case "work-item-unlabeled":
							fields["System.Tags"] = map[string]any{"oldValue": "bug", "newValue": ""}
						}
						if name != "work-item-updated" && trigger.When == nil {
							resource["id"] = 123
						}
					}
					data, err := json.Marshal(payload)
					require.NoError(t, err)
					result, err := plan.ProjectEvent(name, trigger.NativeEvents[0], data)
					require.NoError(t, err)
					require.True(t, result.Untrusted)
					require.False(t, result.Operational)
					require.NotContains(t, result.Fields, "body")
					if trigger.When != nil {
						checkLifecycleNodePredicate(t, *trigger.When, payload)
						switch provider {
						case "linear":
							delete(payload, "updatedFrom")
						case "jira":
							delete(payload, "changelog")
						case "azure-devops":
							payload["resource"].(map[string]any)["fields"] = map[string]any{}
						}
						data, err = json.Marshal(payload)
						require.NoError(t, err)
						_, err = plan.ProjectEvent(name, trigger.NativeEvents[0], data)
						require.Error(t, err, "missing change evidence must not infer a transition")
					}
				})
			}
			require.ErrorIs(t, plan.ActivateTriggers(), connectors.ErrIngressUnavailable)
		})
	}
}

func checkLifecycleNodePredicate(t *testing.T, rule connectors.EventPredicate, payload any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"rule": rule, "payload": payload})
	require.NoError(t, err)
	module, err := filepath.Abs("../../actions/setup/js/event_predicate.cjs")
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "node", "-e", `
const assert = require("node:assert/strict");
const input = JSON.parse(require("node:fs").readFileSync(0,"utf8"));
const result = require(process.argv[1]).evaluateEventPredicate(input.rule,input.payload);
assert.deepEqual(result,{matched:true,known:true});
`, module)
	command.Stdin = strings.NewReader(string(data))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestConnectorLifecyclePayloadVariants(t *testing.T) {
	connector := lifecycleConnector(t, "azure-devops")
	plan, err := connector.Service.Select(connectors.Selection{Triggers: []string{"work-item-updated"}})
	require.NoError(t, err)
	payload := lifecyclePayload("azure-devops")
	resource := payload["resource"].(map[string]any)
	delete(resource, "workItemId")
	resource["id"] = 123
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	result, err := plan.ProjectEvent("work-item-updated", "workitem.updated", data)
	require.NoError(t, err)
	require.Equal(t, json.Number("123"), result.Fields["subject"])
	for _, mutate := range []func(){
		func() { payload["resourceVersion"] = "9.0" },
		func() { payload["resourceVersion"] = "1.0"; resource["revision"] = map[string]any{} },
		func() { delete(resource, "revision"); resource["id"] = "123" },
	} {
		mutate()
		data, err = json.Marshal(payload)
		require.NoError(t, err)
		_, err = plan.ProjectEvent("work-item-updated", "workitem.updated", data)
		require.Error(t, err)
	}
}

func TestConnectorLifecycleFiltersFrozenThroughGateway(t *testing.T) {
	on := "on:\n  gateway-dispatch:\n    tracker:\n      issue-created:\n        sender: [approved]\n"
	compiler, root := awConnectorCompiler(t)
	source := connectorGatewayWorkflow(t, on)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(strings.SplitN(source, "---", 3)[1]), &document))
	trigger := document["connectors"].(map[string]any)["tracker"].(map[string]any)["triggers"].(map[string]any)["issue-created"].(map[string]any)
	trigger["filters"] = map[string]any{"sender": map[string]any{"path": "/actor/id", "type": "string"}}
	content, err := yaml.Marshal(document)
	require.NoError(t, err)
	compiler.repoConfig, compiler.repoConfigLoaded = &RepoConfig{GatewayDispatch: true}, true
	data, err := compiler.ParseWorkflowString("---\n"+string(content)+"---\nUse references only.\n", filepath.Join(root, "filtered.md"))
	require.NoError(t, err)
	require.Len(t, data.WebhookSubscriptions, 1)
	subscription := data.WebhookSubscriptions[0]
	require.Equal(t, connectors.EventEvaluationVersion, subscription.Connector.EvaluationVersion)
	require.Equal(t, []any{"approved"}, subscription.Connector.Filters["sender"].Values)
	found := false
	for _, condition := range subscription.Conditions {
		if condition.Filter == "connector_filter_sender" {
			require.Equal(t, []any{"approved"}, condition.Values)
			found = true
		}
	}
	require.True(t, found)
	for _, filters := range []map[string]any{{"sender": []any{}}, {"unknown": []any{"approved"}}, {"sender": []any{1}}} {
		document["on"].(map[string]any)["gateway-dispatch"].(map[string]any)["tracker"] = map[string]any{"issue-created": filters}
		bad, encodeErr := yaml.Marshal(document)
		require.NoError(t, encodeErr)
		_, err = compiler.ParseWorkflowString("---\n"+string(bad)+"---\nNo widening.\n", filepath.Join(root, "invalid.md"))
		require.Error(t, err)
	}
}
