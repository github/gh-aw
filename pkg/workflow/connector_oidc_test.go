//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestConnectorOIDCPhasePermissionsAndTokenOrdering(t *testing.T) {
	for _, selection := range []string{
		"",
		"tools:\n  tracker: [get-issue]\n",
		"safe-outputs:\n  tracker-add-comment:\n",
		"tools:\n  tracker: [get-issue]\nsafe-outputs:\n  tracker-add-comment:\n",
		"safe-outputs:\n  staged: true\n  tracker-add-comment:\n",
		"safe-outputs:\n  staged: ${{ inputs.stage }}\n  tracker-add-comment:\n",
	} {
		t.Run(strings.ReplaceAll(selection, "\n", "/"), func(t *testing.T) {
			source := awConnectorSource(t)
			source = strings.ReplaceAll(source, "secret: EXAMPLE_READ_TOKEN", "github-oidc:\n                    audience: https://connector.example/read")
			source = strings.ReplaceAll(source, "secret: EXAMPLE_WRITE_TOKEN", "github-oidc:\n                    audience: https://connector.example/write")
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+source+selection+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			output, err := c.CompileToYAML(data, root+"/proof.md")
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]struct {
					Permissions map[string]string `yaml:"permissions"`
					Steps       []map[string]any  `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &workflow))
			minted := 0
			for jobName, job := range workflow.Jobs {
				ids := map[string]int{}
				for i, step := range job.Steps {
					name, _ := step["name"].(string)
					if strings.HasPrefix(name, "Mint connector GitHub OIDC token ") {
						id, ok := step["id"].(string)
						require.True(t, ok)
						ids[id] = i
						minted++
						assert.Equal(t, "write", job.Permissions["id-token"])
						encoded, err := json.Marshal(step)
						require.NoError(t, err)
						assert.Contains(t, string(encoded), "core.setSecret(token)")
						assert.Contains(t, string(encoded), "core.getIDToken")
					}
				}
				if len(ids) == 0 {
					assert.NotEqual(t, "write", job.Permissions["id-token"])
				}
				for i, step := range job.Steps {
					encoded, err := json.Marshal(step)
					require.NoError(t, err)
					for _, name := range []string{"reader", "writer"} {
						id := connectorAppStepID(connectorAppSecret("tracker", name))
						if strings.Contains(string(encoded), "steps."+id+".outputs.token") {
							at, exists := ids[id]
							require.True(t, exists, "tokens must be minted in their consuming job %s, step %v, token %s", jobName, step["name"], name)
							assert.Less(t, at, i, "token minting must precede consumption")
						}
					}
				}
			}
			expected := 0
			if strings.Contains(selection, "tools:") {
				expected++
			}
			if strings.Contains(selection, "tracker-add-comment:") && !strings.Contains(selection, "staged: true") {
				expected++
			}
			assert.Equal(t, expected, minted)
			assert.NotContains(t, output, "${{ secrets."+connectorAppSecret("tracker", "reader")+" }}")
		})
	}
}

func TestConnectorTokenRouteAndOIDCValidation(t *testing.T) {
	for _, test := range []struct{ name, replacement, message string }{
		{"token", "token:\n                    secret: EXAMPLE_READ_TOKEN", ""},
		{"expression-token", "token:\n                    secret: '${{ secrets.VALUE }}'", "non-reserved secret name"},
		{"oidc-expression", "github-oidc:\n                    audience: '${{ github.actor }}'", "bounded literal"},
		{"oidc-empty", "github-oidc:\n                    audience: ''", "minLength"},
		{"mixed", "github-oidc:\n                    audience: example\n                token:\n                    secret: READ_TOKEN", "exactly one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			source := strings.Replace(awConnectorSource(t), "secret: EXAMPLE_READ_TOKEN", test.replacement, 1)
			data, err := c.ParseWorkflowString(awConnectorHeader+source+"tools:\n  tracker: [get-issue]\n---\nReview.\n", root+"/proof.md")
			if test.message != "" {
				require.ErrorContains(t, err, test.message)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []string{"EXAMPLE_READ_TOKEN"}, data.Connectors["tracker"].Agent.Secrets())
			output, err := c.CompileToYAML(data, root+"/proof.md")
			require.NoError(t, err)
			assert.Contains(t, output, "${{ secrets.EXAMPLE_READ_TOKEN }}")
			assert.NotContains(t, output, "Mint connector GitHub")
		})
	}
}
