package connectors

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitLabProviderDeclarationAndDocumentation(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/connectors/gitlab.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var document map[string]any
	require.NoError(t, Decode([]byte(parts[1]), &document))
	require.ElementsMatch(t, []string{"connectors", "import-schema"}, gitLabKeys(document))
	declaration := document["connectors"].(map[string]any)["gitlab"].(map[string]any)
	require.ElementsMatch(t, []string{"experimental", "docs", "endpoints", "credentials", "resources", "triggers", "tools", "safe-outputs"}, gitLabKeys(declaration))
	require.ElementsMatch(t, []string{"get-merge-request", "get-pipeline"}, gitLabKeys(declaration["tools"].(map[string]any)))
	require.ElementsMatch(t, []string{"add-merge-request-note"}, gitLabKeys(declaration["safe-outputs"].(map[string]any)))
	schema := document["import-schema"].(map[string]any)
	for _, resource := range []string{"projects", "merge-requests", "pipelines"} {
		require.Empty(t, schema[resource].(map[string]any)["default"], "imports must not grant sample resources")
	}

	body := strings.TrimSpace(parts[2])
	require.NotContains(t, body, "<!--", "documentation must not be hidden in an HTML comment")
	require.NotContains(t, body, "-->")
	require.True(t, strings.HasPrefix(body, "<connector-documentation "))
	require.True(t, strings.HasSuffix(body, "</connector-documentation>"))
	var documentation struct {
		XMLName  xml.Name `xml:"connector-documentation"`
		ID       string   `xml:"id,attr"`
		Version  string   `xml:"version,attr"`
		Examples []struct {
			ID     string `xml:"id,attr"`
			YAML   string `xml:"frontmatter"`
			Prompt string `xml:"body"`
		} `xml:"examples>example"`
	}
	require.NoError(t, xml.Unmarshal([]byte(body), &documentation))
	require.Equal(t, "gitlab", documentation.ID)
	require.Equal(t, "1", documentation.Version)
	for _, section := range []string{"purpose", "authentication", "configuration", "operations", "triggers", "privacy", "limitations", "examples", "sources"} {
		require.Contains(t, body, "<"+section)
	}
	require.Len(t, documentation.Examples, 2)
	for _, example := range documentation.Examples {
		var frontmatter map[string]any
		require.NoError(t, Decode([]byte(example.YAML), &frontmatter))
		require.Equal(t, "workflow_dispatch", frontmatter["on"])
		require.NotEmpty(t, example.Prompt)
		if example.ID == "staged-note" {
			require.Equal(t, true, frontmatter["safe-outputs"].(map[string]any)["staged"])
		}
	}
}

func gitLabKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestGitLabProviderNativeEventEvidenceDoesNotEnrollIngress(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/connectors/gitlab.md")
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, Decode([]byte(strings.SplitN(string(source), "---", 3)[1]), &document))
	declaration := document["connectors"].(map[string]any)["gitlab"].(map[string]any)
	encoded, err := json.Marshal(declaration["triggers"])
	require.NoError(t, err)
	var triggers map[string]Trigger
	require.NoError(t, Decode([]byte(strings.ReplaceAll(string(encoded), "${{ github.aw.import-inputs.api-origin }}", "https://gitlab.com")), &triggers))
	service, err := NewService(ConnectorSpec{
		APIVersion: APIVersion, Kind: "ConnectorSpec", ID: "aw.github.com/gitlab",
		Endpoints: map[string]string{"api": "https://gitlab.com"}, Triggers: triggers,
	}, Connection{
		APIVersion: APIVersion, Kind: "ConnectorConnection", Connector: "aw.github.com/gitlab",
		Resources: map[string][]string{"projects": {"42"}, "merge-requests": {"7"}, "pipelines": {"900"}},
	})
	require.NoError(t, err)
	plan, err := service.Select(Selection{Triggers: []string{"merge-request-opened", "pipeline-updated"}})
	require.NoError(t, err)
	payload, err := os.ReadFile("testdata/providers/gitlab/native-events.json")
	require.NoError(t, err)
	var events map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload, &events))
	for name, event := range events {
		native := "Merge Request Hook"
		if name == "pipeline-updated" {
			native = "Pipeline Hook"
		}
		projection, err := plan.ProjectEvent(name, native, event)
		require.NoError(t, err)
		require.True(t, projection.Untrusted)
		require.False(t, projection.Operational)
		require.Equal(t, json.Number("42"), projection.Fields["project"])
		for _, invalid := range []string{
			strings.ReplaceAll(string(event), `"id": 42`, `"id": 43`),
			strings.ReplaceAll(strings.ReplaceAll(string(event), `"iid": 7`, `"iid": 8`), `"id": 900`, `"id": 901`),
		} {
			_, err := plan.ProjectEvent(name, native, []byte(invalid))
			require.Error(t, err)
		}
		_, err = plan.ProjectEvent(name, "unverified-event", event)
		require.Error(t, err)
	}
	_, err = plan.ProjectEvent("merge-request-opened", "Merge Request Hook",
		[]byte(strings.ReplaceAll(string(events["merge-request-opened"]), `"target_project_id": 42`, `"target_project_id": 43`)))
	require.Error(t, err)
	require.ErrorIs(t, plan.ActivateTriggers(), ErrIngressUnavailable)
}
