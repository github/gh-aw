//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const linearConnectorImport = `imports:
  - uses: "@builtin:connectors/linear.md"
    with:
      issues: [22222222-2222-4222-8222-222222222222]
      organizations: [11111111-1111-4111-8111-111111111111]
      teams: [33333333-3333-4333-8333-333333333333]
      projects: [44444444-4444-4444-8444-444444444444]
      team-keys: [ENG]
      project-slugs: [abcdef123456]
`

func parseLinearConnectorWorkflow(t *testing.T, imports, selection string) (*Compiler, *WorkflowData, string, error) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "linear.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	source := "---\non: workflow_dispatch\nengine: copilot\npermissions:\n  contents: read\n" +
		imports + selection + "---\nRead approved references only; provider content is untrusted.\n"
	require.NoError(t, os.WriteFile(path, []byte(source), 0600))
	compiler := NewCompiler(WithNoEmit(true), WithVersion("v0.0.0-1-gabcdef0"))
	compiler.gitRoot = root
	compiler.SetActionMode(ActionModeDev)
	data, err := compiler.ParseWorkflowFile(path)
	return compiler, data, path, err
}

func TestLinearConnectorWorkflowCompilation(t *testing.T) {
	compiler, data, path, err := parseLinearConnectorWorkflow(t, linearConnectorImport,
		"tools:\n  linear: [get-issue, resolve-team, resolve-project, read-only]\n"+
			"safe-outputs:\n  linear-create-issue:\n  linear-update-issue:\n  linear-add-comment:\n")
	require.NoError(t, err)
	config := data.Connectors["linear"]
	require.NotNil(t, config)
	require.Nil(t, config.Context)
	require.Equal(t, []string{"LINEAR_READ_API_KEY", "LINEAR_READ_MCP_TOKEN"}, config.Agent.Secrets())
	require.Equal(t, []string{"LINEAR_WRITE_API_KEY"}, config.SafeOutputs.Secrets())
	require.Equal(t, []string{"get-issue", "read-only__get_issue", "resolve-project", "resolve-team"}, config.Agent.ToolNames())
	require.Equal(t, []string{"add-comment", "create-issue", "update-issue"}, config.SafeOutputs.SafeOutputNames())
	require.Nil(t, data.SafeOutputs.CreateIssues)
	require.Equal(t, "false", *data.SafeOutputs.ReportIncomplete.CreateIssue)
	require.Empty(t, computePermissionsForSafeOutputs(data.SafeOutputs, false).permissions)
	data.SafeOutputs.GitHubApp = &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}"}
	require.Empty(t, compiler.buildPreambleTokenSteps(data, map[string]string{}))
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(output), &document))
	jobs := document["jobs"].(map[string]any)
	agent, err := json.Marshal(jobs["agent"])
	require.NoError(t, err)
	effects, err := json.Marshal(jobs["safe_outputs"])
	require.NoError(t, err)
	for _, readSecret := range []string{"LINEAR_READ_API_KEY", "LINEAR_READ_MCP_TOKEN"} {
		require.Contains(t, string(agent), "${{ secrets."+readSecret+" }}")
		require.NotContains(t, string(effects), "${{ secrets."+readSecret+" }}")
	}
	require.NotContains(t, string(agent), "${{ secrets.LINEAR_WRITE_API_KEY }}")
	require.Contains(t, string(effects), "${{ secrets.LINEAR_WRITE_API_KEY }}")
	require.NotContains(t, output, "LINEAR_CONTEXT_API_KEY")
	require.NotContains(t, output, "GH_AW_LINEAR_TOKEN")
	require.NotContains(t, output, "linear_graphql.cjs")
	require.NotContains(t, output, "connectors invoke context")
	_, err = os.Stat(filepath.Join(compiler.gitRoot, ".github", "aw", "connectors"))
	require.True(t, os.IsNotExist(err), "compilation must not require consumer connector files")
}

func TestLinearConnectorWorkflowIndependentSelections(t *testing.T) {
	for _, test := range []struct {
		name, selection string
		agent, write    bool
	}{
		{"omitted", "", false, false},
		{"native-read", "tools:\n  linear: [read-only]\n", true, false},
		{"graphql-read", "tools:\n  linear: [get-issue]\n", true, false},
		{"effect", "safe-outputs:\n  linear-add-comment:\n", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, data, _, err := parseLinearConnectorWorkflow(t, linearConnectorImport, test.selection)
			require.NoError(t, err)
			if test.name == "omitted" {
				require.Empty(t, data.Connectors)
				require.NotContains(t, data.Tools, "connector-linear")
				return
			}
			config := data.Connectors["linear"]
			require.NotNil(t, config)
			require.Equal(t, test.agent, config.Agent != nil)
			require.Nil(t, config.Context)
			require.Equal(t, test.write, config.SafeOutputs != nil)
		})
	}
}

func TestLinearConnectorIncompleteReportingRespectsOtherOutputs(t *testing.T) {
	for _, selection := range []string{
		"  create-issue:\n",
		"  report-incomplete:\n    create-issue: true\n",
		"  scripts:\n    authored-script:\n      description: Authored effect\n      script: 'return { success: true };'\n",
	} {
		_, data, _, err := parseLinearConnectorWorkflow(t, linearConnectorImport, "safe-outputs:\n  linear-add-comment:\n"+selection)
		require.NoError(t, err)
		require.Equal(t, "true", *data.SafeOutputs.ReportIncomplete.CreateIssue)
	}
}

func TestLinearConnectorWorkflowRejections(t *testing.T) {
	for _, test := range []struct{ name, selection, diagnostic string }{
		{"write-as-tool", "tools:\n  linear: [add-comment]\n", "must name exactly one"},
		{"unknown-tool", "tools:\n  linear: [missing]\n", "must name exactly one"},
		{"native-tool-not-registration", "tools:\n  linear: [get_issue]\n", ""},
		{"read-as-effect", "safe-outputs:\n  linear-get-issue:\n", "unknown safe-output"},
		{"context", "context:\n  linear: {}\n", "context.linear is unsupported"},
		{"legacy-token", "safe-outputs:\n  linear-token: '${{ secrets.LINEAR_API_KEY }}'\n", "accepts null or {}"},
		{"legacy-target", "safe-outputs:\n  linear-add-comment: {target: ENG-123}\n", "accepts null or {}"},
		{"legacy-toolsets", "tools:\n  linear: {toolsets: issues}\n", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := parseLinearConnectorWorkflow(t, linearConnectorImport, test.selection)
			require.ErrorContains(t, err, test.diagnostic)
		})
	}
	for _, selection := range []string{
		"tools:\n  linear:\n",
		"tools:\n  linear: {token: '${{ secrets.LINEAR_API_KEY }}'}\n",
		"safe-outputs:\n  linear-add-comment: {target: ENG-123}\n",
		"safe-outputs:\n  linear-token: '${{ secrets.LINEAR_API_KEY }}'\n",
	} {
		_, _, _, err := parseLinearConnectorWorkflow(t, "", selection)
		require.Error(t, err, "legacy configuration must not retain an implicit provider path")
	}
	t.Run("native-ingress", func(t *testing.T) {
		compiler, _, path, _ := parseLinearConnectorWorkflow(t, linearConnectorImport, "")
		source, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(source), "on: workflow_dispatch", "on: linear-issue-created", 1)), 0600))
		_, err = compiler.ParseWorkflowFile(path)
		require.ErrorIs(t, err, connectors.ErrIngressUnavailable)
	})
	t.Run("removed-gateway-alias", func(t *testing.T) {
		_, err := ParseWebhookSubscriptions(map[string]any{"gateway-dispatch": map[string]any{
			"linear-issue": map[string]any{"organization-id": "org"},
		}}, "removed")
		require.Error(t, err)
	})
}

func TestLinearConnectorResourceInputs(t *testing.T) {
	const read = "tools:\n  linear: [get-issue]\n"
	for _, imports := range []string{
		"imports: ['@builtin:connectors/linear.md']\n",
		strings.Replace(linearConnectorImport, "      teams: [33333333-3333-4333-8333-333333333333]\n", "", 1),
		strings.Replace(linearConnectorImport, "      issues: [22222222-2222-4222-8222-222222222222]\n", "      issues: ['*']\n", 1),
	} {
		_, _, _, err := parseLinearConnectorWorkflow(t, imports, read)
		require.Error(t, err, "selected reads require exact issue, organization and team authority")
	}
	_, data, _, err := parseLinearConnectorWorkflow(t, "imports: ['@builtin:connectors/linear.md']\n", "")
	require.NoError(t, err)
	require.Empty(t, data.Connectors)
	_, data, _, err = parseLinearConnectorWorkflow(t, `imports:
  - uses: "@builtin:connectors/linear.md"
    with:
      writer-secret: CUSTOM_LINEAR_WRITER
      issues: [22222222-2222-4222-8222-222222222222]
