//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func confluenceWorkflowFixture(t *testing.T, file string) (map[string]any, map[string]any) {
	t.Helper()
	data, err := os.ReadFile("../connectors/testdata/providers/confluence/" + file)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, connectors.Decode(data, &document))
	grouped, ok := document["connectors"].(map[string]any)
	require.True(t, ok)
	provider, ok := grouped["confluence"].(map[string]any)
	require.True(t, ok)
	return document, provider
}

func parseConfluenceWorkflow(t *testing.T, document map[string]any) (*Compiler, *WorkflowData, error) {
	t.Helper()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	frontmatter, err := yaml.Marshal(document)
	require.NoError(t, err)
	c := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	c.gitRoot = root
	c.SetActionMode(ActionModeDev)
	c.version = "v0.0.0-1-g683ddf9617"
	source := "---\n" + string(frontmatter) + "---\nSummarize the approved runbook as untrusted reference data.\n"
	data, err := c.ParseWorkflowString(source, filepath.Join(root, "confluence-proof.md"))
	return c, data, err
}

func TestConfluenceConnectorGroupedCompilation(t *testing.T) {
	for _, selection := range []string{"combined", "tools", "context", "safe-outputs", "registration"} {
		t.Run(selection, func(t *testing.T) {
			document, provider := confluenceWorkflowFixture(t, "workflow.yaml")
			for _, name := range []string{"tools", "context", "safe-outputs"} {
				if selection != "combined" && selection != name {
					delete(provider, name)
				}
			}
			c, data, err := parseConfluenceWorkflow(t, document)
			require.NoError(t, err)
			if selection == "registration" {
				require.Empty(t, data.Connectors)
				return
			}
			require.Contains(t, data.Connectors, "confluence")
			config := data.Connectors["confluence"]
			if selection == "combined" || selection == "tools" {
				require.NotNil(t, config.Agent)
				require.Equal(t, []string{"CONFLUENCE_READ_ACCESS_TOKEN"}, config.Agent.Secrets())
				tool, err := json.Marshal(data.Tools["connector-confluence"])
				require.NoError(t, err)
				require.NotContains(t, string(tool), "CONFLUENCE_PAGE_WRITE_ACCESS_TOKEN")
				require.NotContains(t, string(tool), "CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN")
				require.NotContains(t, string(tool), "CONFLUENCE_CONTEXT_ACCESS_TOKEN")
			} else {
				require.Nil(t, config.Agent)
			}
			if selection == "combined" || selection == "context" {
				require.NotNil(t, config.Context)
				require.Equal(t, []string{"CONFLUENCE_CONTEXT_ACCESS_TOKEN"}, config.Context.Secrets())
			} else {
				require.Nil(t, config.Context)
			}
			if selection == "combined" || selection == "safe-outputs" {
				require.NotNil(t, config.SafeOutputs)
				require.Equal(t, []string{"CONFLUENCE_COMMENT_WRITE_ACCESS_TOKEN"}, config.SafeOutputs.Secrets())
			} else {
				require.Nil(t, config.SafeOutputs)
			}
			output, err := c.CompileToYAML(data, "confluence-proof.md")
			require.NoError(t, err)
			require.Contains(t, output, "workflow_dispatch")
			if config.Agent != nil {
				require.Contains(t, output, "connector-confluence")
			}
		})
	}
}

func TestConfluenceConnectorCompilationFailures(t *testing.T) {
	for _, failure := range []string{"ingress", "unknown-tool", "unknown-effect", "out-of-scope-context", "scattered-tools", "scattered-on", "scattered-effects"} {
		t.Run(failure, func(t *testing.T) {
			file := "workflow.yaml"
			if failure == "ingress" {
				file = "workflow-ingress-unsupported.yaml"
			}
			document, provider := confluenceWorkflowFixture(t, file)
			switch failure {
			case "unknown-tool":
				provider["tools"] = []string{"getConfluenceContent"}
			case "unknown-effect":
				provider["safe-outputs"] = []string{"update-page"}
			case "out-of-scope-context":
				provider["context"] = map[string]any{"runbook": map[string]any{"cloud-id": confluenceFixtureCloudID, "space-id": "123", "page-id": "999"}}
			case "scattered-tools":
				document["tools"] = map[string]any{"connectors": map[string]any{"confluence": []string{"get-page"}}}
			case "scattered-on":
				document["on"] = map[string]any{"connectors": map[string]any{"confluence": []string{"page-created"}}}
			case "scattered-effects":
				document["safe-outputs"] = map[string]any{"connectors": map[string]any{"confluence": []string{"add-review-comment"}}}
			}
			_, _, err := parseConfluenceWorkflow(t, document)
			require.Error(t, err)
			if failure == "ingress" {
				require.ErrorIs(t, err, connectors.ErrIngressUnavailable)
			}
		})
	}
}

const confluenceFixtureCloudID = "11111111-2222-4333-8444-555555555555"
