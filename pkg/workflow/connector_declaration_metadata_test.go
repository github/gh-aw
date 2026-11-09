//go:build !integration

package workflow

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func connectorMetadataSource(t *testing.T, metadata map[string]any) string {
	t.Helper()
	var document map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(awConnectorSource(t)), &document))
	declaration := document["connectors"].(map[string]any)["tracker"].(map[string]any)
	maps.Copy(declaration, metadata)
	encoded, err := yaml.Marshal(document)
	require.NoError(t, err)
	return string(encoded)
}

func TestConnectorDeclarationMetadata(t *testing.T) {
	for _, experimental := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "stable", true: "experimental"}[experimental], map[bool]string{false: "direct", true: "imported"}[imported]}, "-"), func(t *testing.T) {
				c, root := awConnectorCompiler(t)
				reference := "https://docs.example.com/api/read#response"
				source := connectorMetadataSource(t, map[string]any{"experimental": experimental, "docs": []string{reference}})
				if imported {
					require.NoError(t, os.WriteFile(filepath.Join(root, "connector.md"), []byte("---\n"+source+"---\n"), 0600))
					source = "imports: [connector.md]\n"
				}
				for _, selected := range []bool{false, true} {
					selection := ""
					if selected {
						selection = "tools:\n  tracker: [get-issue]\n"
					}
					data, err := c.ParseWorkflowString(awConnectorHeader+source+selection+"---\nRead approved evidence.\n", filepath.Join(root, "proof.md"))
					require.NoError(t, err)
					if !selected {
						require.Empty(t, data.Connectors)
						continue
					}
					require.Equal(t, experimental, data.Connectors["tracker"].Experimental)
					require.Equal(t, []string{reference}, data.Connectors["tracker"].Docs)
					for _, batchMode := range []bool{false, true} {
						compiler := NewCompiler()
						compiler.SetBatchMode(batchMode)
						var output bytes.Buffer
						compiler.emitExperimentalFeatureWarningsTo(data, &output)
						if experimental {
							require.Equal(t, 1, compiler.GetWarningCount())
							if batchMode {
								require.Empty(t, output.String())
								require.Equal(t, 1, compiler.GetExperimentalFeatureUsage()["Using experimental feature: connectors.tracker"])
							} else {
								require.Contains(t, output.String(), "Using experimental feature: connectors.tracker")
							}
						} else {
							require.Empty(t, output.String())
							require.Zero(t, compiler.GetWarningCount())
						}
					}
				}
			})
		}
	}
}

func TestConnectorDeclarationMetadataTypes(t *testing.T) {
	for _, test := range []struct {
		name     string
		metadata map[string]any
	}{
		{"experimental-string", map[string]any{"experimental": "true"}},
		{"experimental-null", map[string]any{"experimental": nil}},
		{"docs-string", map[string]any{"docs": "https://docs.example.com"}},
		{"docs-non-string", map[string]any{"docs": []any{42}}},
		{"docs-http", map[string]any{"docs": []string{"http://docs.example.com"}}},
		{"docs-relative", map[string]any{"docs": []string{"/api/reference"}}},
		{"docs-credentials", map[string]any{"docs": []string{"https://user:password@docs.example.com"}}},
		{"docs-expression", map[string]any{"docs": []string{"https://docs.example.com/${TOKEN}"}}},
		{"docs-whitespace", map[string]any{"docs": []string{"https://docs.example.com/a b"}}},
		{"docs-duplicate", map[string]any{"docs": []string{"https://docs.example.com", "https://docs.example.com"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			source := connectorMetadataSource(t, test.metadata)
			_, err := c.ParseWorkflowString(awConnectorHeader+source+"---\nRead approved evidence.\n", filepath.Join(root, "proof.md"))
			require.Error(t, err)
		})
	}
}

func TestAWConnectorGlobalStagedOutputs(t *testing.T) {
	for _, staged := range []string{"true", "false", "${{ github.event_name == 'workflow_dispatch' }}"} {
		t.Run(staged, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			source := awConnectorHeader + awConnectorSource(t) + "safe-outputs:\n  staged: " + staged + "\n  tracker-add-comment:\n---\nStage the requested comment.\n"
			data, err := c.ParseWorkflowString(source, filepath.Join(root, "proof.md"))
			require.NoError(t, err)
			output, err := c.CompileToYAML(data, filepath.Join(root, "proof.md"))
			require.NoError(t, err)
			if staged != "false" {
				require.Contains(t, output, "GH_AW_SAFE_OUTPUTS_STAGED:")
			}
			require.Equal(t, staged != "true", strings.Contains(output, "secrets.EXAMPLE_WRITE_TOKEN"))
			require.Contains(t, data.SafeOutputs.Scripts["tracker-add-comment"].Script, "staged ? '--stage' : '--allow-writes'")
		})
	}
}

func TestAWConnectorWithSkipIfDecision(t *testing.T) {
	c, root := awConnectorCompiler(t)
	source := awConnectorHeader + awConnectorSource(t) +
		"skip-if-decision:\n  - Is there no actionable work remaining?\n" +
		"tools:\n  tracker: [get-issue]\n" +
		"safe-outputs:\n  staged: true\n  tracker-add-comment:\n---\nRead approved evidence.\n"
	path := filepath.Join(root, "connector-decision.md")
	data, err := c.ParseWorkflowString(source, path)
	require.NoError(t, err)
	require.Equal(t, []string{"Is there no actionable work remaining?"}, data.SkipIfDecision)
	connector := data.Connectors["tracker"]
	require.NotNil(t, connector)
	require.NotNil(t, connector.Agent)
	require.NotNil(t, connector.SafeOutputs)
	require.Nil(t, connector.Context)
	output, err := c.CompileToYAML(data, path)
	require.NoError(t, err)
	require.Contains(t, output, "GH_AW_SKIP_IF_DECISION")
	require.NotContains(t, output, "secrets.EXAMPLE_WRITE_TOKEN")
}
