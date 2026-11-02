//go:build !integration

package workflow

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func TestConnectorLifecycleURLIdentity(t *testing.T) {
	connector := lifecycleConnector(t, "jira")
	plan, err := connector.Service.Select(connectors.Selection{Triggers: []string{"issue-updated"}})
	require.NoError(t, err)
	payload := lifecyclePayload("jira")
	issue := payload["issue"].(map[string]any)
	for _, reference := range []string{
		"https://approved.atlassian.net/rest/api/3/issue/10002",
		"https://approved.atlassian.net/other/10001",
		"https://other.atlassian.net/rest/api/3/issue/10001",
		"https://approved.atlassian.net/rest/api/3/issue/%31%30%30%30%31",
	} {
		issue["self"] = reference
		data, err := json.Marshal(payload)
		require.NoError(t, err)
		_, err = plan.ProjectEvent("issue-updated", "jira:issue_updated", data)
		require.Error(t, err)
	}
}

func TestConnectorLifecycleRequiresTerminalConfiguration(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	connector, err := LoadDevelopmentConnector(root, "azure-devops", "", nil, nil)
	require.NoError(t, err, "unused lifecycle rules may remain unconfigured")
	_, err = connector.Service.Select(connectors.Selection{Triggers: []string{"work-item-closed"}})
	require.ErrorContains(t, err, "closed-states")
	_, err = connector.Service.Select(connectors.Selection{Triggers: []string{"work-item-created"}})
	require.NoError(t, err)
}

func TestConnectorLifecycleFilterIsNotResourceAuthority(t *testing.T) {
	connector := lifecycleConnector(t, "jira")
	spec := *connector.Registration.Spec
	trigger := spec.Triggers["issue-updated"]
	filter := trigger.Filters["project"]
	filter.Values = []any{"OTHER"}
	trigger.Filters["project"] = filter
	spec.Triggers["issue-updated"] = trigger
	service, err := connectors.NewService(spec, connectors.Connection{
		APIVersion: connectors.APIVersion, Kind: "ConnectorConnection", Connector: spec.ID,
		Resources: map[string][]string{"projects": {"APP"}, "issue-ids": {"10001"}},
	})
	require.NoError(t, err)
	plan, err := service.Select(connectors.Selection{Triggers: []string{"issue-updated"}})
	require.NoError(t, err)
	payload := lifecyclePayload("jira")
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	_, err = plan.ProjectEvent("issue-updated", "jira:issue_updated", data)
	require.ErrorContains(t, err, "filter")
	payload["issue"].(map[string]any)["fields"].(map[string]any)["project"] = map[string]any{"key": "OTHER"}
	data, err = json.Marshal(payload)
	require.NoError(t, err)
	_, err = plan.ProjectEvent("issue-updated", "jira:issue_updated", data)
	require.ErrorContains(t, err, "outside approved resources")
}
