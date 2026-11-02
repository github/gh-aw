//go:build !integration

package workflow

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const awConnectorHeader = "---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\n"

func awConnectorSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../.github/aw/connectors/example.md")
	require.NoError(t, err)
	document, err := parser.ExtractFrontmatterFromContent(string(data))
	require.NoError(t, err)
	inputs, ok := document.Frontmatter["import-schema"].(map[string]any)
	require.True(t, ok)
	delete(document.Frontmatter, "import-schema")
	encoded, err := yaml.Marshal(document.Frontmatter)
	require.NoError(t, err)
	source := string(encoded)
	for name, value := range inputs {
		config, ok := value.(map[string]any)
		require.True(t, ok)
		defaultValue, ok := config["default"].(string)
		require.True(t, ok)
		source = strings.ReplaceAll(source, "${{ github.aw.import-inputs."+name+" }}", defaultValue)
	}
	return source
}

func awConnectorCompiler(t *testing.T) (*Compiler, string) {
	t.Helper()
	c, root := connectorCompiler(t)
	require.NoError(t, os.Remove(filepath.Join(root, "spec.yaml")))
	require.NoError(t, os.Remove(filepath.Join(root, "connection.yaml")))
	return c, root
}

func TestAWConnectorDirectAndImportedDeclarations(t *testing.T) {
	t.Parallel()
	for _, selection := range []struct {
		name, fields string
		agent, write bool
	}{
		{"registration", "", false, false},
		{"tools", "tools:\n  tracker: [get-issue]\n", true, false},
		{"effects", "safe-outputs:\n  tracker-add-comment:\n", false, true},
		{"combined", "tools:\n  tracker: [get-issue]\nsafe-outputs:\n  tracker-add-comment:\n  add-comment:\n", true, true},
	} {
		t.Run(selection.name, func(t *testing.T) {
			for _, imported := range []bool{false, true} {
				c, root := awConnectorCompiler(t)
				source := awConnectorSource(t)
				if imported {
					require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), []byte("---\n"+source+"---\n"), 0600))
					source = "imports:\n  - connector.md\n"
				}
				path := filepath.Join(root, "proof.md")
				content := awConnectorHeader + source + selection.fields + "---\nReview untrusted evidence.\n"
				data, err := c.ParseWorkflowString(content, path)
				require.NoError(t, err)
				if selection.name == "registration" {
					assert.Empty(t, data.Connectors)
					continue
				}
				config := data.Connectors["tracker"]
				require.NotNil(t, config)
				assert.Equal(t, selection.agent, config.Agent != nil)
				assert.Nil(t, config.Context)
				assert.Equal(t, selection.write, config.SafeOutputs != nil)
				output, err := c.CompileToYAML(data, path)
				require.NoError(t, err)
				assert.Equal(t, selection.agent, strings.Contains(output, "${{ secrets.EXAMPLE_READ_TOKEN }}"))
				assert.NotContains(t, output, "${{ secrets.EXAMPLE_CONTEXT_TOKEN }}")
				assert.Equal(t, selection.write, strings.Contains(output, "${{ secrets.EXAMPLE_WRITE_TOKEN }}"))
				require.NoError(t, os.WriteFile(path, []byte(content), 0600))
				fromFile, err := c.ParseWorkflowFile(path)
				require.NoError(t, err)
				assert.Equal(t, config.Plan.Digest, fromFile.Connectors["tracker"].Plan.Digest)
				if selection.name == "combined" {
					require.NotNil(t, data.SafeOutputs.AddComments)
				}
			}
		})
	}
}

