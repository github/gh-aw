//go:build !integration

package workflow

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

type azureDevOpsDocumentation struct {
	XMLName     xml.Name
	ID          string `xml:"id,attr"`
	Version     string `xml:"version,attr"`
	Purpose     string `xml:"purpose"`
	Credentials struct {
		Setup string `xml:"setup"`
		Items []struct {
			Text string `xml:",chardata"`
		} `xml:"credential"`
		Routes string `xml:"routes"`
	} `xml:"credentials"`
	Configuration struct {
		Inputs []struct {
			Name    string `xml:"name,attr"`
			Default string `xml:"default,attr"`
		} `xml:"inputs>input"`
		Resources string `xml:"resources"`
		Selection string `xml:"selection"`
	} `xml:"configuration"`
	Triggers struct {
		Operational string `xml:"operational,attr"`
		Items       []struct {
			ID      string `xml:"id,attr"`
			Event   string `xml:"event,attr"`
			Subject string `xml:"subject,attr"`
		} `xml:"trigger"`
		Boundary string `xml:"boundary"`
	} `xml:"triggers"`
	Effects struct {
		Common string `xml:"common"`
		Items  []struct {
			ID     string `xml:"id,attr"`
			Tool   string `xml:"tool,attr"`
			Inputs string `xml:"inputs,attr"`
			Text   string `xml:",chardata"`
		} `xml:"effect"`
		Responses string `xml:"responses"`
	} `xml:"effects"`
	Context     string `xml:"context"`
	Limitations string `xml:"limitations"`
	Example     struct {
		Path         string `xml:"path,attr"`
		Format       string `xml:"format,attr"`
		Instructions string `xml:"instructions"`
		YAML         string `xml:"frontmatter"`
		Prompt       string `xml:"body"`
	} `xml:"example"`
	Sources []struct {
		Href string `xml:"href,attr"`
	} `xml:"sources>source"`
}

func loadAzureDevOpsDocumentation(t *testing.T) (azureDevOpsDocumentation, map[string]any) {
	t.Helper()
	content, err := os.ReadFile("../../.github/workflows/connectors/azure-devops.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(content), "---", 3)
	require.Len(t, parts, 3)
	body := strings.TrimSpace(parts[2])
	require.Equal(t, 1, strings.Count(body, "<!--"))
	require.Equal(t, 1, strings.Count(body, "-->"))
	require.True(t, strings.HasPrefix(body, "<!--\n"))
	require.True(t, strings.HasSuffix(body, "\n-->"))
	document := strings.TrimSuffix(strings.TrimPrefix(body, "<!--\n"), "\n-->")
	require.NotContains(t, document, "--", "even CDATA must be safe inside the HTML comment")
	var docs azureDevOpsDocumentation
	decoder := xml.NewDecoder(strings.NewReader(document))
	require.NoError(t, decoder.Decode(&docs), "documentation must be well-formed XML")
	_, err = decoder.Token()
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, "connector-documentation", docs.XMLName.Local)
	require.Equal(t, "azure-devops", docs.ID)
	require.Equal(t, "1", docs.Version)
	var frontmatter map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(parts[1]), &frontmatter))
	return docs, frontmatter
}

