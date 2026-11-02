//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const azureDevOpsConsumerImport = `imports:
  - uses: "@builtin:connectors/azure-devops.md"
    with:
      organization: contoso
      project: 33333333-3333-4333-8333-333333333333
      project-name: Approved Project
      work-item: "9876"
      related-work-item: "9877"
      assignee: approved@example.invalid
      agent-reader-secret: APPROVED_AZURE_READER
      writer-secret: APPROVED_AZURE_WRITER
`

func parseAzureDevOpsConsumer(t *testing.T, selection string) (*Compiler, *WorkflowData, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "azure.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(awConnectorHeader+azureDevOpsConsumerImport+selection+"---\nUse only explicitly approved capabilities.\n"), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
	compiler.gitRoot = root
	compiler.SetActionMode(ActionModeDev)
	compiler.version = "v0.0.0-1-g683ddf9617"
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, ".github", "aw"))
	require.True(t, os.IsNotExist(err), "the compiler must not require or materialize provider files")
	return compiler, data, path
}

func TestAzureDevOpsConnectorIndependentPhases(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, selection string
		read, write     bool
	}{
		{"registration", "", false, false},
		{"reader", "tools:\n  azure-devops: [azure-devops]\n", true, false},
		{"writer", "safe-outputs:\n  azure-devops-comment-on-work-item:\n", false, true},
		{"both", "tools:\n  azure-devops: [azure-devops]\nsafe-outputs:\n  azure-devops-comment-on-work-item:\n", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			compiler, data, path := parseAzureDevOpsConsumer(t, test.selection)
			if !test.read && !test.write {
				assert.Empty(t, data.Connectors)
			} else {
				config := data.Connectors["azure-devops"]
				require.NotNil(t, config)
				assert.Equal(t, test.read, config.Agent != nil)
				assert.Equal(t, test.write, config.SafeOutputs != nil)
				assert.Nil(t, config.Context)
				if test.read {
					assert.Equal(t, []string{"APPROVED_AZURE_READER"}, config.Agent.Secrets())
					assert.Equal(t, []string{"azure-devops__core_list_projects"}, config.Agent.ToolNames())
				}
				if test.write {
					assert.Equal(t, []string{"APPROVED_AZURE_WRITER"}, config.SafeOutputs.Secrets())
					assert.Equal(t, []string{"comment-on-work-item"}, config.SafeOutputs.SafeOutputNames())
				}
			}
			output, err := compiler.CompileToYAML(data, path)
			require.NoError(t, err)
			assert.Equal(t, test.read, strings.Contains(output, "${{ secrets.APPROVED_AZURE_READER }}"))
			assert.Equal(t, test.write, strings.Contains(output, "${{ secrets.APPROVED_AZURE_WRITER }}"))
			for _, legacy := range []string{"AZURE_DEVOPS_EXT_PAT", "SYSTEM_ACCESSTOKEN", "ADO_MCP_AUTH_TOKEN", "ado_create_work_item"} {
				assert.NotContains(t, output, legacy)
			}
		})
	}
}

func TestAzureDevOpsConnectorRejectsLegacyAndUnsupportedCapabilities(t *testing.T) {
	t.Parallel()
	for _, selection := range []string{
		"safe-outputs:\n  ado-create-work-item:\n",
		"safe-outputs:\n  ado-update-work-item:\n    title: true\n",
		"safe-outputs:\n  ado-comment-on-work-item:\n",
		"safe-outputs:\n  ado-assign-work-item:\n",
		"safe-outputs:\n  ado-link-work-items:\n",
		"safe-outputs:\n  ado-upload-workitem-attachment:\n",
		"safe-outputs:\n  azure-devops-upload-workitem-attachment:\n",
		"safe-outputs:\n  azure-devops-comment-on-work-item: {target: '*'}\n",
		"tools:\n  azure-devops: [wit_get_work_item]\n",
		"context:\n  azure-devops: {work-item-details: {work-item: '9876'}}\n",
	} {
		t.Run(strings.ReplaceAll(selection, "\n", " "), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".github", "workflows", "invalid.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			require.NoError(t, os.WriteFile(path, []byte(awConnectorHeader+azureDevOpsConsumerImport+selection+"---\nNo fallback.\n"), 0600))
			compiler := NewCompiler(WithNoEmit(true))
			compiler.gitRoot = root
			_, err := compiler.ParseWorkflowFile(path)
			require.Error(t, err)
		})
	}
	for _, trigger := range []string{
		"on:\n  gateway-dispatch:\n    azure-devops-issue:\n      project_id: approved\n",
		"on:\n  azure-devops-work-item-created:\n",
		"on: azure-devops-work-item-updated\n",
		"on: [workflow_dispatch, azure-devops-work-item-deleted]\n",
	} {
		t.Run(trigger, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".github", "workflows", "ingress.md")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			header := strings.Replace(awConnectorHeader, "on: workflow_dispatch\n", trigger, 1)
			require.NoError(t, os.WriteFile(path, []byte(header+azureDevOpsConsumerImport+"---\nDo not activate.\n"), 0600))
			compiler := NewCompiler(WithNoEmit(true))
			compiler.gitRoot = root
			_, err := compiler.ParseWorkflowFile(path)
			require.Error(t, err)
			if !strings.Contains(trigger, "gateway-dispatch") {
				assert.ErrorContains(t, err, "ingress is not operational")
			}
		})
	}
	for _, key := range []string{
		"ado-create-work-item", "ado-update-work-item", "ado-comment-on-work-item",
		"ado-assign-work-item", "ado-link-work-items", "ado-upload-workitem-attachment",
	} {
		_, registered := safeOutputHandlersByKey[key]
		assert.False(t, registered, "legacy provider output must not remain registered: %s", key)
	}
	catalog, err := WebhookCatalog()
	require.NoError(t, err)
	assert.NotContains(t, catalog, "azure_devops_issue")
}
