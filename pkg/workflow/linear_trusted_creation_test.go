//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

const linearTrustedCreationImport = `imports:
  - uses: "@builtin:connectors/linear.md"
    with:
      writer-secret: LINEAR_API_KEY
      create-team-secret: LINEAR_TEAM_ID
      create-project-slug: "810f57a7e383"
`

func TestLinearSmokeWorkflowPreservesTrustedDestination(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	path := filepath.Join(root, ".github", "workflows", "smoke-issues.md")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(source), "create-team-secret: LINEAR_TEAM_ID")
	require.Contains(t, string(source), `create-project-slug: "810f57a7e383"`)
	require.Contains(t, string(source), "writer-secret: LINEAR_API_KEY")
	require.NotContains(t, string(source), "abcdef123456")
	compiler := NewCompiler(WithNoEmit(true), WithVersion("v0.0.0-1-gabcdef0"))
	compiler.gitRoot = root
	compiler.SetActionMode(ActionModeDev)
	compiler.SetWorkflowIdentifier("smoke-issues.md")
	data, err := compiler.ParseWorkflowFile(path)
	require.NoError(t, err)
	require.Nil(t, data.Connectors["linear"].Agent)
	require.Equal(t, []string{"create-issue-in-project"}, data.Connectors["linear"].SafeOutputs.SafeOutputNames())
	require.Equal(t, []string{"LINEAR_API_KEY", "LINEAR_TEAM_ID"}, data.Connectors["linear"].SafeOutputs.Secrets())
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	agent := extractJobSection(output, "agent")
	require.NotContains(t, agent, "${{ secrets.LINEAR_API_KEY }}")
	require.NotContains(t, agent, "${{ secrets.LINEAR_TEAM_ID }}")
	safeOutputs := extractJobSection(output, "safe_outputs")
	require.Contains(t, safeOutputs, "${{ secrets.LINEAR_API_KEY }}")
	require.Contains(t, safeOutputs, "${{ secrets.LINEAR_TEAM_ID }}")
}

func linearTrustedCreationPlan(t *testing.T) *connectors.Plan {
	t.Helper()
	_, data, _, err := parseLinearConnectorWorkflow(t, linearTrustedCreationImport, "safe-outputs:\n  linear-create-issue-in-project:\n")
	require.NoError(t, err)
	require.Nil(t, data.Connectors["linear"].Agent)
	require.Equal(t, []string{"LINEAR_API_KEY", "LINEAR_TEAM_ID"}, data.Connectors["linear"].SafeOutputs.Secrets())
	script := data.SafeOutputs.Scripts[connectorSafeOutputName("linear", "create-issue-in-project")]
	require.NotNil(t, script)
	require.Contains(t, script.Inputs, "title")
	require.Contains(t, script.Inputs, "body")
	require.NotContains(t, script.Inputs, "team-id")
	require.NotContains(t, script.Inputs, "project-id")
	return data.Connectors["linear"].SafeOutputs
}

func linearTrustedPreflightResponse() map[string]any {
	return map[string]any{"data": map[string]any{
		"team": map[string]any{"id": "33333333-3333-4333-8333-333333333333"},
		"projects": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false},
			"nodes": []any{map[string]any{
				"id": "44444444-4444-4444-8444-444444444444", "slugId": "810f57a7e383",
				"teams": map[string]any{
					"nodes":    []any{map[string]any{"id": "33333333-3333-4333-8333-333333333333"}},
					"pageInfo": map[string]any{"hasNextPage": false},
				},
			}},
		},
	}}
}

