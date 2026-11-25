//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func connectorAzureSource(t *testing.T) string {
	t.Helper()
	doc, err := parser.ExtractFrontmatterFromContent("---\n" + awConnectorSource(t) + "---\n")
	require.NoError(t, err)
	tracker := doc.Frontmatter["connectors"].(map[string]any)["tracker"].(map[string]any)
	creds := tracker["credentials"].(map[string]any)
	for _, name := range []string{"reader", "writer"} {
		credential := creds[name].(map[string]any)
		delete(credential, "secret")
		credential["azure-oidc"] = map[string]string{
			"azure-client-id": "${{ vars." + name + "_ID }}",
			"azure-tenant-id": "12345678-1234-1234-1234-123456789012",
			"azure-scope":     "https://api.example.com/.default",
		}
	}
	tracker["mcp-servers"] = map[string]any{"remote": map[string]any{
		"url": "https://api.example.com/mcp", "credential": "reader", "readOnly": true,
		"tools": map[string]any{"native_read": map[string]any{"inputs": map[string]any{}}}, "maxResponseBytes": 65536,
	}}
	source, err := yaml.Marshal(doc.Frontmatter)
	require.NoError(t, err)
	return string(source)
}

func TestConnectorAzureBrokerPhaseIsolation(t *testing.T) {
	const read = "tools:\n  tracker: [get-issue]\n"
	const remote = "tools:\n  tracker: {remote: [native_read]}\n"
	const write = "safe-outputs:\n  environment: connector-publish\n  tracker-add-comment:\n"
	for _, tc := range []struct {
		name, selection string
		reader, writer  bool
	}{
		{"import-only", "", false, false},
		{"reader", read, true, false},
		{"remote", remote, true, false},
		{"writer", write, false, true},
		{"combined", read + write, true, true},
		{"staged", read + "safe-outputs:\n  tracker-add-comment:\n    staged: true\n", true, false},
		{"conditional", "safe-outputs:\n  environment: connector-publish\n  tracker-add-comment:\n    staged: ${{ inputs.stage }}\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+tc.selection+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			output, err := c.CompileToYAML(data, root+"/proof.md")
			require.NoError(t, err)
			var workflow struct {
				Jobs map[string]struct {
					Permissions map[string]string
					Environment string
					Steps       []map[string]any
				}
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &workflow))
			total := 0
			for jobID, job := range workflow.Jobs {
				require.Equal(t, tc.writer && jobID == "safe_outputs", job.Environment == "connector-publish", jobID)
				for _, step := range job.Steps {
					run, _ := step["run"].(string)
					if !strings.HasPrefix(run, "node \"$RUNNER_TEMP/gh-aw/actions/connector_azure_oidc.cjs\" start") {
						continue
					}
					total++
					require.Equal(t, "write", job.Permissions["id-token"])
					writer := step["name"] == "Start connector Azure OIDC broker tracker/writer"
					if writer {
						require.Equal(t, "safe_outputs", jobID)
						require.Equal(t, tc.name == "conditional", step["if"] != nil)
					} else {
						require.Equal(t, "agent", jobID)
					}
				}
				usesFederation := jobID == "agent" && tc.reader || jobID == "safe_outputs" && tc.writer
				if jobID != "activation" && jobID != "conclusion" && !usesFederation {
					require.NotEqual(t, "write", job.Permissions["id-token"], jobID)
				}
			}
			want := 0
			if tc.reader {
				want++
			}
			if tc.writer {
				want++
			}
			require.Equal(t, want, total)
			require.Equal(t, want, strings.Count(output, "Stop connector Azure OIDC broker "))
			require.Equal(t, want, strings.Count(output, "GH_AW_CONNECTOR_BROKER_TIMEOUT_MINUTES:"))
			for _, name := range []string{"reader", "writer"} {
				secret := connectorAppSecret("tracker", name)
				require.NotContains(t, output, "${{ secrets."+secret)
				require.NotContains(t, output, "steps."+connectorAppStepID(secret)+".outputs.token")
			}
			agent, err := json.Marshal(workflow.Jobs["agent"].Steps)
			require.NoError(t, err)
			require.NotContains(t, string(agent), "vars.writer_ID")
			require.Equal(t, tc.reader, strings.Contains(string(agent), connectorAzureMount(connectorAppSecret("tracker", "reader"))))
			require.NotContains(t, string(agent), connectorAzureMount(connectorAppSecret("tracker", "writer")))
			require.Equal(t, tc.writer, strings.Contains(output, "vars.writer_ID"))
		})
	}
}

