//go:build !integration

package workflow

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAzureWorkItemParityDocumentation(t *testing.T) {
	docs, declaration := loadAzureDevOpsDocumentation(t)
	assert.Len(t, docs.Credentials.Items, 5)
	schema := declaration["import-schema"].(map[string]any)
	assert.NotEmpty(t, docs.Configuration.Inputs)
	for _, input := range docs.Configuration.Inputs {
		value := schema[input.Name].(map[string]any)["default"]
		if text, ok := value.(string); ok {
			assert.Equal(t, text, input.Default)
		} else {
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			assert.Equal(t, string(encoded), input.Default)
		}
	}
	plan := azureWorkItemParityPlan(t)
	seen := map[string]bool{}
	for _, effect := range docs.Effects.Items {
		operation := strings.TrimPrefix(effect.ID, "azure-devops-")
		seen[operation] = true
		assert.NotEmpty(t, strings.TrimSpace(effect.Text))
		assert.Equal(t, strings.ReplaceAll(effect.ID, "-", "_"), effect.Tool)
	}
	for _, operation := range plan.SafeOutputNames() {
		assert.True(t, seen[operation], "every new operation has an XML contract: %s", operation)
		inputs, err := plan.Inputs("safe-output", operation)
		require.NoError(t, err)
		for name, input := range inputs {
			assert.Contains(t, []string{"string", "integer", "number", "boolean"}, input.Type, "%s.%s", operation, name)
		}
	}
}

func TestAzureWorkItemParityDocumentationExample(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/connectors/azure-devops.md")
	require.NoError(t, err)
	parts := strings.SplitN(string(source), "---", 3)
	require.Len(t, parts, 3)
	var document struct {
		Example struct {
			YAML   string `xml:"frontmatter"`
			Prompt string `xml:"body"`
		} `xml:"rich-example"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(parts[2]), "<!--\n"), "\n-->")
	require.NoError(t, xml.Unmarshal([]byte(body), &document))
	require.NotEmpty(t, document.Example.YAML)
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "azure-rich-example.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	content := "---\n" + strings.TrimSpace(document.Example.YAML) + "\n---\n" + document.Example.Prompt
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	compiler.gitRoot = root
	installConnectorTestSources(t, root)
	compiler.SetActionMode(ActionModeDev)
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	plan := data.Connectors["azure-devops"]
	assert.Equal(t, []string{"AZURE_DEVOPS_READ_TOKEN"}, plan.Agent.Secrets())
	assert.Equal(t, []string{"get_work_item_description"}, plan.Agent.ToolNames())
	assert.Len(t, plan.SafeOutputs.SafeOutputNames(), 8)
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	assert.Contains(t, output, "${{ secrets.AZURE_DEVOPS_READ_TOKEN }}")
	assert.NotContains(t, output, "${{ secrets.AZURE_DEVOPS_WRITE_TOKEN }}", "staged example does not deliver a writer credential")
}