func TestAzureDevOpsConnectorDocumentationContract(t *testing.T) {
	t.Parallel()
	docs, frontmatter := loadAzureDevOpsDocumentation(t)
	for _, section := range []string{
		docs.Purpose, docs.Credentials.Setup, docs.Credentials.Routes,
		docs.Configuration.Resources, docs.Configuration.Selection,
		docs.Triggers.Boundary, docs.Effects.Common, docs.Effects.Responses,
		docs.Context, docs.Limitations, docs.Example.Instructions, docs.Example.Prompt,
	} {
		require.NotEmpty(t, strings.TrimSpace(section))
	}
	schema := frontmatter["import-schema"].(map[string]any)
	require.Len(t, docs.Configuration.Inputs, len(schema))
	seen := map[string]bool{}
	for _, input := range docs.Configuration.Inputs {
		require.False(t, seen[input.Name], "duplicate input documentation")
		seen[input.Name] = true
		require.Contains(t, schema, input.Name)
		require.Equal(t, schema[input.Name].(map[string]any)["default"], input.Default)
	}
	require.Len(t, docs.Credentials.Items, 2)
	require.Contains(t, docs.Credentials.Items[0].Text, "2a72489c-aab2-4b65-b93a-a91edccf33b8")
	require.Contains(t, docs.Credentials.Items[1].Text, "499b84ac-1321-427f-aa17-267ca6975798")
	require.Contains(t, docs.Credentials.Items[1].Text, "https://app.vssps.visualstudio.com/.default")
	require.NotEmpty(t, docs.Sources)
	for _, source := range docs.Sources {
		require.True(t, strings.HasPrefix(source.Href, "https://learn.microsoft.com/"))
	}
	require.Equal(t, "false", docs.Triggers.Operational)
	require.Len(t, docs.Triggers.Items, 4)
	for _, trigger := range docs.Triggers.Items {
		name := strings.TrimPrefix(trigger.ID, "azure-devops-")
		subject := "/resource/id"
		if name == "work-item-updated" {
			subject = "/resource/workItemId"
		}
		require.Equal(t, subject, trigger.Subject)
		require.Equal(t, "workitem."+strings.TrimPrefix(name, "work-item-"), trigger.Event)
	}
}

func TestAzureDevOpsConnectorDocumentationExample(t *testing.T) {
	t.Parallel()
	docs, _ := loadAzureDevOpsDocumentation(t)
	require.Equal(t, "yaml-frontmatter", docs.Example.Format)
	require.Equal(t, ".github/workflows/azure-devops-review.md", docs.Example.Path)
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(docs.Example.Path))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	content := "---\n" + strings.TrimSpace(docs.Example.YAML) + "\n---\n" + strings.TrimSpace(docs.Example.Prompt) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	compiler := NewCompiler()
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeRelease)
	compiler.SetActionTag("72c8a3ff80e4a7549a83bb82c8ff5a97b879e92d")
	compiler.SetStrictMode(true)
	compiler.version = "v0.0.0-1-g683ddf9617"
	require.NoError(t, compiler.CompileWorkflow(path))
	output, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
	require.NoError(t, err)
	require.Contains(t, string(output), "workflow_dispatch:")
	require.Contains(t, string(output), "staged")
	require.Contains(t, string(output), "github/gh-aw/actions/setup@72c8a3ff80e4a7549a83bb82c8ff5a97b879e92d")
	require.NotContains(t, string(output), "uses: ./actions/")
	require.Contains(t, string(output), "connector_runtime.cjs")
	require.NotContains(t, string(output), "/tmp/gh-aw/connector-bin/")
	require.NotContains(t, string(output), "/tmp/gh-aw/connector-source/")
	require.NotContains(t, string(output), "go build")
	require.NotContains(t, string(output), "source-ref")
	require.NotContains(t, string(output), "connector_12_azure_devops_")
	require.NotContains(t, string(output), "${{ secrets.AZURE_DEVOPS_WRITE_TOKEN }}")
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.Nil(t, data.Connectors["azure-devops"].Context)
	require.Equal(t, []string{"azure-devops__core_list_projects"}, data.Connectors["azure-devops"].Agent.ToolNames())
	require.Len(t, docs.Effects.Items, 5)
	names := data.Connectors["azure-devops"].SafeOutputs.SafeOutputNames()
	var manifest map[string]string
	require.NoError(t, json.Unmarshal([]byte(buildCustomSafeOutputScriptsJSON(data)), &manifest))
	require.Len(t, names, len(docs.Effects.Items))
	for _, effect := range docs.Effects.Items {
		require.Contains(t, names, strings.TrimPrefix(effect.ID, "azure-devops-"))
		require.Equal(t, strings.ReplaceAll(effect.ID, "-", "_"), effect.Tool)
		require.Equal(t, "safe_output_script_"+effect.Tool+".cjs", manifest[effect.Tool])
		require.NotEmpty(t, effect.Inputs)
		require.NotEmpty(t, strings.TrimSpace(effect.Text))
	}
	_, err = os.Stat(filepath.Join(root, ".github", "workflows", "connectors"))
	require.NoError(t, err, "normal imports read the local provider file")
}