func TestConnectorAzureBrokerRejectsUnsafeRoutes(t *testing.T) {
	src := connectorAzureSource(t)
	const selection = "tools:\n  tracker: [get-issue]\nsafe-outputs:\n  tracker-add-comment:\n"
	for _, tc := range []struct{ name, replace, with, extra, err string }{
		{"client-expression", "vars.reader_ID", "github.actor", "", "UUIDs or standalone vars"},
		{"invalid-tenant", "12345678-1234-1234-1234-123456789012", "../common", "", "UUIDs or standalone vars"},
		{"invalid-scope", "https://api.example.com/.default", "dynamic", "", "single literal"},
		{"same-client", "vars.writer_ID", "vars.reader_ID", "", "separate client"},
		{"non-bearer", "scheme: bearer", "scheme: raw", "", "Authorization/bearer"},
		{"disabled", "", "", "sandbox:\n  agent: false\n", "sandbox"},
		{"host-access", "", "", "sandbox:\n  agent:\n    runtime: docker-sudo-iptables\n", "sandbox"},
		{"sibling-mount", "", "", "mcp-servers:\n  sibling:\n    type: stdio\n    container: node:22-alpine\n    mounts: ['${RUNNER_TEMP}:/mount:ro']\n", "sibling"},
		{"gateway-mount", "", "", "sandbox:\n  mcp:\n    mounts: ['${RUNNER_TEMP}:/mount:ro']\n", "trusted MCP gateway"},
		{"container", "", "", "container: ubuntu:latest\n", "custom host access"},
		{"services", "", "", "services:\n  redis:\n    image: redis:latest\n", "custom host access"},
		{"setup-steps", "", "", "jobs:\n  agent:\n    setup-steps:\n      - run: echo setup\n", "authored runner steps"},
		{"job-pre-steps", "", "", "jobs:\n  agent:\n    pre-steps:\n      - run: echo setup\n", "authored runner steps"},
		{"secret-masking", "", "", "secret-masking:\n  steps:\n    - run: echo setup\n", "authored runner steps"},
		{"mcp-scripts", "", "", "mcp-scripts:\n  example:\n    description: example\n    script: return 1;\n", "mcp-scripts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			source := src
			if tc.replace != "" {
				source = strings.ReplaceAll(source, tc.replace, tc.with)
			}

			_, err := c.ParseWorkflowString(awConnectorHeader+source+tc.extra+selection+"---\nReview.\n", root+"/proof.md")
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestConnectorAzureWriterRequiresExclusiveEnvironment(t *testing.T) {
	const write = "safe-outputs:\n  tracker-add-comment:\n"
	for _, tc := range []struct{ name, selection, message string }{
		{"missing", write, "dedicated literal"},
		{"dynamic", write + "  environment: ${{ inputs.stage }}\n", "dedicated literal"},
		{"shared", "environment: Publish\n" + write + "  environment: publish\n", "must not share"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+tc.selection+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			_, err = c.CompileToYAML(data, root+"/proof.md")
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestConnectorAzureWriterIdentityOnlyForLiveWrites(t *testing.T) {
	const read = "tools:\n  tracker: [get-issue]\n"
	for _, tc := range []struct {
		name, selection string
		forceStaged     bool
		wantError       bool
	}{
		{"live", "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n", false, true},
		{"staged", "safe-outputs:\n  tracker-add-comment:\n    staged: true\n", false, false},
		{"forced", "safe-outputs:\n  tracker-add-comment:\n", true, false},
		{"conditional", "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n    staged: ${{ inputs.stage }}\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			c.forceStaged = tc.forceStaged
			source := strings.ReplaceAll(connectorAzureSource(t), "vars.writer_ID", "vars.reader_ID")
			_, err := c.ParseWorkflowString(awConnectorHeader+source+read+tc.selection+"---\nReview.\n", root+"/proof.md")
			if tc.wantError {
				require.ErrorContains(t, err, "separate client")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConnectorAzureBrokerRejectsWindows(t *testing.T) {
	for _, tc := range []struct{ name, selection, phase string }{
		{"reader", "runs-on: windows-latest\ntools:\n  tracker: [get-issue]\n", "agent"},
		{"writer", "safe-outputs:\n  runs-on: windows-latest\n  environment: publish\n  tracker-add-comment:\n", "safe-output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, root := awConnectorCompiler(t)
			data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+tc.selection+"---\nReview.\n", root+"/proof.md")
			require.NoError(t, err)
			_, err = c.CompileToYAML(data, root+"/proof.md")
			require.ErrorContains(t, err, "connector Azure OIDC "+tc.phase+" requires a non-Windows runner")
		})
	}
}

func TestConnectorAzureWriterRejectsNativeInferenceIdentity(t *testing.T) {
	c, root := awConnectorCompiler(t)
	selection := "safe-outputs:\n  environment: publish\n  tracker-add-comment:\n"
	data, err := c.ParseWorkflowString(awConnectorHeader+connectorAzureSource(t)+selection+"---\nReview.\n", root+"/proof.md")
	require.NoError(t, err)
	data.EngineConfig.Auth = &EngineAuthConfig{
		Type: "github-oidc", Provider: "azure", AzureClientID: "${{ vars.writer_ID }}",
	}
	require.ErrorContains(t, c.validateConnectorAzureBoundary(data), "separate client")
	data.EngineConfig.Auth.AzureClientID = "${{ vars.other_ID }}"
	require.NoError(t, c.validateConnectorAzureBoundary(data))
}