func TestAWConnectorUsesExistingWithAndNestedImports(t *testing.T) {
	c, root := awConnectorCompiler(t)
	data, err := os.ReadFile("../../.github/aw/connectors/example.md")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), data, 0600))
	outer := "---\nimports:\n  - uses: connector.md\n    with:\n      issue: OTHER\n      reader-secret: APPROVED_READ_TOKEN\n---\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "outer.md"), []byte(outer), 0600))
	source := awConnectorHeader + "imports:\n  aw:\n    - outer.md\ntools:\n  tracker: [get-issue]\n---\nReview.\n"
	result, err := c.ParseWorkflowString(source, filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	assert.Equal(t, []string{"APPROVED_READ_TOKEN"}, result.Connectors["tracker"].Agent.Secrets())
	encoded, err := result.Connectors["tracker"].Agent.Encode()
	require.NoError(t, err)
	snapshot, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var frozen struct {
		Connection connectors.Connection `json:"connection"`
	}
	require.NoError(t, json.Unmarshal(snapshot, &frozen))
	assert.Equal(t, []string{"OTHER"}, frozen.Connection.Resources["issues"])
	output, err := c.CompileToYAML(result, filepath.Join(root, "proof.md"))
	require.NoError(t, err)
	assert.NotContains(t, output, "connectors invoke context")
	assert.NotContains(t, output, "EXAMPLE_CONTEXT_TOKEN")
}

func TestAWConnectorImportedContextSelectionIsRejected(t *testing.T) {
	t.Parallel()
	for _, selection := range []string{"null", "{}", "{issue-details: {issue: ISSUE-1}}"} {
		t.Run(selection, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), []byte("---\n"+awConnectorSource(t)+"---\n"), 0600))
			source := awConnectorHeader + "imports: [connector.md]\ncontext:\n  tracker: " + selection + "\n---\nRead approved references.\n"
			_, err := c.ParseWorkflowString(source, filepath.Join(root, "proof.md"))
			require.ErrorContains(t, err, "context.tracker is unsupported")
		})
	}
}

func TestAWConnectorDeclarationRejectsInvalidAuthority(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, old, replacement, selection, message string }{
		{"unknown-field", "endpoints:", "unknown-field: true\n        endpoints:", "", "unknown field"},
		{"old-trigger-definition-key", "triggers:", "\"on\":", "", "unknown field \"on\""},
		{"removed-context-definition", "endpoints:", "context: {}\n        endpoints:", "", "unknown field"},
		{"removed-context-credential", "phase: agent", "phase: context", "", "phase"},
		{"reserved-secret", "EXAMPLE_READ_TOKEN", "GITHUB_TOKEN", "tools:\n  tracker: [get-issue]\n", "non-reserved secret name"},
		{"expression-secret", "EXAMPLE_READ_TOKEN", "${{ secrets.TOKEN }}", "tools:\n  tracker: [get-issue]\n", "non-reserved secret name"},
		{"missing-secret", "secret: EXAMPLE_READ_TOKEN", "", "tools:\n  tracker: [get-issue]\n", "no connection credential"},
		{"wildcard-grant", "ISSUE-1", "\"*\"", "", "exact allowlist"},
		{"unknown-capability", "", "", "tools:\n  tracker: [missing]\n", "must name exactly one"},
		{"removed-context-selection", "", "", "context:\n  tracker:\n    issue-details: {issue: ISSUE-1}\n", "context.tracker is unsupported"},
		{"removed-empty-context", "", "", "context:\n  tracker: {}\n", "context.tracker is unsupported"},
		{"trigger", "", "", "on:\n  tracker-issue-created:\n", "ingress is not operational"},
		{"unprefixed-output", "", "", "safe-outputs:\n  add-comment:\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			source := awConnectorSource(t)
			if test.old != "" {
				source = strings.Replace(source, test.old, test.replacement, 1)
			}
			header := awConnectorHeader
			if test.name == "trigger" {
				header = strings.ReplaceAll(header, "on: workflow_dispatch\n", "")
			}
			result, err := c.ParseWorkflowString(header+source+test.selection+"---\nReview.\n", filepath.Join(root, "proof.md"))
			if test.name == "unprefixed-output" {
				require.NoError(t, err)
				assert.Empty(t, result.Connectors)
				require.NotNil(t, result.SafeOutputs.AddComments)
				return
			}
			require.ErrorContains(t, err, test.message)
		})
	}
}
