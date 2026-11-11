//go:build !integration

package workflow

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func TestConnectorTargetedMCPProfiles(t *testing.T) {
	for _, profile := range []string{"personal", "service-account"} {
		t.Run(profile, func(t *testing.T) {
			imports := strings.Replace(jiraEmbeddedImport, "      queries: [project = APP ORDER BY created DESC]\n", "", 1)
			compiler, _, path, err := parseJiraMigrationWorkflow(t, imports+"tools:\n  jira:\n    "+profile+": [getJiraIssue]\n")
			require.NoError(t, err)
			compiler.SetSkipValidation(false)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err, "targeted MCP selections must pass normal structural validation")
			plan := data.Connectors["jira"].Agent
			require.Equal(t, []string{profile + "__getJiraIssue"}, plan.ToolNames())
			encoded, err := plan.Encode()
			require.NoError(t, err)
			raw, err := base64.StdEncoding.DecodeString(encoded)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "searchJiraIssuesUsingJql")
			require.NotContains(t, string(raw), `"queries"`)
			checkMinimalConnectorAdvertisement(t, encoded, []string{profile + "__getJiraIssue"})
			_, err = plan.CallMCP(t.Context(), connectors.Runtime{
				Phase: "agent", AllowedOrigins: plan.Origins(),
				ResolveSecret: func(context.Context, string, string) (string, error) {
					t.Fatal("unselected method resolved a credential")
					return "", nil
				},
			}, profile, "searchJiraIssuesUsingJql", map[string]any{})
			require.ErrorContains(t, err, "exact allowlist")
			_, err = plan.CallMCP(t.Context(), connectors.Runtime{
				Phase: "agent", AllowedOrigins: plan.Origins(),
				ResolveSecret: func(context.Context, string, string) (string, error) {
					t.Fatal("unapproved issue resolved a credential")
					return "", nil
				},
			}, profile, "getJiraIssue", map[string]any{"cloudId": "00000000-0000-4000-8000-000000000001", "issueIdOrKey": "OTHER-1"})
			require.Error(t, err, "narrowing must retain the declaration's input and resource constraints")
		})
	}
}

func TestConnectorTargetedMCPRejectsWidening(t *testing.T) {
	for _, selection := range []string{
		"{}",
		"{personal: []}",
		"{personal: ['*']}",
		"{personal: [writeJiraIssue]}",
		"{personal: [getJiraIssue, getJiraIssue]}",
		"{personal: [1]}",
		"{personal: getJiraIssue}",
		"{not-registered: [getJiraIssue]}",
		"{get-visible-projects: [getJiraIssue]}",
	} {
		t.Run(selection, func(t *testing.T) {
			_, _, _, err := parseJiraMigrationWorkflow(t, jiraEmbeddedImport+"tools:\n  jira: "+selection+"\n")
			require.Error(t, err)
		})
	}
}