func TestLinearTrustedCreationPreflight(t *testing.T) {
	plan := linearTrustedCreationPlan(t)
	for _, scenario := range []string{
		"success", "missing-team-secret", "malformed-team-secret", "wrong-team", "wrong-membership",
		"missing-project", "ambiguous-project", "paginated-membership", "wrong-slug",
		"invalid-projected-uuid", "graphql-errors", "malformed-response", "oversized-response", "redirect", "wrong-created-project",
	} {
		t.Run(scenario, func(t *testing.T) {
			requests := 0
			runtime := connectors.Runtime{
				Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://api.linear.app"},
				ResolveSecret: func(_ context.Context, phase, name string) (string, error) {
					require.Equal(t, "safe-output", phase)
					switch name {
					case "LINEAR_TEAM_ID":
						if scenario == "missing-team-secret" {
							return "", errors.New("team binding is not configured")
						}
						if scenario == "malformed-team-secret" {
							return "${{ github.event.issue.title }}", nil
						}
						return "33333333-3333-4333-8333-333333333333", nil
					case "LINEAR_API_KEY":
						return "fixture-write-key", nil
					default:
						t.Fatalf("unexpected credential %q", name)
						return "", errors.New("unexpected credential")
					}
				},
			}
			runtime.Client = &http.Client{Transport: connectorSmokeTransport(func(request *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, "https://api.linear.app/graphql", request.URL.String())
				require.Equal(t, "fixture-write-key", request.Header.Get("Authorization"))
				var document struct {
					Query     string            `json:"query"`
					Variables map[string]string `json:"variables"`
				}
				require.NoError(t, json.NewDecoder(request.Body).Decode(&document))
				require.Equal(t, "33333333-3333-4333-8333-333333333333", document.Variables["team"])
				var response any = linearTrustedPreflightResponse()
				status := http.StatusOK
				header := http.Header{}
				if requests == 1 {
					require.Contains(t, document.Query, "query ConnectorLinearCreateScope")
					require.Equal(t, "810f57a7e383", document.Variables["project"])
					data := response.(map[string]any)["data"].(map[string]any)
					projects := data["projects"].(map[string]any)
					project := projects["nodes"].([]any)[0].(map[string]any)
					switch scenario {
					case "wrong-team":
						data["team"] = map[string]any{"id": "55555555-5555-4555-8555-555555555555"}
					case "wrong-membership":
						project["teams"].(map[string]any)["nodes"] = []any{map[string]any{"id": "55555555-5555-4555-8555-555555555555"}}
					case "missing-project":
						projects["nodes"] = []any{}
					case "ambiguous-project":
						projects["pageInfo"] = map[string]any{"hasNextPage": true}
					case "paginated-membership":
						project["teams"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true}
					case "wrong-slug":
						project["slugId"] = "abcdef123456"
					case "invalid-projected-uuid":
						project["id"] = "invalid"
					case "graphql-errors":
						response.(map[string]any)["errors"] = []any{map[string]any{"message": "provider data is withheld"}}
					case "redirect":
						status = http.StatusFound
						header.Set("Location", "https://unapproved.invalid/graphql")
					}
				} else {
					require.Equal(t, 2, requests, "preflight and mutation are never retried")
					require.Contains(t, document.Query, "mutation ConnectorLinearCreateInProject")
					require.Equal(t, "44444444-4444-4444-8444-444444444444", document.Variables["project"])
					require.Equal(t, "untrusted $(not-code)", document.Variables["body"])
					project := "44444444-4444-4444-8444-444444444444"
					if scenario == "wrong-created-project" {
						project = "55555555-5555-4555-8555-555555555555"
					}
					response = map[string]any{"data": map[string]any{"issueCreate": map[string]any{
						"success": true, "issue": map[string]any{
							"id": "22222222-2222-4222-8222-222222222222", "identifier": "ENG-123",
							"team": map[string]any{"id": document.Variables["team"]}, "project": map[string]any{"id": project},
						},
					}}}
				}
				encoded, err := json.Marshal(response)
				require.NoError(t, err)
				if scenario == "malformed-response" {
					encoded = []byte("{")
				}
				if scenario == "oversized-response" {
					encoded = []byte(strings.Repeat("x", 16385))
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(string(encoded))), Request: request}, nil
			})}
			host := connectors.CreatedResourceHost{Run: "linear-bound-create", Key: []byte(strings.Repeat("x", 32))}
			result, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue-in-project", map[string]any{"title": "Title", "body": "untrusted $(not-code)"})
			if scenario == "success" {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, 2, requests)
			} else {
				require.Error(t, err)
				maxRequests := 1
				if strings.HasSuffix(scenario, "team-secret") {
					maxRequests = 0
				}
				if scenario == "wrong-created-project" {
					maxRequests = 2
				}
				require.Equal(t, maxRequests, requests, "invalid preflight must not authorize a mutation")
			}
		})
	}
}

func TestLinearTrustedCreationRejectsAgentAuthorityAndStagesWithoutSecrets(t *testing.T) {
	plan := linearTrustedCreationPlan(t)
	args := map[string]any{"title": "Title", "body": "body"}
	_, err := plan.PrepareSafeOutputIntent("create-issue-in-project", args)
	require.NoError(t, err, "staging must not require the team secret or a read preflight")
	calls := 0
	runtime := connectors.Runtime{
		Phase: "safe-output", AllowWrites: true, AllowedOrigins: []string{"https://api.linear.app"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			calls++
			return "", errors.New("must not resolve secrets for invalid agent input")
		},
	}
	for _, name := range []string{"team-id", "project-id", "endpoint", "query"} {
		injected := map[string]any{"title": "Title", "body": "body", name: "untrusted"}
		host := connectors.CreatedResourceHost{Run: "linear-bound-create", Key: []byte(strings.Repeat("x", 32))}
		_, err := plan.ExecuteCreatedSafeOutput(t.Context(), runtime, host, "create-issue-in-project", injected)
		require.Error(t, err)
	}
	require.Zero(t, calls)
	for _, replacement := range []struct{ old, value string }{
		{`create-project-slug: "810f57a7e383"`, `create-project-slug: "${{ vars.ARBITRARY }}"`},
		{`create-team-secret: LINEAR_TEAM_ID`, `create-team-secret: "${{ secrets.ARBITRARY }}"`},
	} {
		_, _, _, err = parseLinearConnectorWorkflow(t, strings.Replace(linearTrustedCreationImport, replacement.old, replacement.value, 1),
			"safe-outputs:\n  linear-create-issue-in-project:\n")
		require.Error(t, err, "Actions expressions are not trusted resource authority or secret names")
	}
}