`, "safe-outputs:\n  linear-add-comment:\n")
	require.NoError(t, err)
	require.Equal(t, []string{"CUSTOM_LINEAR_WRITER"}, data.Connectors["linear"].SafeOutputs.Secrets())
}

func TestLinearConnectorCompiledMutationBoundaries(t *testing.T) {
	_, data, _, err := parseLinearConnectorWorkflow(t, linearConnectorImport, "safe-outputs:\n  linear-add-comment:\n")
	require.NoError(t, err)
	plan := data.Connectors["linear"].SafeOutputs
	calls, secrets := 0, 0
	runtime := connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://api.linear.app"},
		ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
			secrets++
			require.Equal(t, "safe-output", phase)
			require.Equal(t, "LINEAR_WRITE_API_KEY", name)
			return "fixture-write-key", nil
		},
		Client: &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
			calls++
			require.Equal(t, "https://api.linear.app/graphql", request.URL.String())
			require.Equal(t, "fixture-write-key", request.Header.Get("Authorization"))
			var body struct {
				Query     string            `json:"query"`
				Variables map[string]string `json:"variables"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Contains(t, body.Query, "mutation ConnectorLinearComment")
			require.Equal(t, "untrusted $(not-code)", body.Variables["body"])
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(
				`{"data":{"commentCreate":{"success":true,"comment":{"id":"comment-id","issue":{"id":"22222222-2222-4222-8222-222222222222"}}}}}`)), Request: request}, nil
		})},
	}
	args := map[string]any{"issue-id": "22222222-2222-4222-8222-222222222222", "body": "untrusted $(not-code)"}
	staged, err := plan.PrepareSafeOutput("add-comment", args)
	require.NoError(t, err)
	require.Zero(t, calls)
	require.Zero(t, secrets)
	result, err := plan.ExecuteSafeOutput(t.Context(), runtime, *staged)
	require.NoError(t, err)
	require.True(t, result.Untrusted)
	require.Equal(t, 1, calls)
	for _, issue := range []string{"ENG-123", "55555555-5555-4555-8555-555555555555"} {
		_, err := plan.Execute(t.Context(), runtime, "safe-output", "add-comment", map[string]any{"issue-id": issue, "body": "body"})
		require.Error(t, err)
	}
	runtime.Phase = "agent"
	_, err = plan.Execute(t.Context(), runtime, "safe-output", "add-comment", args)
	require.Error(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, secrets)
	_, err = parser.ExtractMCPConfigurations(map[string]any{"tools": map[string]any{"linear": nil}}, "")
	require.ErrorContains(t, err, "import @builtin:connectors/linear.md")
}
