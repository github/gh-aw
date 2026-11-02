//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedConnectorImports(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"linear", "jira", "azure-devops", "slack", "teams", "confluence", "sentry", "servicenow"} {
		t.Run(provider, func(t *testing.T) {
			root := t.TempDir()
			workflows := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(workflows, 0700))
			c := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
			c.gitRoot = root
			c.SetActionMode(ActionModeDev)
			c.version = "v0.0.0-1-g683ddf9617"
			for _, fixture := range []string{"workflow.md", "workflow-ingress-unavailable.md"} {
				source, err := os.ReadFile(filepath.Join("../connectors/testdata/providers", provider, fixture))
				require.NoError(t, err)
				content := strings.ReplaceAll(string(source), ".github/aw/connectors/"+provider+".md", `"@builtin:connectors/`+provider+`.md"`)
				file := filepath.Join(workflows, fixture)
				require.NoError(t, os.WriteFile(file, []byte(content), 0600))
				data, err := c.ParseWorkflowFile(file)
				if fixture == "workflow-ingress-unavailable.md" {
					require.ErrorContains(t, err, "ingress is not operational")
					continue
				}
				require.NoError(t, err)
				require.NotNil(t, data.Connectors[provider])
				assert.Nil(t, data.Connectors[provider].Context)
				_, err = c.CompileToYAML(data, file)
				require.NoError(t, err)
			}
			_, err := os.Stat(filepath.Join(root, ".github", "aw"))
			require.True(t, os.IsNotExist(err), "embedded imports must not require or materialize checkout files")
		})
	}
}

func TestEmbeddedConnectorImportWithInputsAndNoImplicitGrants(t *testing.T) {
	t.Parallel()
	for _, selection := range []string{"", "tools:\n  jira: [personal]\n"} {
		root := t.TempDir()
		c := NewCompiler(WithNoEmit(true), WithSkipValidation(true))
		c.gitRoot = root
		workflows := filepath.Join(root, ".github", "workflows")
		require.NoError(t, os.MkdirAll(workflows, 0700))
		source := awConnectorHeader + `imports:
  - uses: "@builtin:connectors/jira.md"
    with:
      mcp-personal-secret: APPROVED_READ_TOKEN
      cloud-ids: [00000000-0000-4000-8000-000000000001]
      issues: [APP-1]
      queries: [project = APP ORDER BY created DESC]
` + selection + "---\nRead only approved evidence.\n"
		file := filepath.Join(workflows, "proof.md")
		require.NoError(t, os.WriteFile(file, []byte(source), 0600))
		data, err := c.ParseWorkflowFile(file)
		require.NoError(t, err)
		if selection == "" {
			assert.Empty(t, data.Connectors)
			continue
		}
		require.NotNil(t, data.Connectors["jira"].Agent)
		assert.Equal(t, []string{"APPROVED_READ_TOKEN"}, data.Connectors["jira"].Agent.Secrets())
		assert.Nil(t, data.Connectors["jira"].SafeOutputs)
	}
}

func TestEmbeddedConnectorResolutionIsExact(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing.md", "../engines/copilot.md", "jira.md/../linear.md"} {
		_, err := parser.ResolveIncludePath("@builtin:connectors/"+name, t.TempDir(), nil)
		require.ErrorContains(t, err, "builtin file not found")
	}
}
