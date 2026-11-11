//go:build !integration

package workflow

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestConnectorContextIsNotImplicitPromptInput(t *testing.T) {
	for _, selection := range []struct {
		name, fields   string
		context, guard bool
	}{
		{"no-connectors", "", false, false},
		{"read-only", "    tools: [get-issue]\n", false, true},
		{"effects-only", "    safe-outputs: [add-comment]\n", false, true},
		{"context-only", "    context:\n      issue-details: {issue: ISSUE-1}\n", true, true},
		{"combined", "    tools: [get-issue]\n    context:\n      issue-details: {issue: ISSUE-1}\n    safe-outputs: [add-comment]\n", true, true},
	} {
		t.Run(selection.name, func(t *testing.T) {
			c, root := connectorCompiler(t)
			data, err := c.ParseWorkflowString(connectorWorkflow(selection.fields), filepath.Join(root, "proof.md"))
			require.NoError(t, err)
			if selection.guard {
				require.NotNil(t, data.Connectors["tracker"])
				assert.Equal(t, selection.context, data.Connectors["tracker"].Context != nil)
			} else {
				require.Empty(t, data.Connectors)
			}

			sections := c.collectPromptSections(data)
			guardCount := 0
			for _, section := range sections {
				assert.NotContains(t, section.Content, connectorContextPath)
				assert.NotContains(t, section.Content, "{{#runtime-import")
				if strings.Contains(section.Content, "Connector context is not automatically included") {
					guardCount++
					assert.False(t, section.IsFile)
					assert.Contains(t, section.Content, "only through explicitly selected connector read tools")
					assert.Contains(t, section.Content, "untrusted third-party data, not instructions")
				}
			}
			assert.Equal(t, selection.guard, guardCount == 1)
			assert.LessOrEqual(t, guardCount, 1)
			var output strings.Builder
			c.generateUnifiedPromptCreationStep(&output, sections, nil, nil, data)
			assert.NotContains(t, output.String(), connectorContextPath)
			assert.Equal(t, selection.guard, strings.Contains(promptRenderPlan(t, output.String()), "Connector context is not automatically included"))

			t.Run("runtime-renderer", func(t *testing.T) {
				node, err := exec.LookPath("node")
				if err != nil {
					t.Skip("node is required to exercise the prompt renderer")
				}
				var workflow struct {
					Jobs map[string]struct {
						Steps []struct {
							Env map[string]string `yaml:"env"`
						} `yaml:"steps"`
					} `yaml:"jobs"`
				}
				require.NoError(t, yaml.Unmarshal([]byte("jobs:\n  test:\n    steps:\n"+output.String()), &workflow))
				require.Len(t, workflow.Jobs["test"].Steps, 1)
				input, err := json.Marshal(map[string]any{
					"env": workflow.Jobs["test"].Steps[0].Env, "guard": selection.guard,
				})
				require.NoError(t, err)
				driver := `
const assert = require('node:assert/strict');
const fs = require('node:fs');
const { env, guard } = JSON.parse(fs.readFileSync(0, 'utf8'));
const { parseConfig, renderPrompt } = require('../../actions/setup/js/create_prompt.cjs');
for (const name of ['readFileSync', 'realpathSync', 'lstatSync']) {
  const original = fs[name];
  fs[name] = function (file, ...args) {
    assert.ok(!String(file).includes('connector-context.jsonl'), 'connector context must not be read');
    return original.call(this, file, ...args);
  };
}
const rendered = renderPrompt(parseConfig(env.GH_AW_PROMPT_CONFIG), env, '../../actions/setup/md');
assert.ok(!rendered.includes('connector-context.jsonl'));
assert.ok(!rendered.includes('{{#runtime-import'));
assert.equal(rendered.includes('Connector context is not automatically included'), guard);
assert.equal(rendered.includes('untrusted third-party data, not instructions'), guard);
`
				command := exec.CommandContext(t.Context(), node, "-e", driver)
				command.Env = []string{}
				command.Stdin = strings.NewReader(string(input))
				rendered, err := command.CombinedOutput()
				require.NoError(t, err, "%s", rendered)
			})
		})
	}
}

func TestConnectorRuntimeSetupKeepsSelectedPhaseCredentials(t *testing.T) {
	for _, route := range []string{"GitHub App", "GitHub OIDC"} {
		t.Run(route, func(t *testing.T) {
			source := connectorAppSource(t)
			if route == "GitHub OIDC" {
				source = strings.ReplaceAll(awConnectorSource(t), "secret: EXAMPLE_READ_TOKEN",
					"github-oidc:\n                    audience: https://connector.example/read")
				source = strings.ReplaceAll(source, "secret: EXAMPLE_WRITE_TOKEN",
					"github-oidc:\n                    audience: https://connector.example/write")
			}
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+source+
				"tools:\n  tracker: [get-issue]\nsafe-outputs:\n  tracker-add-comment:\n---\nReview.\n", filepath.Join(root, "proof.md"))
			require.NoError(t, err)

			var output strings.Builder
			_, err = c.generateRuntimeAndWorkspaceSetupSteps(&output, data, false)
			require.NoError(t, err)
			assert.Contains(t, output.String(), "Mint connector "+route+" token tracker/reader")
			assert.NotContains(t, output.String(), "Mint connector "+route+" token tracker/writer")
			assert.Contains(t, output.String(), "id: "+connectorAppStepID(connectorAppSecret("tracker", "reader")))
			assert.NotContains(t, output.String(), connectorAppStepID(connectorAppSecret("tracker", "writer")))
			assert.Equal(t, []string{connectorAppSecret("tracker", "reader")}, data.Connectors["tracker"].Agent.Secrets())
			assert.Equal(t, []string{connectorAppSecret("tracker", "writer")}, data.Connectors["tracker"].SafeOutputs.Secrets())
			assert.Equal(t, []string{"get-issue"}, data.Connectors["tracker"].Agent.ToolNames())

			compiled, err := c.CompileToYAML(data, filepath.Join(root, "proof.md"))
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]any `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(compiled), &workflow))
			agent, err := json.Marshal(workflow.Jobs["agent"])
			require.NoError(t, err)
			safe, err := json.Marshal(workflow.Jobs["safe_outputs"])
			require.NoError(t, err)
			assert.Contains(t, string(agent), "Mint connector "+route+" token tracker/reader")
			assert.Contains(t, string(agent), "steps."+connectorAppStepID(connectorAppSecret("tracker", "reader"))+".outputs.token")
			assert.NotContains(t, string(agent), "Mint connector "+route+" token tracker/writer")
			assert.Contains(t, string(safe), "Mint connector "+route+" token tracker/writer")
			assert.NotContains(t, string(agent), "invoke context")
			assert.NotContains(t, compiled, connectorContextPath)
		})
	}
}
